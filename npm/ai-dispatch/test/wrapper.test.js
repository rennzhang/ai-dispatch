"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");
const test = require("node:test");

async function waitForFile(file, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      return (await fs.readFile(file, "utf8")).trim();
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  throw new Error(`timed out waiting for ${file}`);
}

function waitForExit(child) {
  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
}

async function assertSignalForwarding(t, forwardedSignal) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-wrapper-signal-"));
  const binDir = path.join(root, "bin");
  const nativeDir = path.join(binDir, "native");
  const wrapper = path.join(binDir, "ai-dispatch.js");
  const native = path.join(nativeDir, "ai-dispatch-go");
  const pidFile = path.join(root, "native.pid");
  const signalFile = path.join(root, "native.signal");
  t.after(async () => fs.rm(root, { recursive: true, force: true }));

  await fs.mkdir(nativeDir, { recursive: true });
  await fs.copyFile(path.resolve(__dirname, "../bin/ai-dispatch.js"), wrapper);
  await fs.writeFile(
    native,
    `#!/usr/bin/env node
const fs = require("node:fs");
fs.writeFileSync(process.env.NATIVE_PID_FILE, String(process.pid));
process.on(${JSON.stringify(forwardedSignal)}, () => {
  fs.writeFileSync(process.env.NATIVE_SIGNAL_FILE, ${JSON.stringify(forwardedSignal)});
  process.exit(130);
});
setInterval(() => {}, 1000);
`,
    { mode: 0o755 },
  );
  await fs.chmod(wrapper, 0o755);

  const child = spawn(process.execPath, [wrapper], {
    env: {
      ...process.env,
      NATIVE_PID_FILE: pidFile,
      NATIVE_SIGNAL_FILE: signalFile,
    },
    stdio: "ignore",
  });
  const nativePID = Number(await waitForFile(pidFile));
  child.kill(forwardedSignal);
  const result = await waitForExit(child);

  assert.equal(result.code, 130);
  assert.equal(await waitForFile(signalFile), forwardedSignal);
  assert.throws(() => process.kill(nativePID, 0), /ESRCH/);
}

for (const forwardedSignal of ["SIGTERM", "SIGHUP"]) {
  test(`forwards ${forwardedSignal} to the native binary and waits for it`, async (t) => {
    await assertSignalForwarding(t, forwardedSignal);
  });
}

test("does not force-kill native before native finishes provider cleanup", async (t) => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-wrapper-cleanup-"));
  const binDir = path.join(root, "bin");
  const nativeDir = path.join(binDir, "native");
  const wrapper = path.join(binDir, "ai-dispatch.js");
  const native = path.join(nativeDir, "ai-dispatch-go");
  const pidFile = path.join(root, "native.pid");
  const providerPIDFile = path.join(root, "provider.pid");
  let providerPID = 0;
  t.after(async () => {
    if (providerPID) {
      try {
        process.kill(providerPID, "SIGKILL");
      } catch (error) {
        if (error.code !== "ESRCH") throw error;
      }
    }
    await fs.rm(root, { recursive: true, force: true });
  });

  await fs.mkdir(nativeDir, { recursive: true });
  await fs.copyFile(path.resolve(__dirname, "../bin/ai-dispatch.js"), wrapper);
  await fs.writeFile(
    native,
    `#!/usr/bin/env node
const fs = require("node:fs");
const { spawn } = require("node:child_process");
fs.writeFileSync(process.env.NATIVE_PID_FILE, String(process.pid));
const provider = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { stdio: "ignore" });
fs.writeFileSync(process.env.PROVIDER_PID_FILE, String(provider.pid));
process.on("SIGTERM", () => {
  setTimeout(() => {
    provider.once("exit", () => process.exit(130));
    provider.kill("SIGTERM");
  }, 5200);
});
setInterval(() => {}, 1000);
`,
    { mode: 0o755 },
  );
  await fs.chmod(wrapper, 0o755);

  const child = spawn(process.execPath, [wrapper], {
    env: {
      ...process.env,
      NATIVE_PID_FILE: pidFile,
      PROVIDER_PID_FILE: providerPIDFile,
    },
    stdio: "ignore",
  });
  const nativePID = Number(await waitForFile(pidFile));
  providerPID = Number(await waitForFile(providerPIDFile));
  child.kill("SIGTERM");
  const result = await waitForExit(child);

  assert.equal(result.code, 130);
  assert.throws(() => process.kill(nativePID, 0), /ESRCH/);
  assert.throws(() => process.kill(providerPID, 0), /ESRCH/);
  providerPID = 0;
});

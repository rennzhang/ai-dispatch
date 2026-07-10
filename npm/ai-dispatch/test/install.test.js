"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs/promises");
const { createServer } = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { execFile, execFileSync } = require("node:child_process");
const test = require("node:test");
const { promisify } = require("node:util");

const { installBinary } = require("../lib/install");
const { platformAsset } = require("../lib/platform");

const execFileAsync = promisify(execFile);

async function startReleaseServer(binary, checksum, filename, version = "0.3.0") {
  const server = createServer((request, response) => {
    if (request.url === `/download/v${version}/SHA256SUMS`) {
      response.end(`${checksum}  ${filename}\n`);
      return;
    }
    if (request.url === `/download/v${version}/${filename}`) {
      response.end(binary);
      return;
    }
    response.statusCode = 404;
    response.end("not found");
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  return {
    baseURL: `http://127.0.0.1:${address.port}`,
    close: () => new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve()))),
  };
}

test("installs a verified platform binary atomically", async (t) => {
  const binary = Buffer.from("#!/bin/sh\nprintf 'npm-native-ok\\n'\n");
  const checksum = crypto.createHash("sha256").update(binary).digest("hex");
  const server = await startReleaseServer(binary, checksum, "ai-dispatch-linux-amd64.bin");
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-npm-test-"));
  t.after(async () => {
    await server.close();
    await fs.rm(root, { recursive: true, force: true });
  });

  const result = await installBinary({
    root,
    packageJSON: { version: "0.3.0" },
    platform: "linux",
    arch: "x64",
    releaseBaseURL: server.baseURL,
  });

  assert.equal(result.filename, "ai-dispatch-linux-amd64.bin");
  assert.equal(execFileSync(result.destination, { encoding: "utf8" }).trim(), "npm-native-ok");
});

test("refuses a binary that does not match the release checksum", async (t) => {
  const binary = Buffer.from("not the expected binary");
  const server = await startReleaseServer(binary, "0".repeat(64), "ai-dispatch-linux-amd64.bin");
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-npm-test-"));
  t.after(async () => {
    await server.close();
    await fs.rm(root, { recursive: true, force: true });
  });

  await assert.rejects(
    installBinary({
      root,
      packageJSON: { version: "0.3.0" },
      platform: "linux",
      arch: "x64",
      releaseBaseURL: server.baseURL,
    }),
    /checksum mismatch/,
  );
  await assert.rejects(fs.access(path.join(root, "bin", "native", "ai-dispatch-go")));
});

test("a packed package installs and exposes the CLI wrapper", async (t) => {
  const asset = platformAsset();
  const binary = Buffer.from("#!/bin/sh\nprintf 'npm-package-ok\\n'\n");
  const checksum = crypto.createHash("sha256").update(binary).digest("hex");
  const server = await startReleaseServer(binary, checksum, asset.filename);
  const packageRoot = path.resolve(__dirname, "..");
  const packageOutput = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-npm-pack-"));
  const project = await fs.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-npm-install-"));
  t.after(async () => {
    await server.close();
    await fs.rm(packageOutput, { recursive: true, force: true });
    await fs.rm(project, { recursive: true, force: true });
  });

  const { stdout: packOutput } = await execFileAsync("npm", ["pack", "--pack-destination", packageOutput], {
    cwd: packageRoot,
    encoding: "utf8",
  });
  const tarballName = packOutput.trim().split(/\r?\n/).filter((line) => line.endsWith(".tgz")).at(-1);
  assert.ok(tarballName, `npm pack did not print a tarball name: ${packOutput}`);
  const tarball = path.join(packageOutput, tarballName);
  await execFileAsync("npm", ["install", "--no-audit", "--no-fund", tarball], {
    cwd: project,
    encoding: "utf8",
    env: { ...process.env, AI_DISPATCH_RELEASE_BASE_URL: server.baseURL },
  });

  const installedPackage = path.join(project, "node_modules", "ai-dispatch");
  const { stdout: repackOutput } = await execFileAsync("npm", ["pack", "--dry-run"], {
    cwd: installedPackage,
    encoding: "utf8",
  });
  assert.match(repackOutput, /version check skipped outside the source repository/);

  const wrapper = path.join(project, "node_modules", ".bin", "ai-dispatch");
  assert.equal(execFileSync(wrapper, { encoding: "utf8" }).trim(), "npm-package-ok");
});

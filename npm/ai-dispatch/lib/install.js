"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const { parseChecksum, platformAsset, releaseURLs } = require("./platform");

async function downloadTo(url, destination) {
  let fetchError;
  try {
    const response = await fetch(url);
    if (!response.ok) {
      throw new Error(`HTTP ${response.status} ${response.statusText}`.trim());
    }
    await fsp.writeFile(destination, Buffer.from(await response.arrayBuffer()));
    return "fetch";
  } catch (error) {
    fetchError = error;
  }

  const result = spawnSync(
    "curl",
    ["--fail", "--location", "--silent", "--show-error", "--retry", "3", url, "--output", destination],
    { encoding: "utf8" },
  );
  if (!result.error && result.status === 0) {
    return "curl";
  }

  const curlDetail = result.error ? result.error.message : (result.stderr || "curl failed").trim();
  throw new Error(`download failed for ${url}: fetch=${fetchError.message}; curl=${curlDetail}`);
}

async function sha256(file) {
  const hash = crypto.createHash("sha256");
  const stream = fs.createReadStream(file);
  for await (const chunk of stream) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

async function installBinary(options = {}) {
  const root = options.root || path.resolve(__dirname, "..");
  const packageJSON = options.packageJSON || JSON.parse(await fsp.readFile(path.join(root, "package.json"), "utf8"));
  const version = options.version || packageJSON.version;
  const asset = platformAsset(options.platform, options.arch);
  const urls = releaseURLs(version, asset.filename, options.releaseBaseURL || process.env.AI_DISPATCH_RELEASE_BASE_URL);
  const tempDir = await fsp.mkdtemp(path.join(os.tmpdir(), "ai-dispatch-npm-"));
  const checksumsPath = path.join(tempDir, "SHA256SUMS");
  const binaryPath = path.join(tempDir, asset.filename);

  try {
    const checksumTransport = await downloadTo(urls.checksumURL, checksumsPath);
    const expected = parseChecksum(await fsp.readFile(checksumsPath, "utf8"), asset.filename);
    const binaryTransport = await downloadTo(urls.binaryURL, binaryPath);
    const actual = await sha256(binaryPath);
    if (actual !== expected) {
      throw new Error(`checksum mismatch for ${asset.filename}: expected ${expected}, got ${actual}`);
    }

    const destination = path.join(root, "bin", "native", "ai-dispatch-go");
    const staged = `${destination}.tmp-${process.pid}`;
    await fsp.mkdir(path.dirname(destination), { recursive: true });
    await fsp.copyFile(binaryPath, staged);
    await fsp.chmod(staged, 0o755);
    await fsp.rename(staged, destination);
    return {
      destination,
      filename: asset.filename,
      transports: { checksum: checksumTransport, binary: binaryTransport },
    };
  } finally {
    await fsp.rm(tempDir, { recursive: true, force: true });
  }
}

module.exports = {
  downloadTo,
  installBinary,
  sha256,
};

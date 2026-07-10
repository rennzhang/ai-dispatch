"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const { parseChecksum, platformAsset, releaseURLs } = require("../lib/platform");

test("maps supported Node platforms to release asset names", () => {
  assert.deepEqual(platformAsset("darwin", "arm64"), {
    os: "darwin",
    arch: "arm64",
    filename: "ai-dispatch-darwin-arm64.bin",
  });
  assert.deepEqual(platformAsset("linux", "x64"), {
    os: "linux",
    arch: "amd64",
    filename: "ai-dispatch-linux-amd64.bin",
  });
});

test("rejects unsupported platforms before downloading", () => {
  assert.throws(() => platformAsset("win32", "x64"), /unsupported platform/);
  assert.throws(() => platformAsset("linux", "ia32"), /unsupported platform/);
});

test("uses the matching release tag and checksum line", () => {
  const urls = releaseURLs("0.3.0", "ai-dispatch-linux-amd64.bin", "https://example.test/releases/");
  assert.equal(urls.checksumURL, "https://example.test/releases/download/v0.3.0/SHA256SUMS");
  assert.equal(urls.binaryURL, "https://example.test/releases/download/v0.3.0/ai-dispatch-linux-amd64.bin");

  const expected = "a".repeat(64);
  assert.equal(
    parseChecksum(`${expected}  ai-dispatch-linux-amd64.bin\n`, "ai-dispatch-linux-amd64.bin"),
    expected,
  );
  assert.throws(() => parseChecksum("", "ai-dispatch-linux-amd64.bin"), /does not contain/);
});

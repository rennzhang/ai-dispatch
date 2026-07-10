"use strict";

const OS_NAMES = {
  darwin: "darwin",
  linux: "linux",
};

const ARCH_NAMES = {
  x64: "amd64",
  arm64: "arm64",
};

function platformAsset(platform = process.platform, arch = process.arch) {
  const os = OS_NAMES[platform];
  const goarch = ARCH_NAMES[arch];
  if (!os || !goarch) {
    throw new Error(
      `unsupported platform ${platform}/${arch}; supported: darwin or linux on x64 or arm64`,
    );
  }

  return {
    os,
    arch: goarch,
    filename: `ai-dispatch-${os}-${goarch}.bin`,
  };
}

function releaseURLs(version, filename, releaseBaseURL) {
  const base = (releaseBaseURL || "https://github.com/rennzhang/ai-dispatch/releases").replace(/\/$/, "");
  const tag = version.startsWith("v") ? version : `v${version}`;
  const releaseURL = `${base}/download/${tag}`;
  return {
    checksumURL: `${releaseURL}/SHA256SUMS`,
    binaryURL: `${releaseURL}/${filename}`,
  };
}

function parseChecksum(checksums, filename) {
  const escapedFilename = filename.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(`^([a-fA-F0-9]{64})\\s+\\*?${escapedFilename}\\s*$`, "m");
  const match = pattern.exec(checksums);
  if (!match) {
    throw new Error(`SHA256SUMS does not contain ${filename}`);
  }
  return match[1].toLowerCase();
}

module.exports = {
  parseChecksum,
  platformAsset,
  releaseURLs,
};

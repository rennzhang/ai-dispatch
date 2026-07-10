"use strict";

const fs = require("node:fs");
const path = require("node:path");

const packageRoot = path.resolve(__dirname, "..");
const repositoryRoot = path.resolve(packageRoot, "../..");
const packageJSON = JSON.parse(fs.readFileSync(path.join(packageRoot, "package.json"), "utf8"));
const versionFile = path.join(repositoryRoot, "skills", "ai-dispatch", "VERSION");
if (!fs.existsSync(versionFile)) {
  console.log("ai-dispatch npm package version check skipped outside the source repository");
  process.exit(0);
}
const releaseVersion = fs.readFileSync(versionFile, "utf8").trim().replace(/^v/, "");

if (packageJSON.version !== releaseVersion) {
  throw new Error(
    `npm package version ${packageJSON.version} must match skills/ai-dispatch/VERSION ${releaseVersion}`,
  );
}

console.log(`ai-dispatch npm package version verified: ${packageJSON.version}`);

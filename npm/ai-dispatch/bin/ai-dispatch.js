#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const binary = path.join(__dirname, "native", "ai-dispatch-go");
if (!fs.existsSync(binary)) {
  console.error("ai-dispatch: native binary is missing; reinstall with npm install -g ai-dispatch");
  process.exit(1);
}

const result = spawnSync(binary, process.argv.slice(2), {
  env: process.env,
  stdio: "inherit",
});
if (result.error) {
  console.error(`ai-dispatch: failed to start native binary: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status == null ? 1 : result.status);

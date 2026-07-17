#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

const binary = path.join(__dirname, "native", "ai-dispatch-go");
if (!fs.existsSync(binary)) {
  console.error("ai-dispatch: native binary is missing; reinstall with npm install -g ai-dispatch");
  process.exit(1);
}

const child = spawn(binary, process.argv.slice(2), {
  env: process.env,
  stdio: "inherit",
});

let forwardedSignal = "";

function exitCodeForSignal(signal) {
  const number = os.constants.signals[signal];
  return number ? 128 + number : 1;
}

function forwardSignal(signal) {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  if (forwardedSignal) {
    return;
  }
  forwardedSignal = signal;
  child.kill(signal);
}

for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(signal, () => forwardSignal(signal));
}

child.on("error", (error) => {
  console.error(`ai-dispatch: failed to start native binary: ${error.message}`);
  process.exitCode = 1;
});

child.on("exit", (code, signal) => {
  if (code !== null) {
    process.exitCode = forwardedSignal && code === 0 ? exitCodeForSignal(forwardedSignal) : code;
    return;
  }
  process.exitCode = exitCodeForSignal(signal || forwardedSignal);
});

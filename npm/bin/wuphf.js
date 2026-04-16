#!/usr/bin/env node
"use strict";

// Thin shim that spawns the native wuphf binary. Downloads lazily on first
// run if postinstall was skipped (common with `npm install --ignore-scripts`
// and with some `npx` cache behaviors).

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { downloadBinary } = require("../scripts/download-binary");

const binaryPath =
  process.env.WUPHF_BINARY || path.join(__dirname, "wuphf");

async function ensureBinary() {
  if (process.env.WUPHF_BINARY && fs.existsSync(process.env.WUPHF_BINARY)) {
    return process.env.WUPHF_BINARY;
  }
  if (fs.existsSync(binaryPath)) return binaryPath;
  return downloadBinary();
}

function run(resolvedPath) {
  const child = spawn(resolvedPath, process.argv.slice(2), {
    stdio: "inherit",
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
    } else {
      process.exit(code ?? 0);
    }
  });
  child.on("error", (err) => {
    process.stderr.write(`wuphf: failed to launch binary: ${err.message}\n`);
    process.exit(1);
  });
}

ensureBinary()
  .then(run)
  .catch((err) => {
    process.stderr.write(`wuphf: ${err.message}\n`);
    process.exit(1);
  });

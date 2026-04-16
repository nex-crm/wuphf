"use strict";

// Best-effort: fetch the binary at install time. Failures are non-fatal —
// the bin/wuphf.js shim will retry on first invocation. This keeps
// `npm install` from failing behind flaky networks or corporate proxies.

const { downloadBinary } = require("./download-binary");

if (process.env.WUPHF_SKIP_POSTINSTALL === "1") {
  process.exit(0);
}

downloadBinary().catch((err) => {
  process.stderr.write(
    `wuphf: postinstall download failed (${err.message}). ` +
      `The binary will be fetched on first run.\n`,
  );
  process.exit(0);
});

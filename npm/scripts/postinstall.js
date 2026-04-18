"use strict";

// Postinstall: fetch and cryptographically verify the wuphf binary.
//
// Security model: the download is verified against the SHA256 listed in the
// release's checksums.txt. If the archive is tampered with, or the hash file
// is unreachable, the install MUST fail — silently continuing would allow a
// compromised release token to plant a backdoored binary on every machine
// that runs `npm install wuphf`.
//
// Escape hatches (opt-in only):
//   WUPHF_SKIP_POSTINSTALL=1
//     Skip the download entirely. The bin/wuphf.js shim will attempt an
//     (also-verified) download on first invocation. Use this for packaging
//     builds, offline mirrors, or CI images that restore a prebuilt bin/.
//
//   WUPHF_POSTINSTALL_SOFT_FAIL=1
//     Downgrade a *network* failure (e.g., GitHub unreachable behind a
//     corporate proxy) from fatal to a warning. SHA256 mismatches are ALWAYS
//     fatal and cannot be soft-failed — that path exists to catch tampering.

const { downloadBinary } = require("./download-binary");

if (process.env.WUPHF_SKIP_POSTINSTALL === "1") {
  process.stderr.write(
    "wuphf: postinstall skipped via WUPHF_SKIP_POSTINSTALL=1\n",
  );
  process.exit(0);
}

downloadBinary().catch((err) => {
  const message = err && err.message ? err.message : String(err);
  const isIntegrityFailure =
    message.includes("SHA256 mismatch") ||
    message.includes("Cannot verify download integrity");

  // Integrity failures are ALWAYS fatal. No soft-fail, no retry-on-first-run.
  if (isIntegrityFailure) {
    process.stderr.write(
      `\nwuphf: SECURITY: ${message}\n` +
        `wuphf: aborting install. No binary has been placed in bin/.\n\n`,
    );
    process.exit(1);
  }

  // Non-integrity failures (network, DNS, disk, unsupported platform).
  if (process.env.WUPHF_POSTINSTALL_SOFT_FAIL === "1") {
    process.stderr.write(
      `wuphf: postinstall download failed (${message}).\n` +
        `wuphf: continuing because WUPHF_POSTINSTALL_SOFT_FAIL=1 is set. ` +
        `The binary will be fetched (and verified) on first run.\n`,
    );
    process.exit(0);
  }

  process.stderr.write(
    `\nwuphf: postinstall download failed: ${message}\n` +
      `wuphf: set WUPHF_POSTINSTALL_SOFT_FAIL=1 to downgrade this to a ` +
      `warning, or WUPHF_SKIP_POSTINSTALL=1 to skip the download entirely.\n\n`,
  );
  process.exit(1);
});

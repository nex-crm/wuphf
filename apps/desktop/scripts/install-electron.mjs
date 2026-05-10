import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, "../../..");
const electronCacheRoot = resolve(repoRoot, ".cache/electron");
mkdirSync(electronCacheRoot, { recursive: true });
process.env.electron_config_cache ??= electronCacheRoot;
process.env.ELECTRON_CACHE ??= electronCacheRoot;

const require = createRequire(import.meta.url);
let installEntrypoint;
try {
  installEntrypoint = require.resolve("electron/install.js");
} catch (cause) {
  throw new Error(
    "Unable to resolve electron/install.js. Verify the installer entrypoint for the pinned Electron version.",
    { cause },
  );
}
require(installEntrypoint);

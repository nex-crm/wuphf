import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, "../../..");
const electronCacheRoot = resolve(repoRoot, ".cache/electron");
mkdirSync(electronCacheRoot, { recursive: true });
process.env.electron_config_cache ??= electronCacheRoot;

const require = createRequire(import.meta.url);
require("electron/install.js");

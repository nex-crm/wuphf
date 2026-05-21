import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";

const rootDir = dirname(fileURLToPath(import.meta.url));

// Workspace packages MUST be inlined into the build output. Their
// package.json `exports` map to raw `./src/*.ts`, and the packaged
// Electron utility process can't evaluate raw TS at runtime — Node's
// strip-only TS support rejects parameter properties used by classes
// like `SqliteReceiptStore`. Externalizing them produces an
// `import "@wuphf/broker"` statement that crashes the utility process
// on startup.
const WORKSPACE_BUNDLE = ["@wuphf/broker", "@wuphf/protocol"];

// `@wuphf/broker` now uses Node's stdlib `node:sqlite`; no native npm
// SQLite binding needs to remain as a runtime external.

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin({ exclude: WORKSPACE_BUNDLE })],
    build: {
      outDir: "out/main",
      rollupOptions: {
        input: {
          index: resolve(rootDir, "src/main/index.ts"),
          "broker-entry": resolve(rootDir, "src/main/broker-entry.ts"),
        },
        output: {
          format: "es",
          entryFileNames: "[name].js",
        },
      },
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin({ exclude: WORKSPACE_BUNDLE })],
    build: {
      outDir: "out/preload",
      rollupOptions: {
        input: resolve(rootDir, "src/preload/preload.ts"),
        output: {
          // Electron sandboxed preload scripts run as CommonJS.
          format: "cjs",
          entryFileNames: "preload.js",
        },
      },
    },
  },
  renderer: {
    root: resolve(rootDir, "src/renderer"),
    server: {
      host: "localhost",
      port: 5173,
      strictPort: true,
    },
    build: {
      outDir: resolve(rootDir, "out/renderer"),
      emptyOutDir: true,
      rollupOptions: {
        input: resolve(rootDir, "src/renderer/index.html"),
      },
    },
  },
});

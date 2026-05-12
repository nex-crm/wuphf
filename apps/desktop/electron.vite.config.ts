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

// Packages that MUST stay external in the main bundle even after
// inlining the workspace deps. `better-sqlite3` is a transitive of
// `@wuphf/broker` and pulls native bindings via `bindings()` — the
// resolver looks for its sibling `.node` file in `node_modules/
// better-sqlite3/`, so the JS wrapper must remain a runtime require
// rather than getting flattened into our chunk.
const NATIVE_EXTERNAL = ["better-sqlite3"];

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin({ exclude: WORKSPACE_BUNDLE })],
    build: {
      outDir: "out/main",
      rollupOptions: {
        external: NATIVE_EXTERNAL,
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

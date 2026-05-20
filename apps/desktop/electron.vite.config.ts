import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import type { Plugin } from "vite";

const rootDir = dirname(fileURLToPath(import.meta.url));

// The broker's migrations.ts reads its `.sql` files via
// `readFileSync(new URL("./00X_*.sql", import.meta.url))`. When the broker
// is bundled into `out/main/broker-entry.js` the relative URL resolves into
// `out/main/`, so the SQL files must be copied alongside the bundle.
// Workspace-internal contract: keep this list source-of-truth in
// `packages/broker/src/event-log/`.
function copyBrokerMigrationsPlugin(): Plugin {
  const migrationsDir = resolve(rootDir, "../../packages/broker/src/event-log");
  return {
    name: "wuphf-copy-broker-migrations",
    apply: "build",
    generateBundle() {
      for (const entry of readdirSync(migrationsDir)) {
        if (!entry.endsWith(".sql")) continue;
        this.emitFile({
          type: "asset",
          fileName: entry,
          source: readFileSync(resolve(migrationsDir, entry), "utf8"),
        });
      }
    },
  };
}

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

// Vite dev needs an inline preamble for React Refresh and a websocket for
// HMR. The packaged `index.html` ships a strict `<meta>` CSP that blocks
// both, so we rewrite the meta CSP only when serving dev. The
// `installDynamicRendererCsp` listener applies the matching response-header
// loosening in `src/main/csp.ts`.
const DEV_INDEX_CSP =
  "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws://localhost:5173 http://localhost:5173; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

function devRendererCspPlugin(): Plugin {
  return {
    name: "wuphf-dev-renderer-csp",
    apply: "serve",
    transformIndexHtml(html) {
      return html.replace(
        /<meta[^>]*http-equiv=["']Content-Security-Policy["'][^>]*>/i,
        `<meta http-equiv="Content-Security-Policy" content="${DEV_INDEX_CSP}" />`,
      );
    },
  };
}

export default defineConfig({
  main: {
    plugins: [
      externalizeDepsPlugin({ exclude: WORKSPACE_BUNDLE }),
      copyBrokerMigrationsPlugin(),
    ],
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
    plugins: [react(), devRendererCspPlugin()],
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

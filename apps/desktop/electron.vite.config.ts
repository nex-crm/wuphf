import { lstatSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import type { Plugin } from "vite";

const rootDir = dirname(fileURLToPath(import.meta.url));

// Authoritative list of migration files the broker reads at startup.
// Mirrors the `MIGRATIONS` array in
// `packages/broker/src/event-log/migrations.ts`. Keep this list in sync;
// a missing entry means the packaged broker boots against the previous
// schema and fails the first foreign-key write.
//
// We hard-code the list rather than globbing the migrations directory so
// that:
//   1. a stray `.sql` fixture or symlink dropped under `event-log/` cannot
//      be silently emitted into the packaged artifact (security: arbitrary
//      file ends up next to Electron main code)
//   2. `bun --cwd apps/desktop run build` fails fast and points at the
//      mismatch if migrations.ts gains a new version without this list
//      being updated.
const BROKER_MIGRATION_FILES = [
  "001_initial.sql",
  "002_cost_ledger.sql",
  "003_agent_provider_routing.sql",
  "004_webauthn.sql",
  "005_webauthn_ttl_indexes.sql",
  "006_threads.sql",
  "007_approvals.sql",
  "008_pending_approvals_thread_status_index.sql",
  "009_pending_approvals_thread_fk.sql",
] as const;

// The broker's migrations.ts reads its `.sql` files via
// `readFileSync(new URL("./00X_*.sql", import.meta.url))`. When the broker
// is bundled into `out/main/broker-entry.js` the relative URL resolves
// into `out/main/`, so the SQL files must be copied alongside that
// bundle. We also enforce the contract that `broker-entry.js` lives at
// `out/main/broker-entry.js` rather than under `out/main/chunks/` — if a
// future Rollup split moves the importing chunk elsewhere,
// `import.meta.url` will resolve into the chunk directory and miss the
// emitted SQL. Fail at build time rather than at runtime in the field.
function copyBrokerMigrationsPlugin(): Plugin {
  const migrationsDir = resolve(rootDir, "../../packages/broker/src/event-log");
  return {
    name: "wuphf-copy-broker-migrations",
    apply: "build",
    generateBundle(_options, bundle) {
      for (const fileName of BROKER_MIGRATION_FILES) {
        const sourcePath = resolve(migrationsDir, fileName);
        // `lstat` (not `stat`) so a symlink under `event-log/` cannot
        // redirect us to read an unrelated path. The migrations
        // directory is part of the workspace and only checked-in
        // regular files belong here.
        const stat = lstatSync(sourcePath);
        if (!stat.isFile()) {
          throw new Error(
            `copyBrokerMigrationsPlugin: ${fileName} is not a regular file (symlinks, directories, and devices are refused)`,
          );
        }
        this.emitFile({
          type: "asset",
          fileName,
          source: readFileSync(sourcePath, "utf8"),
        });
      }

      // Hard-pin the assumption that the broker entry bundle lives at
      // `out/main/broker-entry.js`. If Rollup ever moves it under
      // `chunks/`, the runtime `new URL("./001_initial.sql",
      // import.meta.url)` resolution misses the emitted SQL and the
      // broker subprocess crash-loops at startup. CI catches it because
      // the e2e spec depends on broker readiness.
      const brokerEntry = bundle["broker-entry.js"];
      if (brokerEntry === undefined) {
        throw new Error(
          "copyBrokerMigrationsPlugin: expected `broker-entry.js` at out/main/broker-entry.js — Rollup chunking changed; update this guard and verify migration colocation",
        );
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

// `@wuphf/broker` now uses Node's stdlib `node:sqlite`; no native npm
// SQLite binding needs to remain as a runtime external.

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
      const rewritten = html.replace(
        /<meta[^>]*http-equiv=["']Content-Security-Policy["'][^>]*>/i,
        `<meta http-equiv="Content-Security-Policy" content="${DEV_INDEX_CSP}" />`,
      );
      if (rewritten === html) {
        // If the meta tag shape ever changes we want a loud failure at
        // dev-server boot rather than a silent strict CSP that blocks
        // Vite's React Refresh preamble with no breadcrumb.
        throw new Error(
          "devRendererCspPlugin: Content-Security-Policy meta tag not found in index.html — rewrite would silently no-op",
        );
      }
      return rewritten;
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

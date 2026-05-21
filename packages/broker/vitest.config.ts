import { defineConfig } from "vitest/config";

// Vitest 2 on local Node 22 strips `node:` from experimental builtins that
// are absent from `module.builtinModules`; keep production imports direct.
function nodeSqliteVitestPlugin() {
  const virtualId = "\0broker-node-sqlite";
  return {
    name: "broker-node-sqlite",
    enforce: "pre" as const,
    resolveId(id: string) {
      if (id === "node:sqlite") {
        return virtualId;
      }
      return undefined;
    },
    load(id: string) {
      if (id !== virtualId) {
        return undefined;
      }

      return [
        'const sqlite = require("node:sqlite");',
        "export const DatabaseSync = sqlite.DatabaseSync;",
        "export const StatementSync = sqlite.StatementSync;",
        "export const backup = sqlite.backup;",
        "export const constants = sqlite.constants;",
        "export default sqlite;",
      ].join("\n");
    },
  };
}

// One-way ratchet: each PR must hold or improve every metric. Lower a number
// only by adding a docs commit that explains why; never to make a red PR
// green. Aspirational floor 98/98/98/98.
export default defineConfig({
  plugins: [nodeSqliteVitestPlugin()],
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      reporter: process.env["CI"] ? ["text", "json-summary"] : ["text"],
      reportsDirectory: "coverage",
      thresholds: {
        // Branch-4 slice measured floor — one-way ratchet from here.
        // Defensive paths (route-failed catch, client-disconnect-while-
        // writing-keepalive, statusText switch defaults, decodeURIComponent
        // error branch) hold the numbers down. Tighten as later branches
        // exercise these paths through real handlers.
        lines: 87,
        statements: 87,
        functions: 93,
        branches: 75,
      },
    },
  },
});

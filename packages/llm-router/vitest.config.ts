import { defineConfig } from "vitest/config";

// Vitest 2 on Node 22 strips the `node:` prefix from experimental builtins
// (absent from `module.builtinModules`) when transitively loading sources
// from a workspace dependency. llm-router imports `@wuphf/broker`, which
// uses `node:sqlite`; once vite-node strips the prefix, the bare `sqlite`
// resolves to nothing. Shim both forms so transitive broker loads work.
function nodeSqliteVitestPlugin() {
  const virtualId = "\0llm-router-node-sqlite";
  return {
    name: "llm-router-node-sqlite",
    enforce: "pre" as const,
    resolveId(id: string) {
      if (id === "node:sqlite" || id === "sqlite") {
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

// One-way ratchet (matches broker convention): each PR must hold or improve
// every metric. Lower a number only by adding a commit that explains why;
// never to make a red PR green. Aspirational floor 95/95/95/95 once the
// SDK-loader convenience constructors (createXxxProviderWithKey) and ollama
// transport-failure paths grow integration tests against real fakes.
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
        // PR #834 round-1 measured floor. Tighten when SDK-loader and
        // long-tail transport paths gain integration tests.
        lines: 84,
        statements: 84,
        functions: 88,
        branches: 83,
      },
    },
  },
});

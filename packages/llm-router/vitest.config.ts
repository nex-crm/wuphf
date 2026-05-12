import { defineConfig } from "vitest/config";

// One-way ratchet (matches broker convention): each PR must hold or improve
// every metric. Lower a number only by adding a commit that explains why;
// never to make a red PR green. Aspirational floor 95/95/95/95 once the
// SDK-loader convenience constructors (createXxxProviderWithKey) and ollama
// transport-failure paths grow integration tests against real fakes.
export default defineConfig({
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

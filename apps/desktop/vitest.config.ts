import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    typecheck: {
      enabled: true,
      tsconfig: "tsconfig.tests.json",
    },
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: ["src/renderer/**/*.ts", "src/main/index.ts"],
      thresholds: {
        // One-way ratchet at the measured floor minus at most one percentage point.
        // Measured: 97.32 lines / 97.32 statements / 97.01 functions / 96.75 branches.
        lines: 97,
        statements: 97,
        functions: 97,
        branches: 96,
      },
    },
  },
});

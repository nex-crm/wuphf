import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: [
        "src/renderer/**/*.ts",
        "src/main/index.ts",
      ],
      thresholds: {
        // One-way ratchet at the measured floor minus at most one percentage point.
        // Measured: 85.73 lines / 85.73 statements / 94.23 functions / 84.21 branches.
        lines: 85,
        statements: 85,
        functions: 94,
        branches: 83,
      },
    },
  },
});

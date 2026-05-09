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
        // Ratcheted after the desktop shell skeleton added contract tests.
        // Measured: 86.68 lines / 86.68 statements / 95.23 functions / 79.2 branches.
        lines: 85,
        statements: 85,
        functions: 94,
        branches: 78,
      },
    },
  },
});

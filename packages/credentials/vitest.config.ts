import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      reporter: process.env["CI"] ? ["text", "json-summary"] : ["text"],
      reportsDirectory: "coverage",
    },
  },
});

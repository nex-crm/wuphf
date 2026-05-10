import { defineConfig } from "vitest/config";

// One-way ratchet: each PR must hold or improve every metric. Lower a number
// only by adding a docs commit that explains why; never to make a red PR
// green. Aspirational floor 98/98/98/98.
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

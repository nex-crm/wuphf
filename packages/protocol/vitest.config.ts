import { defineConfig } from "vitest/config";

// Coverage thresholds form a quality ratchet: each PR must maintain or improve
// every metric. NEVER lower these to make a PR green — write tests instead, or
// surface the regression to the reviewer. The aspirational floor is 98/98/98/98;
// raise these (in a docs commit) every time the measured number stably exceeds
// the gate by ≥1 point.
export default defineConfig({
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: [
        // brand.ts is the Brand<T, "Tag"> type primitive: pure type, zero
        // runtime. v8 coverage reports it as 0% which drags the average
        // down without measuring anything real.
        "src/brand.ts",
      ],
      reporter: process.env["CI"] ? ["text", "json-summary"] : ["text"],
      reportsDirectory: "coverage",
      thresholds: {
        // Ratcheted up after R11 added 198 spec-driven tests.
        // Measured: 97.05 lines / 97.05 statements / 99.62 functions / 89.79 branches.
        // Aspirational target: 98/98/98/98.
        lines: 97,
        statements: 97,
        functions: 99,
        branches: 89,
      },
    },
  },
});

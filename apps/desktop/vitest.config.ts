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
        // One-way ratchet at the measured floor.
        // Measured (post R15 broker hardening + dedicated branch tests):
        // 99.8 lines / 99.8 statements / 100 functions / 96.21 branches.
        // The two genuinely unreachable defensive branches (NOOP_LOGGER
        // arrow bodies and the stopping guard in handleRestartStartFailure)
        // are wrapped in v8 ignore blocks at their definitions; restart and
        // settle-twice paths have dedicated coverage in broker-supervisor.spec.ts.
        lines: 99,
        statements: 99,
        functions: 100,
        branches: 96,
      },
    },
  },
});

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
      exclude: [
        "src/renderer/**/*.ts",
        "src/main/index.ts",
        // Subprocess entry — runs in a forked utility process, depends on
        // `process.parentPort` + `process.exit`. The broker logic it wires
        // up is covered exhaustively by packages/broker; the supervisor's
        // side of the channel is covered by broker-supervisor.spec.ts via
        // a mock UtilityProcess.
        "src/main/broker-entry.ts",
      ],
      thresholds: {
        // 100/100/100/100. The pass-1/pass-2 broker hardening added
        // defensive branches (malformed-message rejection, payload-key
        // allowlisting, startup-timeout fencing, broker-log forwarding
        // grammar) that briefly pushed branches below the previous floor.
        // This branch backfills targeted property tests of every helper
        // (`broker-helpers.spec.ts`) plus supervisor-level coverage of
        // each timer-race / sender-identity / restart-fatal arm
        // (`broker-supervisor.spec.ts`). One arm remains genuinely
        // unreachable and is documented at its `/* v8 ignore */` site:
        // the non-http/file rendererUrl fallthrough (`window.ts`).
        //
        // Keep at 100 — a regression past this floor is a meaningful
        // signal that new code added an untested defensive branch or
        // an untested happy path.
        lines: 100,
        statements: 100,
        functions: 100,
        branches: 100,
      },
    },
  },
});

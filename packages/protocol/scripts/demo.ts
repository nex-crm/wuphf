// @wuphf/protocol — manually-runnable demo
//
// Usage:
//   bun run packages/protocol/scripts/demo.ts
//
// Walks through adversarial scenarios. Each prints:
//   • what we tried to do
//   • what the moat MUST do (expected behavior)
//   • what the moat actually did (so a human can spot a regression)
//
// This is the manual companion to the test suite — same invariants, but
// laid out as a narrative so a reviewer doesn't have to read fast-check
// arbitraries to be convinced the moat fires on the right inputs.

import { runApprovalRouteScenarios } from "./demo/approval-route-scenarios.ts";
import { runCoreScenarios, runFrozenNfkcScenario } from "./demo/core-scenarios.ts";
import { runCostScenarios } from "./demo/cost-scenarios.ts";
import { runCredentialRunnerScenarios } from "./demo/credential-runner-scenarios.ts";
import { ANSI, printSummaryAndExit } from "./demo/harness.ts";
import { runIpcScenarios } from "./demo/ipc-scenarios.ts";
import { touchPublicSurfaceSentinels } from "./demo/public-surface-sentinels.ts";
import { runThreadScenarios } from "./demo/thread-scenarios.ts";

console.log(`${ANSI.bold}@wuphf/protocol — moat demo${ANSI.reset}`);
console.log(`${ANSI.dim}Each scenario: input → expected behavior → actual.${ANSI.reset}`);

runCoreScenarios();
runIpcScenarios();
runThreadScenarios();
runCostScenarios();
runCredentialRunnerScenarios();
runFrozenNfkcScenario();
runApprovalRouteScenarios();
touchPublicSurfaceSentinels();

printSummaryAndExit();

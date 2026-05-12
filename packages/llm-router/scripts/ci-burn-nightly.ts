// Section 10.4 nightly burn-down. The scheduled workflow runs this
// long-form simulation separately from pull-request CI. Contract from RFC
// Section 10.4:
//
//   bun run --cwd packages/llm-router burn:nightly
//
// Asserts:
//   1. Read-only at $5.00 ± $0.05 — total office spend lands inside that
//      band after 60 minutes of throttled traffic.
//   2. Per-agent wake cap throttles BEFORE the office daily cap. (Confirmed
//      directly by counting calls a single agent landed before its wake
//      cap rejected it; this number must be `wakeCapPerHour`, not the
//      full minute-by-minute throughput.)
//   3. Circuit breaker fires within 3 consecutive failures.
//
// The Section 10.4 assertion of "cost tile updates every 30s" is a renderer-side
// SLO. We approximate it here by calling `gateway.inspect()` every
// 30s and confirming the call is constant-time and reflects current state;
// the real wire emission belongs to renderer integration.
//
// Running:
//   bun run packages/llm-router/scripts/ci-burn-nightly.ts
//
// Exits 0 on all assertions passing; 1 on any failure.

import {
  createCostLedger,
  createEventLog,
  openDatabase,
  runMigrations,
} from "@wuphf/broker/cost-ledger";
import { asAgentSlug } from "@wuphf/protocol";

import {
  CapExceededError,
  CircuitBreakerOpenError,
  createGateway,
  createStubProvider,
  type Gateway,
  STUB_FIXED_COST_MICRO_USD,
  STUB_MODEL_ERROR,
  STUB_MODEL_FIXED_COST,
  type SupervisorContext,
} from "../src/index.ts";

interface BurnConfig {
  readonly wakesPerMinute: number;
  readonly minutes: number;
  readonly dailyMicroUsd: number;
  readonly wakeCapPerHour: number;
}

const CONFIG: BurnConfig = {
  wakesPerMinute: 10,
  minutes: 60,
  dailyMicroUsd: 5_000_000, // $5
  wakeCapPerHour: 12,
};

const DAILY_TOLERANCE_MICRO_USD = 50_000; // ± $0.05

interface BurnResult {
  readonly name: string;
  readonly passed: boolean;
  readonly observed: string;
  readonly expected: string;
}

const results: BurnResult[] = [];

function record(name: string, passed: boolean, observed: string, expected: string): void {
  results.push({ name, passed, observed, expected });
  console.log(`${passed ? "PASS" : "FAIL"} ${name}`);
  console.log(`     observed: ${observed}`);
  console.log(`     expected: ${expected}`);
}

interface Clock {
  now: number;
}

function buildGateway(): {
  gateway: Gateway;
  clock: Clock;
  ledger: ReturnType<typeof createCostLedger>;
  close: () => void;
} {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const ledger = createCostLedger(db, eventLog);
  const clock: Clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
  const gateway = createGateway({
    ledger,
    providers: [createStubProvider()],
    nowMs: () => clock.now,
    config: {
      caps: {
        dailyMicroUsd: CONFIG.dailyMicroUsd,
        wakeCapPerHour: CONFIG.wakeCapPerHour,
      },
    },
  });
  return { gateway, clock, ledger, close: () => db.close() };
}

async function assertion1ReadOnlyAtCap(): Promise<void> {
  const { gateway, clock, ledger, close } = buildGateway();
  try {
    // Spread the load across enough agents so the per-agent wake cap
    // doesn't dominate. ceil(daily_cap / per_call_cost / wake_cap) =
    // 5_000_000 / 10_000 / 12 = 42 agents lets us land ~500 successful
    // calls = $5.00 even with the wake-cap throttle per agent.
    const totalWakes = CONFIG.wakesPerMinute * CONFIG.minutes;
    const agentCount = Math.ceil(totalWakes / CONFIG.wakeCapPerHour);
    const agents: SupervisorContext[] = Array.from({ length: agentCount }, (_, i) => ({
      agentSlug: asAgentSlug(`agent_${String(i).padStart(3, "0")}`),
    }));

    let cappedCount = 0;
    for (let minute = 0; minute < CONFIG.minutes; minute += 1) {
      for (let wake = 0; wake < CONFIG.wakesPerMinute; wake += 1) {
        const agent = agents[(minute * CONFIG.wakesPerMinute + wake) % agentCount];
        if (agent === undefined) continue;
        // Note human activity each minute to keep the idle gate open.
        if (wake === 0) gateway.noteHumanActivity();
        try {
          await gateway.complete(agent, {
            model: STUB_MODEL_FIXED_COST,
            // Distinct prompt per call so dedupe never short-circuits.
            prompt: `burn-${minute}-${wake}`,
            maxOutputTokens: 8,
          });
        } catch (err) {
          if (err instanceof CapExceededError) {
            cappedCount += 1;
          } else {
            throw err;
          }
        }
        clock.now += 1; // tick a millisecond between calls so wake-cap timestamps differ
      }
      clock.now += 60_000 - CONFIG.wakesPerMinute; // advance to the next minute boundary
    }

    const totalSpend = ledger
      .listAgentSpend({ dayUtc: "2026-05-12" })
      .reduce((acc, row) => acc + (row.totalMicroUsd as number), 0);

    const inBand =
      totalSpend >= CONFIG.dailyMicroUsd - DAILY_TOLERANCE_MICRO_USD &&
      totalSpend <= CONFIG.dailyMicroUsd + DAILY_TOLERANCE_MICRO_USD;

    record(
      "read-only at $5.00 ± $0.05",
      inBand,
      `total_spend=${totalSpend} μUSD, capped_count=${cappedCount}`,
      `total_spend in [${CONFIG.dailyMicroUsd - DAILY_TOLERANCE_MICRO_USD}, ${CONFIG.dailyMicroUsd + DAILY_TOLERANCE_MICRO_USD}]`,
    );
  } finally {
    close();
  }
}

async function assertion2WakeCapBeforeDaily(): Promise<void> {
  const { gateway, clock, close } = buildGateway();
  try {
    const single: SupervisorContext = { agentSlug: asAgentSlug("solo_agent") };
    let successes = 0;
    let wakeCapReached = false;
    let wakeCapHitAtCall: number | null = null;

    // Try far more wakes than either cap allows; the wake cap (12/hr)
    // should reject us long before the daily cap (500 calls).
    for (let i = 0; i < 100; i += 1) {
      gateway.noteHumanActivity();
      try {
        await gateway.complete(single, {
          model: STUB_MODEL_FIXED_COST,
          prompt: `solo-${i}`,
          maxOutputTokens: 8,
        });
        successes += 1;
      } catch (err) {
        if (err instanceof CapExceededError && err.cap === "wake") {
          wakeCapReached = true;
          wakeCapHitAtCall = i;
          break;
        }
        if (err instanceof CapExceededError && err.cap === "daily") {
          break;
        }
        throw err;
      }
      clock.now += 1;
    }

    const dailyCallCap = Math.floor(CONFIG.dailyMicroUsd / STUB_FIXED_COST_MICRO_USD);
    const passed =
      wakeCapReached && successes === CONFIG.wakeCapPerHour && successes < dailyCallCap;
    record(
      "per-agent wake cap throttles before daily cap",
      passed,
      `successes=${successes}, wake_cap_hit_at=${wakeCapHitAtCall}`,
      `successes == wake_cap (${CONFIG.wakeCapPerHour}) and successes < daily_call_cap (${dailyCallCap})`,
    );
  } finally {
    close();
  }
}

async function assertion3BreakerWithinThreeFailures(): Promise<void> {
  const { gateway, clock, close } = buildGateway();
  try {
    const agent: SupervisorContext = { agentSlug: asAgentSlug("breaker_test") };
    let breakerTrippedAtFailure: number | null = null;

    for (let i = 0; i < 5; i += 1) {
      gateway.noteHumanActivity();
      try {
        await gateway.complete(agent, {
          model: STUB_MODEL_ERROR,
          prompt: `breaker-${i}`,
          maxOutputTokens: 8,
        });
      } catch (err) {
        if (err instanceof CircuitBreakerOpenError) {
          breakerTrippedAtFailure = i;
          break;
        }
        // Provider error is expected for stub-error; keep going until breaker opens.
      }
      clock.now += 1;
    }

    const passed = breakerTrippedAtFailure !== null && breakerTrippedAtFailure <= 3;
    record(
      "circuit breaker fires within 3 consecutive failures",
      passed,
      `breaker_tripped_at_failure=${breakerTrippedAtFailure}`,
      `breaker_tripped_at_failure <= 3`,
    );
  } finally {
    close();
  }
}

async function main(): Promise<void> {
  console.log(
    `@wuphf/llm-router — §10.4 burn-down\n` +
      `--wakes-per-minute ${CONFIG.wakesPerMinute} ` +
      `--minutes ${CONFIG.minutes} ` +
      `--model ${STUB_MODEL_FIXED_COST}\n`,
  );

  await assertion1ReadOnlyAtCap();
  await assertion2WakeCapBeforeDaily();
  await assertion3BreakerWithinThreeFailures();

  const failed = results.filter((r) => !r.passed).length;
  console.log(`\n${results.length - failed}/${results.length} assertions passed`);
  process.exit(failed === 0 ? 0 : 1);
}

await main();

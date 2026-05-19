import {
  canonicalJSON,
  type validateReceiptBudget,
  type verifyChain,
  type verifyChainIncremental,
} from "../../src/index.ts";

export const ANSI = {
  reset: "\x1b[0m",
  dim: "\x1b[2m",
  bold: "\x1b[1m",
  green: "\x1b[32m",
  red: "\x1b[31m",
  yellow: "\x1b[33m",
  cyan: "\x1b[36m",
};

let passed = 0;
let failed = 0;

export const textDecoder = new TextDecoder();

export function header(num: number, title: string): void {
  console.log("");
  console.log(`${ANSI.bold}${ANSI.cyan}── Scenario ${num}: ${title}${ANSI.reset}`);
}

export function expectThrows(fn: () => unknown, expectedFragment: RegExp): void {
  try {
    const result = fn();
    console.log(`  ${ANSI.red}FAIL${ANSI.reset} expected throw, got: ${JSON.stringify(result)}`);
    failed++;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (expectedFragment.test(msg)) {
      console.log(
        `  ${ANSI.green}PASS${ANSI.reset} threw: ${ANSI.dim}${msg.slice(0, 100)}${ANSI.reset}`,
      );
      passed++;
    } else {
      console.log(
        `  ${ANSI.red}FAIL${ANSI.reset} threw wrong message: ${msg}\n` +
          `       expected to match: ${expectedFragment}`,
      );
      failed++;
    }
  }
}

export function expectEqual<T>(label: string, actual: T, expected: T): void {
  const ok = JSON.stringify(actual) === JSON.stringify(expected);
  if (ok) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${JSON.stringify(actual)}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label}\n` +
        `       expected: ${JSON.stringify(expected)}\n` +
        `       actual:   ${JSON.stringify(actual)}`,
    );
    failed++;
  }
}

export function expectCodecRoundTrip<T>(
  label: string,
  input: unknown,
  fromJson: (value: unknown) => T,
  toJsonValue: (value: T) => unknown,
): void {
  const serialized = toJsonValue(fromJson(input));
  const reparsed = fromJson(JSON.parse(canonicalJSON(serialized)));
  expectEqual(label, canonicalJSON(toJsonValue(reparsed)), canonicalJSON(serialized));
}

export function nonNull<T>(value: T | null | undefined, label: string): T {
  if (value === null || value === undefined) {
    throw new Error(`demo fixture missing required value: ${label}`);
  }
  return value;
}

export function expectChainResult(
  label: string,
  actual: ReturnType<typeof verifyChain>,
  expectedCode: string | "ok",
): void {
  const actualCode = actual.ok ? "ok" : actual.code;
  if (actualCode === expectedCode) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actualCode}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected ${expectedCode}, got ${actualCode}`,
    );
    failed++;
  }
}

export function expectIncrementalResult(
  label: string,
  actual: ReturnType<typeof verifyChainIncremental>,
  expectedCode: string | "ok",
): void {
  const actualCode = actual.ok ? "ok" : actual.code;
  if (actualCode === expectedCode) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actualCode}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected ${expectedCode}, got ${actualCode}`,
    );
    failed++;
  }
}

export function expectBudgetFailure(
  label: string,
  actual: ReturnType<typeof validateReceiptBudget>,
  expectedFragment: RegExp,
): void {
  if (!actual.ok && expectedFragment.test(actual.reason)) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actual.reason}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected reason matching ${expectedFragment}`,
    );
    failed++;
  }
}

export function printSummaryAndExit(): never {
  console.log("");
  console.log(`${ANSI.bold}─────────────────────────────────────${ANSI.reset}`);
  const summaryColor = failed === 0 ? ANSI.green : ANSI.red;
  console.log(`${ANSI.bold}${summaryColor}${passed} passed, ${failed} failed${ANSI.reset}`);
  process.exit(failed === 0 ? 0 : 1);
}

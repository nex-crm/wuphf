import type { RunnerEvent, RunnerFailureCode } from "@wuphf/protocol";
import type { LifecycleStateMachine } from "../lifecycle.ts";
import type { Receipt } from "../runner.ts";
import type { SerializedEmitter } from "./event-hub.ts";

export const DEFAULT_TERMINAL_CLEANUP_GRACE_MS = 5_000;

export class RunnerFailure extends Error {
  readonly code: RunnerFailureCode;

  constructor(message: string, code: RunnerFailureCode, options?: ErrorOptions | undefined) {
    super(message, options);
    this.name = "RunnerFailure";
    this.code = code;
  }
}

export interface TerminalCleanupChild {
  readonly isAlive: () => boolean;
  readonly kill: (signal: NodeJS.Signals) => void;
  readonly wait: () => Promise<unknown>;
}

export interface TerminalCleanupAbort {
  readonly abort: () => void;
  readonly wait: () => Promise<unknown>;
}

export type TerminalCleanupTarget =
  | { readonly kind: "child"; readonly child: TerminalCleanupChild }
  | { readonly kind: "abort"; readonly abort: TerminalCleanupAbort };

export interface TerminalCleanupArgs {
  readonly lifecycle: LifecycleStateMachine;
  readonly target?: TerminalCleanupTarget | undefined;
  readonly emitter: SerializedEmitter;
  readonly receiptStore?: { readonly put: (receipt: Receipt) => Promise<unknown> } | undefined;
  readonly failureReceipt?: Receipt | undefined;
  readonly failureCode: RunnerFailureCode;
  readonly failureEvent?: RunnerEvent | undefined;
  readonly failureAlreadyPublished?: boolean | undefined;
  readonly gracePeriodMs?: number | undefined;
  readonly stopped: {
    readonly exitCode?: number | undefined;
    readonly error?: string | undefined;
  };
}

export async function terminalCleanup(args: TerminalCleanupArgs): Promise<void> {
  const terminalClaimed = args.lifecycle.tryTerminate(args.failureCode);
  if (!terminalClaimed) return;
  const gracePeriodMs = args.gracePeriodMs ?? DEFAULT_TERMINAL_CLEANUP_GRACE_MS;
  try {
    await stopTarget(args.target, gracePeriodMs);
  } finally {
    try {
      if (args.failureReceipt !== undefined) {
        await writeFailureReceiptBestEffort(args.receiptStore, args.failureReceipt);
      }
      if (args.failureEvent !== undefined && args.failureAlreadyPublished !== true) {
        await args.emitter.emit(args.failureEvent).catch(() => undefined);
      }
    } finally {
      args.lifecycle.markStopped(args.stopped);
    }
  }
}

async function stopTarget(
  target: TerminalCleanupTarget | undefined,
  gracePeriodMs: number,
): Promise<void> {
  if (target === undefined) return;
  if (target.kind === "abort") {
    target.abort.abort();
    await target.abort.wait().catch(() => undefined);
    return;
  }
  const child = target.child;
  if (!child.isAlive()) return;
  child.kill("SIGTERM");
  const hardKill = setTimeout(() => {
    if (child.isAlive()) child.kill("SIGKILL");
  }, gracePeriodMs);
  hardKill.unref();
  try {
    await child.wait().catch(() => undefined);
  } finally {
    clearTimeout(hardKill);
  }
}

async function writeFailureReceiptBestEffort(
  receiptStore: { readonly put: (receipt: Receipt) => Promise<unknown> } | undefined,
  receipt: Receipt,
): Promise<void> {
  if (receiptStore === undefined) return;
  try {
    await receiptStore.put(receipt);
  } catch {
    // Cleanup must not poison the terminal failed event.
  }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function runnerFailureFromError(
  error: unknown,
  fallbackCode: RunnerFailureCode,
): RunnerFailure {
  if (error instanceof RunnerFailure) return error;
  return new RunnerFailure(errorMessage(error), fallbackCode, { cause: error });
}

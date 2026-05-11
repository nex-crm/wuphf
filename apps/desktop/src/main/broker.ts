import { execFile } from "node:child_process";

import type { BrokerUrl } from "@wuphf/protocol";
import { utilityProcess } from "electron";

import type { BrokerSnapshot, BrokerStatus } from "../shared/api-contract.ts";
import {
  type BrokerLogPayload,
  brokerUrlPort,
  errorCode,
  errorMessage,
  filterPayloadToSafeKeys,
  readBrokerLogMessage,
  readReadyMessage,
  sanitizeBrokerEventName,
} from "./broker-internal.ts";
import type { Logger, LogPayloadValue } from "./logger.ts";
import { monotonicNowMs } from "./monotonic-clock.ts";

const BROKER_SERVICE_NAME = "wuphf-broker";
// Env vars the broker subprocess actually needs. `WUPHF_RENDERER_DIST`
// tells broker-entry where the packaged renderer bundle lives so its
// static handler serves `/` instead of 404. Without it on the allowlist
// the env-allowlist filter in buildBrokerEnv silently drops the value
// `main/index.ts` set on `process.env` for packaged mode, and the
// packaged window loads `${brokerUrl}/` against a broker with
// `renderer: null` — the bundle 404s and the user sees an empty page.
// `WUPHF_DEV_RENDERER_ORIGIN` carries the electron-vite dev server origin
// in dev mode so the broker's `/api-token` origin gate accepts the
// cross-origin bootstrap fetch.
const BROKER_ENV_ALLOWLIST = [
  "PATH",
  "HOME",
  "USER",
  "LANG",
  "LC_ALL",
  "TZ",
  "WUPHF_RENDERER_DIST",
  "WUPHF_DEV_RENDERER_ORIGIN",
] as const;
const DEFAULT_STOP_GRACE_MS = 5_000;
const DEFAULT_FORCE_STOP_GRACE_MS = 1_000;
const DEFAULT_FIRST_BACKOFF_MS = 250;
const DEFAULT_MAX_BACKOFF_MS = 60_000;
const DEFAULT_MAX_RESTART_RETRIES = 5;
const DEFAULT_STABILITY_WINDOW_MS = 60_000;
const DEFAULT_LIVENESS_STALE_MS = 5_000;
// 10 s startup ceiling for the broker to bind a loopback port and post
// `{ ready }`. The listener is pure-Node and binds an ephemeral port — in
// practice this takes a handful of milliseconds — so 10 s only fires on
// pathological wedges (SIGSTOPed subprocess, deadlock in createBroker,
// listener bound but failed before IPC). Tighter than the v0 Go broker's
// 30 s because the new entry has no DB/cache warmup work.
const DEFAULT_STARTUP_TIMEOUT_MS = 10_000;
// All three arrow bodies execute through the no-logger smoke test in
// broker-supervisor.spec.ts: start → broker_starting/broker_started (info),
// liveness staleness → broker_ping_missed (warn), restart cap → broker_restart_cap_reached (error).
const NOOP_LOGGER: Logger = {
  info: () => undefined,
  warn: () => undefined,
  error: () => undefined,
};

type UtilityProcessHandle = ReturnType<typeof utilityProcess.fork>;
type ForkUtilityProcess = typeof utilityProcess.fork;
type RunWindowsTaskkill = (pid: number, options: { readonly force: boolean }) => Promise<void>;
type KillProcess = (pid: number, signal: NodeJS.Signals) => void;
export type ExecFileRunner = (
  file: string,
  args: readonly string[],
  callback: (error: Error | null) => void,
) => void;
type MonotonicNow = () => number;

export interface BrokerSupervisorConfig {
  readonly brokerEntryPath: string;
  readonly envSource?: NodeJS.ProcessEnv;
  readonly forkProcess?: ForkUtilityProcess;
  readonly platform?: NodeJS.Platform;
  readonly runWindowsTaskkill?: RunWindowsTaskkill;
  readonly killProcess?: KillProcess;
  readonly monotonicNow?: MonotonicNow;
  readonly stopGraceMs?: number;
  readonly forceStopGraceMs?: number;
  readonly firstBackoffMs?: number;
  readonly maxBackoffMs?: number;
  readonly maxRestartRetries?: number;
  readonly stabilityWindowMs?: number;
  readonly livenessStaleMs?: number;
  readonly startupTimeoutMs?: number;
  readonly onFatal?: (reason: string) => void;
  readonly logger?: Logger;
}

export class BrokerSupervisor {
  private readonly brokerEntryPath: string;
  private readonly envSource: NodeJS.ProcessEnv;
  private readonly forkProcess: ForkUtilityProcess;
  private readonly platform: NodeJS.Platform;
  private readonly runWindowsTaskkill: RunWindowsTaskkill;
  private readonly killProcess: KillProcess;
  private readonly monotonicNow: MonotonicNow;
  private readonly stopGraceMs: number;
  private readonly forceStopGraceMs: number;
  private readonly firstBackoffMs: number;
  private readonly maxBackoffMs: number;
  private readonly maxRestartRetries: number;
  private readonly stabilityWindowMs: number;
  private readonly livenessStaleMs: number;
  private readonly startupTimeoutMs: number;
  private readonly onFatal: ((reason: string) => void) | undefined;
  private readonly logger: Logger;

  private brokerProcess: UtilityProcessHandle | null = null;
  private restartTimer: NodeJS.Timeout | null = null;
  private startupTimer: NodeJS.Timeout | null = null;
  private startupForceTimer: NodeJS.Timeout | null = null;
  // Per-handle set so the message handler can drop ready/alive/broker_log
  // messages that arrive between the watchdog firing and the subprocess
  // actually exiting. Without this, a queued `{ ready }` racing the
  // termination would publish the brokerUrl of a process we already
  // decided to kill — same shape as the stale-fork race but with the
  // CURRENT fork, so sender-identity alone does not catch it.
  private startupTimedOutHandles: WeakSet<UtilityProcessHandle> = new WeakSet();
  private status: BrokerStatus = "unknown";
  private restartCount = 0;
  private stopping = false;
  private fatalReason: string | null = null;
  private lastRestartScheduledAtMs: number | null = null;
  private startedAtMs: number | null = null;
  private aliveSinceMs: number | null = null;
  private lastPingAtMs: number | null = null;
  private brokerUrl: BrokerUrl | null = null;
  private readyWaiters: Array<{
    readonly resolve: (url: BrokerUrl) => void;
    readonly reject: (err: Error) => void;
  }> = [];
  private readyListeners: Array<(url: BrokerUrl) => void> = [];

  constructor(config: BrokerSupervisorConfig) {
    this.brokerEntryPath = config.brokerEntryPath;
    this.envSource = config.envSource ?? process.env;
    this.forkProcess = config.forkProcess ?? utilityProcess.fork.bind(utilityProcess);
    this.platform = config.platform ?? process.platform;
    this.runWindowsTaskkill = config.runWindowsTaskkill ?? runWindowsTaskkill;
    this.killProcess = config.killProcess ?? process.kill.bind(process);
    this.monotonicNow = config.monotonicNow ?? monotonicNowMs;
    this.stopGraceMs = config.stopGraceMs ?? DEFAULT_STOP_GRACE_MS;
    this.forceStopGraceMs = config.forceStopGraceMs ?? DEFAULT_FORCE_STOP_GRACE_MS;
    this.firstBackoffMs = config.firstBackoffMs ?? DEFAULT_FIRST_BACKOFF_MS;
    this.maxBackoffMs = config.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
    this.maxRestartRetries = config.maxRestartRetries ?? DEFAULT_MAX_RESTART_RETRIES;
    this.stabilityWindowMs = config.stabilityWindowMs ?? DEFAULT_STABILITY_WINDOW_MS;
    this.livenessStaleMs = config.livenessStaleMs ?? DEFAULT_LIVENESS_STALE_MS;
    this.startupTimeoutMs = config.startupTimeoutMs ?? DEFAULT_STARTUP_TIMEOUT_MS;
    this.onFatal = config.onFatal;
    this.logger = config.logger ?? NOOP_LOGGER;
  }

  start(): void {
    if (this.brokerProcess !== null || this.fatalReason !== null) {
      return;
    }

    this.stopping = false;
    this.status = "starting";
    this.aliveSinceMs = null;
    this.lastPingAtMs = null;
    // brokerUrl belongs to the previous broker process; clear it so consumers
    // do not believe a freshly-restarting broker is still bound to its old
    // port. The new entry will report a fresh URL through `{ ready }` once
    // the listener binds.
    this.brokerUrl = null;
    this.startedAtMs = this.monotonicNow();
    this.logger.info("broker_starting", {
      restartCount: this.restartCount,
      serviceName: BROKER_SERVICE_NAME,
    });

    let brokerProcess: UtilityProcessHandle;
    try {
      brokerProcess = this.forkProcess(this.brokerEntryPath, [], {
        serviceName: BROKER_SERVICE_NAME,
        stdio: "pipe",
        env: buildBrokerEnv(this.envSource),
      });
    } catch (error) {
      this.status = "dead";
      this.startedAtMs = null;
      this.logger.error("broker_start_failed", {
        error: errorMessage(error),
        restartCount: this.restartCount,
        serviceName: BROKER_SERVICE_NAME,
      });
      throw error;
    }

    this.brokerProcess = brokerProcess;
    this.logger.info("broker_started", {
      pid: getProcessPid(brokerProcess),
      restartCount: this.restartCount,
      serviceName: BROKER_SERVICE_NAME,
    });
    drainBrokerStdio(brokerProcess);
    this.armStartupTimer(brokerProcess);
    brokerProcess.on("message", (message: unknown) => {
      // Stale-sender guard: utilityProcess messages can land after a restart
      // moved us to a fresh fork. Compare against the closure-captured handle
      // so a late `{ ready }` from the previous broker process cannot
      // overwrite the current `brokerUrl` or resolve waiters with a dead URL.
      if (this.brokerProcess !== brokerProcess) {
        return;
      }
      // Post-stop guard: stop() flipped `stopping = true` but a queued
      // {ready}/{alive} is still racing toward us. stop() owns lifecycle
      // from here; ignoring the message keeps `whenReady()` rejected and
      // prevents `brokerUrl` from being set during shutdown.
      if (this.stopping) {
        return;
      }
      // Startup-watchdog guard: the watchdog fired and the supervisor has
      // declared this fork wedged. Drop any in-flight messages from it so
      // a late `{ ready }` (between SIGTERM call and exit) cannot publish
      // a brokerUrl that's seconds away from being torn down.
      if (this.startupTimedOutHandles.has(brokerProcess)) {
        return;
      }
      if (isAliveMessage(message)) {
        const nowMs = this.monotonicNow();
        if (this.status !== "alive") {
          this.aliveSinceMs = nowMs;
          this.logger.info("broker_alive", {
            pid: getProcessPid(brokerProcess),
            restartCount: this.restartCount,
          });
        }
        this.lastPingAtMs = nowMs;
        this.status = "alive";
        return;
      }
      const ready = readReadyMessage(message);
      if (ready !== null) {
        this.clearStartupTimer();
        this.brokerUrl = ready.brokerUrl;
        // Flush waiters and notify listeners BEFORE logging. A previous
        // regression had this order reversed: the logger threw on a banned
        // payload key, which aborted the handler before flushReadyWaiters
        // ran, hanging whenReady() in packaged builds. We log a safe `port`
        // value today, but a future contributor adding any payload key
        // containing "url"/"path"/"token" etc. (logger.ts allowlist) would
        // reintroduce the exact same hang. Make the handshake robust by
        // construction: deliver readiness first, then log (wrapped in a
        // try/catch so the logger can never regress this ordering again).
        this.flushReadyWaiters(ready.brokerUrl);
        this.notifyReadyListeners(ready.brokerUrl);
        try {
          this.logger.info("broker_ready", {
            pid: getProcessPid(brokerProcess),
            port: brokerUrlPort(ready.brokerUrl),
            restartCount: this.restartCount,
          });
        } catch {
          // Swallow: the broker is ready, callers have been notified.
          // A logger failure (banned payload key, IO error in packaged
          // builds) must not regress the ready handshake.
        }
        return;
      }
      const brokerLog = readBrokerLogMessage(message);
      if (brokerLog !== null) {
        this.forwardBrokerLog(brokerLog);
        return;
      }
    });
    brokerProcess.once("exit", (exitCode: number | null) => {
      // utilityProcess can fire `exit` with `null` for signal-only POSIX
      // termination. Pass it through unchanged so telemetry distinguishes
      // "exited cleanly with code 0" from "killed by signal, no code".
      this.handleExit(brokerProcess, exitCode, null);
    });
  }

  getStatus(): BrokerStatus {
    if (
      this.status === "alive" &&
      this.lastPingAtMs !== null &&
      this.monotonicNow() - this.lastPingAtMs > this.livenessStaleMs
    ) {
      const nowMs = this.monotonicNow();
      this.status = "unresponsive";
      this.logger.warn("broker_ping_missed", {
        pid: getProcessPid(this.brokerProcess),
        lastPingAt: this.lastPingAtMs,
        livenessAgeMs: nowMs - this.lastPingAtMs,
        restartCount: this.restartCount,
      });
      return "unresponsive";
    }

    return this.status;
  }

  getPid(): number | null {
    return getProcessPid(this.brokerProcess);
  }

  getRestartCount(): number {
    return this.restartCount;
  }

  getSnapshot(): BrokerSnapshot {
    return {
      status: this.getStatus(),
      pid: getProcessPid(this.brokerProcess),
      restartCount: this.restartCount,
      brokerUrl: this.brokerUrl,
    };
  }

  /**
   * Wait until the broker reports `{ ready, brokerUrl }`. Resolves with the
   * URL the listener bound. Rejects if `stop()` runs or the supervisor hits
   * its restart cap before a `ready` arrives. Already-ready brokers resolve
   * synchronously on the next microtask. Calls made after a completed
   * `stop()` reject immediately rather than hang — without this guard a
   * post-stop waiter has nothing to resolve or reject it (no broker process
   * to send `{ ready }`, no exit/restart-cap path to fire `rejectReadyWaiters`).
   */
  whenReady(): Promise<BrokerUrl> {
    // Lifecycle checks come BEFORE the cached-URL fast path. If stop() has
    // begun, the cached `brokerUrl` is about to be cleared at settle() —
    // resolving with it would hand the caller a URL that's seconds away
    // from being torn down. Same logic for fatalReason: once the restart
    // cap fired, the URL is moot.
    if (this.fatalReason !== null) {
      return silentlyRejected(new Error(this.fatalReason));
    }
    if (this.stopping || (this.brokerProcess === null && this.status === "dead")) {
      return silentlyRejected(new Error("broker_stopped"));
    }
    if (this.brokerUrl !== null) {
      return Promise.resolve(this.brokerUrl);
    }
    let resolveFn!: (url: BrokerUrl) => void;
    let rejectFn!: (err: Error) => void;
    const promise = new Promise<BrokerUrl>((resolve, reject) => {
      resolveFn = resolve;
      rejectFn = reject;
    });
    // Silent shadow handler: stop() now synchronously rejects pending
    // waiters, which means the promise can be rejected before the caller's
    // `.then`/`.catch` is attached (a real production path: callers that
    // chain `await whenReady()` through another async function). Without
    // this shadow, Node fires `unhandledRejection` and Vitest treats it as
    // a failure — both spurious because callers DO observe the rejection
    // through their own chain. The shadow attaches a no-op handler at
    // creation time so the rejection is never counted as unhandled,
    // without consuming it from any other handler the caller adds later.
    promise.catch(noop);
    this.readyWaiters.push({ resolve: resolveFn, reject: rejectFn });
    return promise;
  }

  getLastRestartScheduledAtMs(): number | null {
    return this.lastRestartScheduledAtMs;
  }

  async stop(): Promise<void> {
    this.stopping = true;
    this.clearRestartTimer();
    this.clearStartupTimer();
    // Reject pending waiters synchronously. The message-handler `stopping`
    // gate above drops any late `{ ready }` arriving in the shutdown window,
    // so without rejecting here, whenReady() would hang until subprocess
    // exit even though stop() has committed to tearing things down.
    // settle()/the null branch call rejectReadyWaiters again with the same
    // sentinel — that's a no-op once the waiter array is drained.
    this.rejectReadyWaiters(new Error("broker_stopped"));
    this.logger.info("broker_stop_requested", {
      pid: getProcessPid(this.brokerProcess),
      restartCount: this.restartCount,
      status: this.status,
    });

    const brokerProcess = this.brokerProcess;
    if (brokerProcess === null) {
      this.status = "dead";
      this.startedAtMs = null;
      this.aliveSinceMs = null;
      this.lastPingAtMs = null;
      this.brokerUrl = null;
      this.rejectReadyWaiters(new Error("broker_stopped"));
      this.logger.info("broker_stop_noop");
      return;
    }

    await new Promise<void>((resolve) => {
      let settled = false;
      let stopTimer: NodeJS.Timeout | null = null;

      const settle = (): void => {
        if (settled) {
          return;
        }
        settled = true;
        if (stopTimer !== null) {
          clearTimeout(stopTimer);
        }
        brokerProcess.off("exit", settle);
        if (this.brokerProcess === brokerProcess) {
          this.brokerProcess = null;
        }
        this.status = "dead";
        this.startedAtMs = null;
        this.aliveSinceMs = null;
        this.lastPingAtMs = null;
        this.brokerUrl = null;
        this.rejectReadyWaiters(new Error("broker_stopped"));
        this.logger.info("broker_stopped", {
          pid: getProcessPid(brokerProcess),
          restartCount: this.restartCount,
        });
        resolve();
      };

      const scheduleStopStep = (delayMs: number, step: () => void): void => {
        stopTimer = setTimeout(() => {
          stopTimer = null;
          step();
        }, delayMs);
      };

      brokerProcess.once("exit", settle);
      this.requestGracefulStop(brokerProcess);

      scheduleStopStep(this.stopGraceMs, () => {
        this.requestProcessTermination(brokerProcess);
        scheduleStopStep(this.forceStopGraceMs, () => {
          this.forceStop(brokerProcess);
          settle();
        });
      });
    });
  }

  private handleExit(
    exitedProcess: UtilityProcessHandle,
    exitCode: number | null = null,
    signal: string | null = null,
  ): void {
    if (this.brokerProcess !== exitedProcess) {
      return;
    }

    const nowMs = this.monotonicNow();
    const startedAtMs = this.startedAtMs;
    // `startedAtMs === null` is defensive — `start()` sets `startedAtMs`
    // BEFORE assigning `brokerProcess`, so when we get past the
    // `brokerProcess !== exitedProcess` gate above, `startedAtMs` is
    // always non-null. The null arm is dead code today; kept as a
    // belt-and-suspenders guard against a future refactor that splits
    // the start handshake.
    this.logger.warn("broker_exited", {
      pid: getProcessPid(exitedProcess),
      exitCode,
      signal,
      restartCount: this.restartCount,
      /* v8 ignore next */
      uptimeMs: startedAtMs === null ? null : nowMs - startedAtMs,
      lastPingAt: this.lastPingAtMs,
    });

    this.brokerProcess = null;
    this.lastPingAtMs = null;
    // The listener died with the process; clear the cached URL so
    // `getSnapshot()` and `whenReady()` cannot hand out a stale endpoint
    // that no longer exists.
    this.brokerUrl = null;

    if (this.stopping) {
      this.status = "dead";
      this.aliveSinceMs = null;
      this.startedAtMs = null;
      this.rejectReadyWaiters(new Error("broker_stopped"));
      return;
    }

    this.resetRestartCountAfterStableWindow();
    this.scheduleRestart();
  }

  private flushReadyWaiters(brokerUrl: BrokerUrl): void {
    const waiters = this.readyWaiters;
    this.readyWaiters = [];
    for (const w of waiters) {
      w.resolve(brokerUrl);
    }
  }

  /**
   * Subscribe to every `{ ready, brokerUrl }` message from the broker
   * subprocess — first start AND every subsequent restart. Returns an
   * unsubscribe function. Use this from the main process to detect a
   * restart and reload broker-pinned BrowserWindows to the new origin
   * (the loopback port is ephemeral and changes on every fork).
   *
   * Listeners are invoked synchronously after `whenReady()` waiters are
   * flushed; an exception in a listener is logged at warn and does not
   * disrupt the next listener.
   */
  subscribeReady(listener: (brokerUrl: BrokerUrl) => void): () => void {
    this.readyListeners.push(listener);
    return () => {
      const idx = this.readyListeners.indexOf(listener);
      if (idx >= 0) {
        this.readyListeners.splice(idx, 1);
      }
    };
  }

  private notifyReadyListeners(brokerUrl: BrokerUrl): void {
    // Snapshot before iterating. A listener that unsubscribes itself
    // (or another listener) inside its callback mutates the live array
    // mid-iteration, and the index-based for…of advances past the now-
    // shifted next slot — silently skipping listeners. A common pattern
    // is "subscribe for the very next ready, then unsubscribe": that
    // would corrupt iteration without this snapshot.
    const snapshot = this.readyListeners.slice();
    for (const listener of snapshot) {
      try {
        listener(brokerUrl);
      } catch (error) {
        this.logger.warn("broker_ready_listener_threw", {
          error: errorMessage(error),
        });
      }
    }
  }

  private rejectReadyWaiters(err: Error): void {
    const waiters = this.readyWaiters;
    this.readyWaiters = [];
    for (const w of waiters) {
      w.reject(err);
    }
  }

  private scheduleRestart(): void {
    const nextRestartCount = this.restartCount + 1;
    if (nextRestartCount > this.maxRestartRetries) {
      this.status = "dead";
      this.fatalReason = `Broker exited after ${this.restartCount} restart retries`;
      this.logger.error("broker_restart_cap_reached", {
        restartCount: this.restartCount,
        maxRestartRetries: this.maxRestartRetries,
      });
      this.rejectReadyWaiters(new Error(this.fatalReason));
      this.onFatal?.(this.fatalReason);
      return;
    }

    this.restartCount = nextRestartCount;
    this.status = "starting";
    const backoffMs = Math.min(
      this.maxBackoffMs,
      this.firstBackoffMs * 2 ** (nextRestartCount - 1),
    );

    this.lastRestartScheduledAtMs = this.monotonicNow();
    this.logger.warn("broker_restart_scheduled", {
      restartCount: this.restartCount,
      backoffMs,
      maxRestartRetries: this.maxRestartRetries,
    });
    this.restartTimer = setTimeout(() => {
      this.restartTimer = null;
      // Symmetric guard at callback entry. Closes the same real-Node race
      // documented below in handleRestartStartFailure: if stop() runs while
      // this callback is already on the event queue, clearTimeout cannot
      // recall it. Without this check, start() would unconditionally clear
      // stopping=false and fork a fresh broker AFTER stop() completed,
      // leaking a process whether start() throws or succeeds.
      //
      // The `fatalReason` arm is defensive — `fatalReason` is only ever
      // set in cap-paths (`handleExit` line 536, `handleRestartStartFailure`
      // line 624) that `return` WITHOUT calling `scheduleRestart`, so no
      // public flow ends up with a pending restart-timer whose body sees
      // `fatalReason !== null`. We keep the guard as belt-and-suspenders
      // for future paths that might schedule a restart-then-go-fatal, but
      // the "fatal" ternary arm is dead code today.
      if (this.stopping || /* v8 ignore next */ this.fatalReason !== null) {
        this.logger.info("broker_restart_skipped", {
          restartCount: this.restartCount,
          /* v8 ignore next */
          reason: this.stopping ? "stopping" : "fatal",
        });
        return;
      }
      this.logger.info("broker_restart_attempt", {
        restartCount: this.restartCount,
        maxRestartRetries: this.maxRestartRetries,
      });
      try {
        this.start();
      } catch (error) {
        this.handleRestartStartFailure(error);
      }
    }, backoffMs);
  }

  private handleRestartStartFailure(error: unknown): void {
    this.status = "dead";
    this.brokerProcess = null;
    this.startedAtMs = null;
    this.aliveSinceMs = null;
    this.lastPingAtMs = null;
    const message = errorMessage(error);
    this.logger.error("broker_restart_start_failed", {
      error: message,
      restartCount: this.restartCount,
      maxRestartRetries: this.maxRestartRetries,
      serviceName: BROKER_SERVICE_NAME,
    });

    // Belt-and-suspenders for the restart-after-stop race that the timer
    // callback's entry guard (in scheduleRestart) closes for the common path.
    // The entry guard catches the case where stop() ran before the callback
    // fired. This guard catches the residual case where stop() flips
    // stopping=true synchronously from inside start() (e.g. a synchronous
    // forkProcess hook that calls back into the supervisor) and then start()
    // throws. Without it we would fall through to scheduleRestart() and
    // leak a fresh broker AFTER stop() requested shutdown. The single-thread
    // event-loop model and Vitest fake timers make a deterministic test for
    // this exact path infeasible, so coverage is suppressed.
    /* v8 ignore start */
    if (this.stopping) {
      return;
    }
    /* v8 ignore stop */

    if (this.restartCount >= this.maxRestartRetries) {
      this.fatalReason = `Broker start failed after ${this.restartCount} restart retries: ${message}`;
      this.rejectReadyWaiters(new Error(this.fatalReason));
      this.onFatal?.(this.fatalReason);
      return;
    }

    this.scheduleRestart();
  }

  private clearRestartTimer(): void {
    if (this.restartTimer !== null) {
      clearTimeout(this.restartTimer);
      this.restartTimer = null;
    }
  }

  private armStartupTimer(brokerProcess: UtilityProcessHandle): void {
    this.clearStartupTimer();
    this.startupTimer = setTimeout(() => {
      this.startupTimer = null;
      // Bail if we've already moved on (ready arrived, stop called, fatal cap
      // hit, or a restart cycled to a new fork). The closure-captured handle
      // disambiguates "this is the wedged fork" from "a fresh fork is running."
      if (
        this.brokerProcess !== brokerProcess ||
        this.brokerUrl !== null ||
        this.stopping ||
        this.fatalReason !== null
      ) {
        return;
      }
      const nowMs = this.monotonicNow();
      const startedAtMs = this.startedAtMs;
      // Same dead-arm rationale as the broker_exited log above:
      // `start()` sets `startedAtMs` before `armStartupTimer` registers
      // this callback, and any path that clears `startedAtMs` also
      // changes `brokerProcess`, which the 4-condition guard catches
      // FIRST. Defensive null arm only.
      this.logger.error("broker_ready_timeout", {
        pid: getProcessPid(brokerProcess),
        restartCount: this.restartCount,
        /* v8 ignore next */
        uptimeMs: startedAtMs === null ? null : nowMs - startedAtMs,
      });
      // Mark BEFORE termination so the message handler drops any in-flight
      // `{ ready }` / `{ alive }` / `{ broker_log }` that races between
      // the SIGTERM call and the exit event. Without this, a queued ready
      // could publish a brokerUrl for a process we're actively killing.
      this.startupTimedOutHandles.add(brokerProcess);
      // Kill the wedged subprocess. handleExit fires (status flips dead,
      // brokerUrl stays null), which feeds the existing restart cycle and
      // counts against the cap — so a permanently-wedged broker eventually
      // surfaces a fatalReason and rejects whenReady() waiters.
      this.requestProcessTermination(brokerProcess);
      // Force-escalate after the same grace stop() uses. If the subprocess
      // is so wedged that SIGTERM doesn't take (uninterruptible sleep, deep
      // deadlock, SIGSTOP), SIGKILL hits next. Without this ladder, exit
      // never fires and the restart cycle never starts — whenReady() waiters
      // hang past the cap. Mirrors the stop() ladder shape.
      this.startupForceTimer = setTimeout(() => {
        this.startupForceTimer = null;
        // The `brokerProcess === brokerProcess` (closure capture) check
        // is defensive: any natural code path that swaps `brokerProcess`
        // goes through `start()` → `armStartupTimer()` →
        // `clearStartupTimer()`, which clears this force timer before it
        // can fire. So the equality false-arm is dead today; kept as a
        // guard against a refactor that breaks the cleanup invariant.
        /* v8 ignore start */
        if (this.brokerProcess !== brokerProcess) {
          return;
        }
        /* v8 ignore stop */
        this.forceStop(brokerProcess);
      }, this.forceStopGraceMs);
    }, this.startupTimeoutMs);
  }

  private clearStartupTimer(): void {
    if (this.startupTimer !== null) {
      clearTimeout(this.startupTimer);
      this.startupTimer = null;
    }
    if (this.startupForceTimer !== null) {
      clearTimeout(this.startupForceTimer);
      this.startupForceTimer = null;
    }
  }

  // Re-emit a `{ broker_log }` message from the broker subprocess through the
  // main-process structured logger. The subprocess uses the permissive
  // BrokerLogger interface (any keys) but main uses an allowlist, so we
  // pre-filter to safe keys via `isSafePayloadKey`. Banned keys (url, path,
  // token, etc.) and unknown keys are silently dropped — we record only the
  // count via `droppedKeys` so on-call sees that redaction happened without
  // re-leaking the redacted values. Event names are sanitized against the
  // logger's own naming pattern; unparseable events are dropped entirely.
  private forwardBrokerLog(log: BrokerLogPayload): void {
    const event = sanitizeBrokerEventName(log.event);
    if (event === null) {
      this.logger.warn("broker_subprocess_log_invalid_event");
      return;
    }
    const { safePayload, droppedKeyCount } = filterPayloadToSafeKeys(log.payload);
    const finalPayload: Record<string, LogPayloadValue> = { ...safePayload };
    if (droppedKeyCount > 0) {
      // Bracket access required: tsconfig has noPropertyAccessFromIndexSignature.
      // biome-ignore lint/complexity/useLiteralKeys: see comment above
      finalPayload["droppedKeys"] = droppedKeyCount;
    }
    try {
      this.logger[log.broker_log](`broker_${event}`, finalPayload);
    } catch {
      // Pre-filter should make this unreachable; the supervisor's logger
      // must never bring the main process down regardless. Swallow.
    }
  }

  private requestGracefulStop(brokerProcess: UtilityProcessHandle): void {
    this.logger.info("broker_graceful_stop_requested", {
      pid: getProcessPid(brokerProcess),
      restartCount: this.restartCount,
    });
    safePostMessage(brokerProcess, { type: "shutdown" });
  }

  private requestProcessTermination(brokerProcess: UtilityProcessHandle): void {
    const pid = getProcessPid(brokerProcess);
    this.logger.warn("broker_termination_requested", {
      pid,
      force: false,
      restartCount: this.restartCount,
    });
    if (this.platform === "win32") {
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: false }).catch((error: unknown) => {
          this.logTaskkillFailure(pid, false, error);
        });
      }
      return;
    }

    killUtilityProcess(brokerProcess);
  }

  private forceStop(brokerProcess: UtilityProcessHandle): void {
    const pid = getProcessPid(brokerProcess);
    this.logger.warn("broker_termination_requested", {
      pid,
      force: true,
      restartCount: this.restartCount,
    });
    if (this.platform === "win32") {
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: true }).catch((error: unknown) => {
          this.logTaskkillFailure(pid, true, error);
        });
        return;
      }
    }

    if (pid !== null) {
      try {
        this.killProcess(pid, "SIGKILL");
        return;
      } catch (error) {
        this.logger.warn("broker_posix_sigkill_failed", {
          pid,
          error: errorMessage(error),
          code: errorCode(error),
          restartCount: this.restartCount,
        });
      }
    }

    killUtilityProcess(brokerProcess);
  }

  private logTaskkillFailure(pid: number, force: boolean, error: unknown): void {
    this.logger.warn("broker_taskkill_failed", {
      pid,
      force,
      error: errorMessage(error),
      code: errorCode(error),
    });
  }

  private resetRestartCountAfterStableWindow(): void {
    if (
      this.aliveSinceMs !== null &&
      this.monotonicNow() - this.aliveSinceMs > this.stabilityWindowMs
    ) {
      this.restartCount = 0;
    }
    this.aliveSinceMs = null;
  }
}

export function buildBrokerEnv(envSource: NodeJS.ProcessEnv): Record<string, string> {
  const brokerEnv: Record<string, string> = {};
  for (const key of BROKER_ENV_ALLOWLIST) {
    const value = envSource[key];
    if (typeof value === "string") {
      brokerEnv[key] = value;
    }
  }
  return brokerEnv;
}

function getProcessPid(brokerProcess: UtilityProcessHandle | null): number | null {
  const pid = brokerProcess?.pid;
  return typeof pid === "number" ? pid : null;
}

function isAliveMessage(message: unknown): message is { readonly alive: true } {
  return (
    typeof message === "object" &&
    message !== null &&
    Object.hasOwn(message, "alive") &&
    (message as { readonly alive?: unknown }).alive === true
  );
}

// Sentinel no-op for the whenReady() shadow-handler pattern. Pulled out to
// a named function so the call site reads as "attach silent shadow" instead
// of an inline arrow that could be mistaken for swallowing a real error.
function noop(): void {
  // Intentional: see silent-shadow comment in whenReady().
}

function silentlyRejected(err: Error): Promise<never> {
  const p = Promise.reject(err);
  p.catch(noop);
  return p;
}

function safePostMessage(brokerProcess: UtilityProcessHandle, message: unknown): void {
  try {
    brokerProcess.postMessage(message);
  } catch {
    // The force path remains armed; a closed message port should not block app quit.
  }
}

function drainBrokerStdio(brokerProcess: UtilityProcessHandle): void {
  brokerProcess.stdout?.on("data", discardBrokerOutput);
  brokerProcess.stderr?.on("data", discardBrokerOutput);
}

function discardBrokerOutput(_chunk: unknown): void {
  // Drain pipe-backed stdio without logging future broker app data into the main process.
}

function killUtilityProcess(brokerProcess: UtilityProcessHandle): void {
  brokerProcess.kill();
}

export function runWindowsTaskkill(
  pid: number,
  options: { readonly force: boolean },
  execFileRunner: ExecFileRunner = defaultExecFile,
): Promise<void> {
  const args = ["/pid", String(pid), "/T"];
  if (options.force) {
    args.push("/F");
  }

  return new Promise((resolve, reject) => {
    execFileRunner("taskkill", args, (error) => {
      if (error === null) {
        resolve();
        return;
      }

      reject(error);
    });
  });
}

function defaultExecFile(
  file: string,
  args: readonly string[],
  callback: (error: Error | null) => void,
): void {
  execFile(file, [...args], (error) => {
    callback(error);
  });
}

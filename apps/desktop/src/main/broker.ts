import { utilityProcess } from "electron";

import type { BrokerStatus } from "../shared/api-contract.ts";

const BROKER_SERVICE_NAME = "wuphf-broker";
const BROKER_ENV_ALLOWLIST = ["PATH", "HOME", "USER", "LANG", "LC_ALL", "TZ"] as const;
const DEFAULT_STOP_GRACE_MS = 5_000;
const DEFAULT_INITIAL_BACKOFF_MS = 250;
const DEFAULT_MAX_BACKOFF_MS = 60_000;
const DEFAULT_MAX_RESTART_RETRIES = 5;

type UtilityProcessHandle = ReturnType<typeof utilityProcess.fork>;
type ForkUtilityProcess = typeof utilityProcess.fork;
type KillProcess = (pid: number, signal: NodeJS.Signals) => void;

export interface BrokerSupervisorConfig {
  readonly brokerEntryPath: string;
  readonly envSource?: NodeJS.ProcessEnv;
  readonly forkProcess?: ForkUtilityProcess;
  readonly killProcess?: KillProcess;
  readonly stopGraceMs?: number;
  readonly initialBackoffMs?: number;
  readonly maxBackoffMs?: number;
  readonly maxRestartRetries?: number;
  readonly onFatal?: (reason: string) => void;
}

export class BrokerSupervisor {
  private readonly brokerEntryPath: string;
  private readonly envSource: NodeJS.ProcessEnv;
  private readonly forkProcess: ForkUtilityProcess;
  private readonly killProcess: KillProcess;
  private readonly stopGraceMs: number;
  private readonly initialBackoffMs: number;
  private readonly maxBackoffMs: number;
  private readonly maxRestartRetries: number;
  private readonly onFatal: ((reason: string) => void) | undefined;

  private brokerProcess: UtilityProcessHandle | null = null;
  private restartTimer: NodeJS.Timeout | null = null;
  private status: BrokerStatus = "unknown";
  private restartCount = 0;
  private stopping = false;
  private fatalReason: string | null = null;
  private lastRestartScheduledAtMs: number | null = null;

  constructor(config: BrokerSupervisorConfig) {
    this.brokerEntryPath = config.brokerEntryPath;
    this.envSource = config.envSource ?? process.env;
    this.forkProcess = config.forkProcess ?? utilityProcess.fork;
    this.killProcess = config.killProcess ?? process.kill;
    this.stopGraceMs = config.stopGraceMs ?? DEFAULT_STOP_GRACE_MS;
    this.initialBackoffMs = config.initialBackoffMs ?? DEFAULT_INITIAL_BACKOFF_MS;
    this.maxBackoffMs = config.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
    this.maxRestartRetries = config.maxRestartRetries ?? DEFAULT_MAX_RESTART_RETRIES;
    this.onFatal = config.onFatal;
  }

  start(): void {
    if (this.brokerProcess !== null || this.fatalReason !== null) {
      return;
    }

    this.stopping = false;
    this.status = "starting";
    const brokerProcess = this.forkProcess(this.brokerEntryPath, [], {
      serviceName: BROKER_SERVICE_NAME,
      stdio: "pipe",
      env: buildBrokerEnv(this.envSource),
    });

    this.brokerProcess = brokerProcess;
    brokerProcess.on("message", (message: unknown) => {
      if (isAliveMessage(message)) {
        this.status = "alive";
      }
    });
    brokerProcess.once("exit", () => {
      this.handleExit(brokerProcess);
    });
  }

  getStatus(): BrokerStatus {
    return this.status;
  }

  getPid(): number | null {
    return getProcessPid(this.brokerProcess);
  }

  getRestartCount(): number {
    return this.restartCount;
  }

  getLastRestartScheduledAtMs(): number | null {
    return this.lastRestartScheduledAtMs;
  }

  async stop(): Promise<void> {
    this.stopping = true;
    this.clearRestartTimer();

    const brokerProcess = this.brokerProcess;
    if (brokerProcess === null) {
      this.status = "dead";
      return;
    }

    await new Promise<void>((resolve) => {
      let settled = false;
      let forceKillTimer: NodeJS.Timeout | null = null;

      const settle = (): void => {
        if (settled) {
          return;
        }
        settled = true;
        if (forceKillTimer !== null) {
          clearTimeout(forceKillTimer);
        }
        brokerProcess.off("exit", settle);
        if (this.brokerProcess === brokerProcess) {
          this.brokerProcess = null;
        }
        this.status = "dead";
        resolve();
      };

      brokerProcess.once("exit", settle);
      this.sendSignal(brokerProcess, "SIGTERM");

      forceKillTimer = setTimeout(() => {
        this.sendSignal(brokerProcess, "SIGKILL");
        settle();
      }, this.stopGraceMs);
    });
  }

  private handleExit(exitedProcess: UtilityProcessHandle): void {
    if (this.brokerProcess !== exitedProcess) {
      return;
    }

    this.brokerProcess = null;

    if (this.stopping) {
      this.status = "dead";
      return;
    }

    this.scheduleRestart();
  }

  private scheduleRestart(): void {
    const nextRestartCount = this.restartCount + 1;
    if (nextRestartCount > this.maxRestartRetries) {
      this.status = "dead";
      this.fatalReason = `Broker exited after ${this.restartCount} restart retries`;
      this.onFatal?.(this.fatalReason);
      return;
    }

    this.restartCount = nextRestartCount;
    this.status = "starting";
    const backoffMs = Math.min(this.maxBackoffMs, this.initialBackoffMs * 2 ** nextRestartCount);

    // AGENTS.md rule 11 exception: restart backoff records monotonic time only.
    this.lastRestartScheduledAtMs = performance.now();
    this.restartTimer = setTimeout(() => {
      this.restartTimer = null;
      this.start();
    }, backoffMs);
  }

  private clearRestartTimer(): void {
    if (this.restartTimer !== null) {
      clearTimeout(this.restartTimer);
      this.restartTimer = null;
    }
  }

  private sendSignal(brokerProcess: UtilityProcessHandle, signal: NodeJS.Signals): void {
    const pid = getProcessPid(brokerProcess);
    if (pid === null) {
      brokerProcess.kill();
      return;
    }

    try {
      this.killProcess(pid, signal);
    } catch (error) {
      if (!isNoSuchProcessError(error)) {
        throw error;
      }
    }
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

function isNoSuchProcessError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    Object.hasOwn(error, "code") &&
    (error as { readonly code?: unknown }).code === "ESRCH"
  );
}

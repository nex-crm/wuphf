import { EventEmitter } from "node:events";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  BrokerSupervisor,
  type BrokerSupervisorConfig,
  buildBrokerEnv,
  type ExecFileRunner,
  runWindowsTaskkill,
} from "../src/main/broker.ts";
import type { Logger, LogPayload } from "../src/main/logger.ts";

const electronMock = vi.hoisted(() => ({
  fork: vi.fn(),
}));

vi.mock("electron", () => ({
  utilityProcess: {
    fork: electronMock.fork,
  },
}));

type ForkProcess = NonNullable<BrokerSupervisorConfig["forkProcess"]>;
type ForkOptions = Parameters<ForkProcess>[2];
type ElectronUtilityProcess = ReturnType<ForkProcess>;
type KillProcess = NonNullable<BrokerSupervisorConfig["killProcess"]>;

class FakeUtilityProcess extends EventEmitter {
  readonly pid: number;
  readonly kill = vi.fn<() => boolean>(() => true);
  readonly postMessage = vi.fn<(message: unknown) => void>();
  readonly stdout = new EventEmitter();
  readonly stderr = new EventEmitter();

  constructor(pid: number) {
    super();
    this.pid = pid;
  }
}

describe("BrokerSupervisor", () => {
  beforeEach(() => {
    vi.useRealTimers();
    electronMock.fork.mockReset();
  });

  it("spawns the broker through utilityProcess.fork with the service name and env allowlist", () => {
    const { forkProcess, forkProcessMock } = createForkMock([new FakeUtilityProcess(4321)]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      envSource: {
        PATH: "/bin",
        HOME: "/Users/fran",
        USER: "fran",
        LANG: "en_US.UTF-8",
        LC_ALL: "C",
        TZ: "UTC",
        SECRET_TOKEN: "must-not-leak",
      },
      forkProcess,
    });

    supervisor.start();

    expect(forkProcessMock).toHaveBeenCalledWith("/app/out/main/broker-stub.js", [], {
      serviceName: "wuphf-broker",
      stdio: "pipe",
      env: {
        PATH: "/bin",
        HOME: "/Users/fran",
        USER: "fran",
        LANG: "en_US.UTF-8",
        LC_ALL: "C",
        TZ: "UTC",
      },
    });
    expect(supervisor.getPid()).toBe(4321);
    expect(supervisor.getStatus()).toBe("starting");
  });

  it("does not fork twice when start is called while a broker is already running", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess, forkProcessMock } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    supervisor.start();

    expect(forkProcessMock).toHaveBeenCalledTimes(1);
    expect(supervisor.getPid()).toBe(4321);
    expect(supervisor.getStatus()).toBe("starting");
  });

  it("logs fork failures before surfacing the startup error", () => {
    const forkError = new Error("fork failed");
    const forkProcess = vi.fn(() => {
      throw forkError;
    }) as unknown as ForkProcess;
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    expect(() => supervisor.start()).toThrow(forkError);

    expect(supervisor.getStatus()).toBe("dead");
    expect(calls).toContainEqual({
      level: "error",
      event: "broker_start_failed",
      payload: {
        error: "fork failed",
        restartCount: 0,
        serviceName: "wuphf-broker",
      },
    });
  });

  it("drains broker stdout and stderr pipes without buffering output", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();

    expect(processHandle.stdout.listenerCount("data")).toBe(1);
    expect(processHandle.stderr.listenerCount("data")).toBe(1);
    expect(() => {
      processHandle.stdout.emit("data", Buffer.from("stdout payload"));
      processHandle.stderr.emit("data", Buffer.from("stderr payload"));
    }).not.toThrow();
  });

  it("binds the default utilityProcess.fork receiver before storing it", async () => {
    const { utilityProcess } = await import("electron");
    const processHandle = new FakeUtilityProcess(2468);
    electronMock.fork.mockImplementation(function (
      this: unknown,
      _entryPath: string,
      _args: readonly string[],
      _options: ForkOptions,
    ): ElectronUtilityProcess {
      expect(this).toBe(utilityProcess);
      return processHandle as unknown as ElectronUtilityProcess;
    });
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
    });

    supervisor.start();

    expect(electronMock.fork).toHaveBeenCalledTimes(1);
    expect(supervisor.getPid()).toBe(2468);
  });

  it("marks the broker dead when stop is called before a process exists", async () => {
    const { forkProcess, forkProcessMock } = createForkMock([]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    await supervisor.stop();

    expect(forkProcessMock).not.toHaveBeenCalled();
    expect(supervisor.getSnapshot()).toEqual({
      status: "dead",
      pid: null,
      restartCount: 0,
    });
  });

  it("updates status when the broker sends a liveness ping", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });

    expect(supervisor.getStatus()).toBe("alive");
  });

  it("logs broker exits with lifecycle-only causal fields before scheduling restart", () => {
    let nowMs = 1_000;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      monotonicNow: () => nowMs,
      firstBackoffMs: 250,
    });

    supervisor.start();
    nowMs = 1_200;
    processHandle.emit("message", { alive: true });
    nowMs = 1_500;
    processHandle.emit("exit", 42, "SIGTERM");

    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_exited",
      payload: {
        pid: 4321,
        exitCode: 42,
        signal: null,
        restartCount: 0,
        uptimeMs: 500,
        lastPingAt: 1_200,
      },
    });
    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_restart_scheduled",
      payload: {
        restartCount: 1,
        backoffMs: 250,
        maxRestartRetries: 5,
      },
    });
  });

  it("ignores stale exits from a process that is no longer current", () => {
    const currentProcess = new FakeUtilityProcess(2002);
    const staleProcess = new FakeUtilityProcess(2001);
    const { forkProcess } = createForkMock([currentProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    invokeHandleExit(supervisor, staleProcess);

    expect(supervisor.getSnapshot()).toEqual({
      status: "starting",
      pid: 2002,
      restartCount: 0,
    });
  });

  it("waits for graceful broker exit before killing", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "linux",
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    expect(processHandle.postMessage).toHaveBeenCalledWith({ type: "shutdown" });
    expect(processHandle.kill).not.toHaveBeenCalled();

    processHandle.emit("exit", 0);
    await stopPromise;

    await vi.advanceTimersByTimeAsync(6_000);
    expect(processHandle.kill).not.toHaveBeenCalled();
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("continues stop escalation when graceful shutdown postMessage fails", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    processHandle.postMessage.mockImplementationOnce(() => {
      throw new Error("message port already closed");
    });
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      platform: "linux",
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    expect(processHandle.postMessage).toHaveBeenCalledWith({ type: "shutdown" });

    await vi.advanceTimersByTimeAsync(5_000);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1_000);
    await stopPromise;

    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    expect(killProcess).toHaveBeenCalledWith(4321, "SIGKILL");
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("requests POSIX termination after the graceful stop window", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "linux",
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    expect(processHandle.postMessage).toHaveBeenCalledWith({ type: "shutdown" });
    await vi.advanceTimersByTimeAsync(4_999);
    expect(processHandle.kill).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    expect(processHandle.kill).toHaveBeenCalledWith();

    processHandle.emit("exit", 0);
    await stopPromise;
    await vi.advanceTimersByTimeAsync(1_000);

    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("force-kills POSIX brokers that ignore graceful and termination windows", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      platform: "linux",
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    await vi.advanceTimersByTimeAsync(5_000);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(999);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    await stopPromise;

    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    expect(killProcess).toHaveBeenCalledWith(4321, "SIGKILL");
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("falls back to handle-bound termination when POSIX SIGKILL fails", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killError = Object.assign(new Error("permission denied"), { code: "EPERM" });
    const killProcess = vi.fn<KillProcess>(() => {
      throw killError;
    });
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      logger,
      platform: "linux",
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(6_000);
    await stopPromise;

    expect(killProcess).toHaveBeenCalledWith(4321, "SIGKILL");
    expect(processHandle.kill).toHaveBeenCalledTimes(2);
    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_posix_sigkill_failed",
      payload: {
        pid: 4321,
        error: "permission denied",
        code: "EPERM",
        restartCount: 0,
      },
    });
  });

  it("uses Windows taskkill after grace and escalates to force after one more second", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkill = vi.fn<
      (pid: number, options: { readonly force: boolean }) => Promise<void>
    >(() => Promise.resolve());
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "win32",
      runWindowsTaskkill,
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    expect(processHandle.postMessage).toHaveBeenCalledWith({ type: "shutdown" });
    expect(runWindowsTaskkill).not.toHaveBeenCalled();
    expect(processHandle.kill).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(4_999);
    expect(runWindowsTaskkill).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1);
    expect(runWindowsTaskkill).toHaveBeenCalledWith(4321, { force: false });
    expect(runWindowsTaskkill).not.toHaveBeenCalledWith(4321, { force: true });

    await vi.advanceTimersByTimeAsync(999);
    expect(runWindowsTaskkill).not.toHaveBeenCalledWith(4321, { force: true });

    await vi.advanceTimersByTimeAsync(1);
    await stopPromise;

    expect(runWindowsTaskkill).toHaveBeenCalledWith(4321, { force: true });
    expect(processHandle.kill).not.toHaveBeenCalled();
  });

  it("clears a pending restart timer when stop is called after a crash", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(1001);
    const { forkProcess, forkProcessMock } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 250,
    });
    supervisor.start();

    processHandle.emit("exit", 1);
    expect(supervisor.getRestartCount()).toBe(1);
    expect(supervisor.getStatus()).toBe("starting");

    await supervisor.stop();
    await vi.advanceTimersByTimeAsync(250);

    expect(forkProcessMock).toHaveBeenCalledTimes(1);
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("restarts crashed brokers with exponential backoff", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const secondProcess = new FakeUtilityProcess(1002);
    const { forkProcess, forkProcessMock } = createForkMock([firstProcess, secondProcess]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      firstBackoffMs: 250,
      maxBackoffMs: 60_000,
    });

    supervisor.start();
    firstProcess.emit("exit", 1);

    expect(supervisor.getRestartCount()).toBe(1);
    expect(supervisor.getStatus()).toBe("starting");
    expect(supervisor.getLastRestartScheduledAtMs()).not.toBeNull();
    await vi.advanceTimersByTimeAsync(249);
    expect(forkProcessMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    expect(forkProcessMock).toHaveBeenCalledTimes(2);
    expect(supervisor.getPid()).toBe(1002);
    expect(calls).toContainEqual({
      level: "info",
      event: "broker_restart_attempt",
      payload: {
        restartCount: 1,
        maxRestartRetries: 5,
      },
    });
  });

  it("keeps scheduled restart fork failures inside the retry policy", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const forkError = new Error("fork retry failed");
    let forkCallCount = 0;
    const forkProcess = vi.fn(
      (
        _entryPath: string,
        _args: readonly string[],
        _options: ForkOptions,
      ): ElectronUtilityProcess => {
        forkCallCount += 1;
        if (forkCallCount === 1) {
          return firstProcess as unknown as ElectronUtilityProcess;
        }
        throw forkError;
      },
    ) as unknown as ForkProcess;
    const onFatal = vi.fn<(reason: string) => void>();
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      firstBackoffMs: 250,
      maxRestartRetries: 1,
      onFatal,
    });

    supervisor.start();
    firstProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(250);

    expect(supervisor.getStatus()).toBe("dead");
    expect(onFatal).toHaveBeenCalledWith(
      "Broker start failed after 1 restart retries: fork retry failed",
    );
    expect(calls).toContainEqual({
      level: "error",
      event: "broker_restart_start_failed",
      payload: {
        error: "fork retry failed",
        restartCount: 1,
        maxRestartRetries: 1,
        serviceName: "wuphf-broker",
      },
    });
  });

  it("uses firstBackoffMs as the first wait and then doubles each retry", async () => {
    vi.useFakeTimers();
    const processes = [
      new FakeUtilityProcess(1001),
      new FakeUtilityProcess(1002),
      new FakeUtilityProcess(1003),
      new FakeUtilityProcess(1004),
    ];
    const { forkProcess, forkProcessMock } = createForkMock(processes);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 250,
      maxBackoffMs: 60_000,
    });

    supervisor.start();

    for (const [index, waitMs] of [250, 500, 1_000].entries()) {
      processes[index]?.emit("exit", 1);
      await vi.advanceTimersByTimeAsync(waitMs - 1);
      expect(forkProcessMock).toHaveBeenCalledTimes(index + 1);
      await vi.advanceTimersByTimeAsync(1);
      expect(forkProcessMock).toHaveBeenCalledTimes(index + 2);
    }
  });

  it("stops restarting after the max retry cap and surfaces fatal state", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const secondProcess = new FakeUtilityProcess(1002);
    const thirdProcess = new FakeUtilityProcess(1003);
    const { forkProcess, forkProcessMock } = createForkMock([
      firstProcess,
      secondProcess,
      thirdProcess,
    ]);
    const onFatal = vi.fn<(reason: string) => void>();
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      firstBackoffMs: 1,
      maxRestartRetries: 2,
      onFatal,
    });

    supervisor.start();
    firstProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(1);
    secondProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(2);
    thirdProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(4);

    expect(forkProcessMock).toHaveBeenCalledTimes(3);
    expect(onFatal).toHaveBeenCalledWith("Broker exited after 2 restart retries");
    expect(supervisor.getStatus()).toBe("dead");
    expect(supervisor.getRestartCount()).toBe(2);
    expect(calls).toContainEqual({
      level: "error",
      event: "broker_restart_cap_reached",
      payload: {
        restartCount: 2,
        maxRestartRetries: 2,
      },
    });
  });

  it("resets restartCount after the broker survives the stability window", async () => {
    vi.useFakeTimers();
    let nowMs = 0;
    const processes = [
      new FakeUtilityProcess(1001),
      new FakeUtilityProcess(1002),
      new FakeUtilityProcess(1003),
      new FakeUtilityProcess(1004),
      new FakeUtilityProcess(1005),
      new FakeUtilityProcess(1006),
      new FakeUtilityProcess(1007),
    ];
    const { forkProcess, forkProcessMock } = createForkMock(processes);
    const onFatal = vi.fn<(reason: string) => void>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 1,
      maxRestartRetries: 5,
      monotonicNow: () => nowMs,
      onFatal,
      stabilityWindowMs: 60_000,
    });

    supervisor.start();
    for (let index = 0; index < 5; index += 1) {
      processes[index]?.emit("exit", 1);
      await vi.advanceTimersByTimeAsync(2 ** index);
    }

    expect(forkProcessMock).toHaveBeenCalledTimes(6);
    expect(supervisor.getRestartCount()).toBe(5);

    processes[5]?.emit("message", { alive: true });
    nowMs = 65_000;
    processes[5]?.emit("exit", 1);

    expect(onFatal).not.toHaveBeenCalled();
    expect(supervisor.getRestartCount()).toBe(1);
    expect(supervisor.getStatus()).toBe("starting");
  });

  it("reports alive brokers as unresponsive after the liveness timeout", () => {
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      livenessStaleMs: 5_000,
      monotonicNow: () => nowMs,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });
    expect(supervisor.getStatus()).toBe("alive");

    nowMs = 5_001;

    expect(supervisor.getStatus()).toBe("unresponsive");
  });

  it("logs the first missed broker liveness ping without repeating on later status reads", () => {
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      livenessStaleMs: 5_000,
      monotonicNow: () => nowMs,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });
    nowMs = 5_001;

    expect(supervisor.getStatus()).toBe("unresponsive");
    expect(supervisor.getStatus()).toBe("unresponsive");

    expect(calls.filter((call) => call.event === "broker_ping_missed")).toEqual([
      {
        level: "warn",
        event: "broker_ping_missed",
        payload: {
          pid: 4321,
          lastPingAt: 0,
          livenessAgeMs: 5_001,
          restartCount: 0,
        },
      },
    ]);
  });

  it("logs Windows taskkill failures without failing broker shutdown", async () => {
    vi.useFakeTimers();
    const taskkillError = Object.assign(new Error("taskkill missing"), { code: "ENOENT" });
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkill = vi.fn<
      (pid: number, options: { readonly force: boolean }) => Promise<void>
    >(() => Promise.reject(taskkillError));
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      platform: "win32",
      runWindowsTaskkill,
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(5_000);
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(1_000);
    await Promise.resolve();
    await stopPromise;

    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_taskkill_failed",
      payload: {
        pid: 4321,
        force: false,
        error: "taskkill missing",
        code: "ENOENT",
      },
    });
    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_taskkill_failed",
      payload: {
        pid: 4321,
        force: true,
        error: "taskkill missing",
        code: "ENOENT",
      },
    });

    const errorWithoutCode = new Error("taskkill refused");
    runWindowsTaskkill.mockImplementationOnce(() => Promise.reject(errorWithoutCode));
    const secondProcessHandle = new FakeUtilityProcess(4322);
    const { forkProcess: secondForkProcess } = createForkMock([secondProcessHandle]);
    const secondSupervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess: secondForkProcess,
      logger,
      platform: "win32",
      runWindowsTaskkill,
      stopGraceMs: 5_000,
    });
    secondSupervisor.start();

    const secondStopPromise = secondSupervisor.stop();
    await vi.advanceTimersByTimeAsync(5_000);
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(1_000);
    await Promise.resolve();
    await secondStopPromise;

    expect(calls).toContainEqual({
      level: "warn",
      event: "broker_taskkill_failed",
      payload: {
        pid: 4322,
        force: false,
        error: "taskkill refused",
        code: null,
      },
    });
  });

  it("uses the noop logger silently when no logger is provided", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    expect(() => supervisor.start()).not.toThrow();
    processHandle.emit("message", { alive: true });
    expect(supervisor.getStatus()).toBe("alive");
  });

  it("does not reschedule restart when start() throws on a fork-queue exhaustion during retry", async () => {
    vi.useFakeTimers();
    let nowMs = 0;
    const monotonicNow = (): number => nowMs;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      monotonicNow,
      maxRestartRetries: 1,
      firstBackoffMs: 100,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });
    processHandle.emit("exit", 1);

    nowMs = 100;
    await vi.advanceTimersByTimeAsync(100);
    await Promise.resolve();

    expect(calls.some((call) => call.event === "broker_restart_start_failed")).toBe(true);
    expect(supervisor.getStatus()).toBe("dead");
  });
});

describe("runWindowsTaskkill", () => {
  it("constructs the normal and force taskkill argv", async () => {
    const calls: { readonly file: string; readonly args: readonly string[] }[] = [];
    const execFileRunner = vi.fn<ExecFileRunner>((file, args, callback) => {
      calls.push({ file, args: [...args] });
      callback(null);
    });

    await expect(runWindowsTaskkill(4321, { force: false }, execFileRunner)).resolves.toBe(
      undefined,
    );
    await expect(runWindowsTaskkill(4321, { force: true }, execFileRunner)).resolves.toBe(
      undefined,
    );

    expect(calls).toEqual([
      { file: "taskkill", args: ["/pid", "4321", "/T"] },
      { file: "taskkill", args: ["/pid", "4321", "/T", "/F"] },
    ]);
  });

  it("rejects through the default execFile runner on non-Windows hosts", async () => {
    if (process.platform === "win32") {
      return;
    }

    await expect(runWindowsTaskkill(4321, { force: false })).rejects.toBeInstanceOf(Error);
  });
});

describe("buildBrokerEnv", () => {
  it("passes through only the explicit broker env allowlist", () => {
    expect(
      buildBrokerEnv({
        PATH: "/bin",
        HOME: "/Users/fran",
        USER: "fran",
        LANG: "en_US.UTF-8",
        LC_ALL: "C",
        TZ: "UTC",
        AWS_SECRET_ACCESS_KEY: "must-not-leak",
      }),
    ).toEqual({
      PATH: "/bin",
      HOME: "/Users/fran",
      USER: "fran",
      LANG: "en_US.UTF-8",
      LC_ALL: "C",
      TZ: "UTC",
    });
  });
});

function createForkMock(processes: readonly FakeUtilityProcess[]): {
  readonly forkProcess: ForkProcess;
  readonly forkProcessMock: ReturnType<
    typeof vi.fn<
      (entryPath: string, args: readonly string[], options: ForkOptions) => ElectronUtilityProcess
    >
  >;
} {
  const queue = [...processes];
  const forkProcessMock = vi.fn(
    (
      _entryPath: string,
      _args: readonly string[],
      _options: ForkOptions,
    ): ElectronUtilityProcess => {
      const processHandle = queue.shift();
      if (processHandle === undefined) {
        throw new Error("Unexpected extra utilityProcess.fork call (test queue exhausted)");
      }
      return processHandle as unknown as ElectronUtilityProcess;
    },
  );

  return {
    forkProcess: forkProcessMock as unknown as ForkProcess,
    forkProcessMock,
  };
}

function invokeHandleExit(supervisor: BrokerSupervisor, processHandle: FakeUtilityProcess): void {
  const handleExit = Reflect.get(supervisor, "handleExit");
  if (typeof handleExit !== "function") {
    throw new Error("Expected BrokerSupervisor.handleExit to exist");
  }

  Reflect.apply(handleExit, supervisor, [processHandle, null, null]);
}

interface LogCall {
  readonly level: "debug" | "info" | "warn" | "error";
  readonly event: string;
  readonly payload: LogPayload | undefined;
}

function createMemoryLogger(): { readonly logger: Logger; readonly calls: LogCall[] } {
  const calls: LogCall[] = [];
  const push = (level: LogCall["level"], event: string, payload?: LogPayload): void => {
    calls.push({ level, event, payload });
  };

  return {
    calls,
    logger: {
      debug: (event, payload) => push("debug", event, payload),
      info: (event, payload) => push("info", event, payload),
      warn: (event, payload) => push("warn", event, payload),
      error: (event, payload) => push("error", event, payload),
    },
  };
}

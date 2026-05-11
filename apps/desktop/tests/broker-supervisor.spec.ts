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
      brokerUrl: null,
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

  it("captures brokerUrl from a {ready} message and exposes it via getSnapshot()", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:54321" });

    expect(supervisor.getSnapshot().brokerUrl).toBe("http://127.0.0.1:54321");
  });

  it("rejects {ready} with malformed brokerUrl (port 0, non-loopback, wrong scheme)", () => {
    // Triangulation pass 2: readReadyMessage now validates via the
    // protocol's isBrokerUrl brand check. A subprocess sending a
    // non-canonical URL is dropped at the IPC boundary instead of being
    // handed downstream as a "string" the supervisor would later trust
    // as a fetch origin.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:0" });
    processHandle.emit("message", { ready: true, brokerUrl: "https://127.0.0.1:8080" });
    processHandle.emit("message", { ready: true, brokerUrl: "http://evil.com:8080" });
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1" });
    expect(supervisor.getSnapshot().brokerUrl).toBeNull();
  });

  it("ignores malformed {ready} messages (missing url, wrong shape)", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true });
    processHandle.emit("message", { ready: false, brokerUrl: "http://127.0.0.1:1" });
    processHandle.emit("message", { ready: true, brokerUrl: "" });
    processHandle.emit("message", { ready: true, brokerUrl: 42 });

    expect(supervisor.getSnapshot().brokerUrl).toBeNull();
  });

  it("whenReady() resolves with the brokerUrl once the broker reports ready", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    const ready = supervisor.whenReady();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });

    await expect(ready).resolves.toBe("http://127.0.0.1:7891");
  });

  it("whenReady() resolves immediately if the broker is already ready", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });

    await expect(supervisor.whenReady()).resolves.toBe("http://127.0.0.1:7891");
  });

  it("whenReady() rejects when stop() runs before a ready message arrives", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    const ready = supervisor.whenReady();
    setImmediate(() => processHandle.emit("exit", 0, null));
    await supervisor.stop();
    await expect(ready).rejects.toThrow(/broker_stopped/);
  });

  it("whenReady() rejects synchronously when called after a completed stop()", async () => {
    const { forkProcess } = createForkMock([]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    await supervisor.stop();
    // After a clean stop with no broker process, a fresh whenReady() would
    // otherwise add a waiter that nothing resolves or rejects — promise leak.
    await expect(supervisor.whenReady()).rejects.toThrow(/broker_stopped/);
  });

  it("whenReady() resolves all pending waiters when ready arrives (fanout)", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    supervisor.start();
    const a = supervisor.whenReady();
    const b = supervisor.whenReady();
    const c = supervisor.whenReady();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });

    await expect(Promise.all([a, b, c])).resolves.toEqual([
      "http://127.0.0.1:7891",
      "http://127.0.0.1:7891",
      "http://127.0.0.1:7891",
    ]);
  });

  it("clears brokerUrl when the broker process exits and re-captures on restart", () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1);
    const secondProcess = new FakeUtilityProcess(2);
    const { forkProcess } = createForkMock([firstProcess, secondProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 10,
    });

    supervisor.start();
    firstProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:1" });
    expect(supervisor.getSnapshot().brokerUrl).toBe("http://127.0.0.1:1");

    firstProcess.emit("exit", 0, null);
    expect(supervisor.getSnapshot().brokerUrl).toBeNull();

    vi.advanceTimersByTime(10);
    secondProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:2" });
    expect(supervisor.getSnapshot().brokerUrl).toBe("http://127.0.0.1:2");
    vi.useRealTimers();
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
      brokerUrl: null,
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

  it("uses the noop logger silently when no logger is provided (covers info/warn/error paths)", () => {
    vi.useFakeTimers();
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      monotonicNow: () => nowMs,
      maxRestartRetries: 0,
      livenessStaleMs: 1_000,
    });

    // info: broker_starting + broker_started + broker_alive
    expect(() => supervisor.start()).not.toThrow();
    processHandle.emit("message", { alive: true });
    expect(supervisor.getStatus()).toBe("alive");

    // warn: broker_ping_missed (liveness staleness)
    nowMs = 2_000;
    expect(supervisor.getStatus()).toBe("unresponsive");

    // error: broker_restart_cap_reached on exit (max retries = 0)
    processHandle.emit("exit", 1);
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("hits the fatal cap when start() throws on the only remaining retry", async () => {
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

  it("reschedules another restart when start() throws and retries remain (covers handleRestartStartFailure fall-through)", async () => {
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
      maxRestartRetries: 3,
      firstBackoffMs: 100,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });
    processHandle.emit("exit", 1);

    nowMs = 100;
    await vi.advanceTimersByTimeAsync(100);
    await Promise.resolve();

    // Restart attempt fired, queue exhausted, start() threw,
    // handleRestartStartFailure fell through to scheduleRestart() because
    // restartCount < maxRestartRetries and !this.stopping. The supervisor
    // should now have a NEW pending restart timer scheduled.
    expect(calls.some((call) => call.event === "broker_restart_start_failed")).toBe(true);
    expect(calls.some((call) => call.event === "broker_restart_cap_reached")).toBe(false);
    expect(
      calls.filter((call) => call.event === "broker_restart_scheduled").length,
    ).toBeGreaterThanOrEqual(2);
    expect(supervisor.getStatus()).toBe("starting");

    // Tear down the lingering retry loop so the test does not leak timers.
    await supervisor.stop();
  });

  it("does not restart when the timer callback fires after stop() (closes the queued-callback race)", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(4321);
    const secondProcess = new FakeUtilityProcess(5432);
    const { forkProcess } = createForkMock([firstProcess, secondProcess]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      maxRestartRetries: 3,
      firstBackoffMs: 100,
      stopGraceMs: 0,
      forceStopGraceMs: 0,
    });

    supervisor.start();
    firstProcess.emit("message", { alive: true });

    // Process exits → restart timer is scheduled.
    firstProcess.emit("exit", 1);
    expect(calls.some((call) => call.event === "broker_restart_scheduled")).toBe(true);

    // Simulate the real-Node race: the timer callback was already on the
    // event queue when stop() runs, so clearTimeout cannot cancel it.
    // Make clearTimeout a no-op so the deterministic fake-timer scheduler
    // still fires the callback after stop() completed.
    const clearSpy = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => undefined);

    await supervisor.stop();
    expect(supervisor.getStatus()).toBe("dead");

    // Advance past the restart backoff so the queued callback fires post-stop.
    await vi.advanceTimersByTimeAsync(100);
    await Promise.resolve();

    clearSpy.mockRestore();

    // The entry-guard MUST suppress the restart: no fork, no attempt log,
    // status stays dead, and the skipped event records the suppression.
    expect(forkProcess).toHaveBeenCalledTimes(1);
    expect(calls.some((call) => call.event === "broker_restart_attempt")).toBe(false);
    expect(
      calls.find(
        (call) =>
          call.event === "broker_restart_skipped" &&
          (call.payload as { reason?: string } | undefined)?.reason === "stopping",
      ),
    ).toBeDefined();
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("ignores the redundant explicit settle() when the broker's exit fires during force-stop", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    // killProcess(SIGKILL) synchronously surfaces the broker's exit BEFORE
    // forceStop returns control to the inner timer's explicit settle() call.
    // Settle then runs twice in the same tick: first via the once-listener
    // (settled=true, off, resolve), then via the explicit settle() inside
    // the timer callback. The second invocation must hit the `if (settled)
    // return` guard at broker.ts:227 — branch coverage target.
    const killProcess = vi.fn<KillProcess>((_pid, signal) => {
      if (signal === "SIGKILL") {
        processHandle.emit("exit", -1);
      }
    });
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      killProcess,
      stopGraceMs: 5_000,
      forceStopGraceMs: 1_000,
    });

    supervisor.start();
    processHandle.emit("message", { alive: true });

    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(5_000);
    await vi.advanceTimersByTimeAsync(1_000);
    await stopPromise;

    const stoppedLogs = calls.filter((call) => call.event === "broker_stopped");
    expect(stoppedLogs).toHaveLength(1);
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

describe("BrokerSupervisor — review-pass-2 regressions", () => {
  it("does not throw when the real StructuredLogger handles a {ready} message", async () => {
    // BLOCK regression: validatePayloadKey at logger.ts:188 bans payload
    // keys containing "url", so logging `brokerUrl` (the old code) threw
    // UnsafeLogPayloadError BEFORE flushReadyWaiters fired — whenReady()
    // hung forever in packaged builds and no window opened. Tests had
    // been using a memory logger that bypassed the validator. This test
    // wires the real StructuredLogger and asserts the full ready handshake
    // does not throw and the waiter resolves.
    const { StructuredLogger } = await import("../src/main/logger.ts");
    const lines: string[] = [];
    const structured = new StructuredLogger({
      logDirectory: () => null, // do not touch the filesystem
      consoleWriter: (_level, line) => lines.push(line),
    });
    const realLogger = structured.forModule("broker");

    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger: realLogger,
    });

    supervisor.start();
    const ready = supervisor.whenReady();
    expect(() =>
      processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" }),
    ).not.toThrow();
    await expect(ready).resolves.toBe("http://127.0.0.1:7891");

    const readyLog = lines.find((line) => line.includes('"event":"broker_ready"'));
    expect(readyLog).toBeDefined();
    expect(readyLog).not.toContain("brokerUrl");
    expect(readyLog).toContain('"port":7891');
  });

  it("drops late {ready} from a previous broker process after restart", () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1);
    const secondProcess = new FakeUtilityProcess(2);
    const { forkProcess } = createForkMock([firstProcess, secondProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 10,
    });

    supervisor.start();
    firstProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:1" });
    firstProcess.emit("exit", 0, null);
    vi.advanceTimersByTime(10);
    secondProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:2" });
    expect(supervisor.getSnapshot().brokerUrl).toBe("http://127.0.0.1:2");

    // Stale message from the now-dead first process MUST NOT overwrite the
    // current brokerUrl. The supervisor checks message provenance against
    // the closure-captured handle.
    // Use a syntactically valid loopback URL with a different port — the
    // protocol BrokerUrl brand rejects non-numeric ports, so a "STALE"
    // sentinel would be dropped by URL validation before the stale-sender
    // guard runs. Port 9999 exercises the actual stale-sender path.
    firstProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:9999" });
    expect(supervisor.getSnapshot().brokerUrl).toBe("http://127.0.0.1:2");

    vi.useRealTimers();
  });

  it("ignores {ready} that arrives during stop()'s shutdown window", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      stopGraceMs: 10,
      forceStopGraceMs: 10,
    });

    supervisor.start();
    const ready = supervisor.whenReady();

    // Race: emit {ready} AFTER stop() flips `stopping=true` but BEFORE the
    // subprocess exit settles stop(). Without the stopping-gate, the late
    // ready would resolve waiters and set brokerUrl during shutdown.
    const stopPromise = supervisor.stop();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:9999" });
    setImmediate(() => processHandle.emit("exit", 0, null));
    await stopPromise;

    await expect(ready).rejects.toThrow(/broker_stopped/);
    expect(supervisor.getSnapshot().brokerUrl).toBeNull();
  });

  it("kills the wedged subprocess and counts against the restart cap on broker_ready_timeout", () => {
    vi.useFakeTimers();
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      killProcess,
      monotonicNow: () => nowMs,
      startupTimeoutMs: 500,
      firstBackoffMs: 5,
      maxRestartRetries: 0,
    });

    supervisor.start();
    // Watchdog ceiling — no `{ ready }` arrives.
    nowMs = 500;
    vi.advanceTimersByTime(500);
    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(true);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("drops a {ready} that races between watchdog termination and exit", () => {
    // Triangulation pass-2 regression: the watchdog calls
    // requestProcessTermination but doesn't fence the subprocess against
    // late messages. Without the timed-out marker, a queued {ready}
    // arriving AFTER the watchdog decides the fork is wedged but BEFORE
    // exit lands would publish brokerUrl for a process we're killing.
    vi.useFakeTimers();
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      monotonicNow: () => nowMs,
      startupTimeoutMs: 500,
      forceStopGraceMs: 1_000,
      maxRestartRetries: 0,
    });

    supervisor.start();
    nowMs = 500;
    vi.advanceTimersByTime(500);
    // Watchdog has fired and called requestProcessTermination. A late
    // {ready} now arrives — must not set brokerUrl.
    processHandle.emit("message", {
      ready: true,
      brokerUrl: "http://127.0.0.1:9999",
    });
    expect(supervisor.getSnapshot().brokerUrl).toBeNull();
    vi.useRealTimers();
  });

  it("force-stops the wedged subprocess after forceStopGraceMs if SIGTERM doesn't take", () => {
    // Triangulation pass-2 regression: a subprocess wedged so deeply
    // that SIGTERM is ignored (uninterruptible sleep, SIGSTOP, kernel
    // bug) would never exit, so handleExit never fires, so the restart
    // cycle never starts, so whenReady() waiters hang past the cap.
    // The watchdog must escalate to SIGKILL after the same grace stop()
    // uses.
    vi.useFakeTimers();
    let nowMs = 0;
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      monotonicNow: () => nowMs,
      startupTimeoutMs: 500,
      forceStopGraceMs: 1_000,
      maxRestartRetries: 0,
    });

    supervisor.start();
    nowMs = 500;
    vi.advanceTimersByTime(500);
    // SIGTERM-equivalent fired (utilityProcess.kill()); the subprocess
    // is ignoring it. Advance past the force grace.
    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    nowMs = 1_500;
    vi.advanceTimersByTime(1_000);
    // POSIX path: forceStop invokes killProcess(pid, "SIGKILL") OR
    // utilityProcess.kill() again, depending on whether pid is null.
    // Either way, the force path is exercised — assert SOMETHING was
    // called beyond the initial SIGTERM.
    const totalForceCalls = killProcess.mock.calls.length + processHandle.kill.mock.calls.length;
    expect(totalForceCalls).toBeGreaterThan(1);
    vi.useRealTimers();
  });

  it("clears the startup timer when {ready} arrives in time", () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      startupTimeoutMs: 500,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });
    vi.advanceTimersByTime(10_000);
    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(false);
    vi.useRealTimers();
  });

  it("forwards a broker_log message through the desktop logger, dropping banned keys", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("message", {
      broker_log: "info",
      event: "listener_started",
      payload: { port: 7891, url: "http://127.0.0.1:7891" },
    });

    // Expect the listener_started forward with safe keys only. The `url`
    // key is banned (fragment match in logger.ts) so it must be dropped
    // and accounted for via droppedKeys=1.
    expect(calls).toContainEqual({
      level: "info",
      event: "broker_listener_started",
      payload: { port: 7891, droppedKeys: 1 },
    });
  });

  it("drops a broker_log with an invalid event name and emits a structured warn", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("message", {
      broker_log: "info",
      event: "Has Spaces And UPPER",
      payload: {},
    });

    expect(calls.some((c) => c.event === "broker_subprocess_log_invalid_event")).toBe(true);
  });

  it("subscribeReady fires on every {ready}, supports unsubscribe, and doesn't fire from stale forks", () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1);
    const secondProcess = new FakeUtilityProcess(2);
    const { forkProcess } = createForkMock([firstProcess, secondProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      firstBackoffMs: 10,
    });
    const seen: string[] = [];
    const unsubscribe = supervisor.subscribeReady((url) => {
      seen.push(url);
    });

    supervisor.start();
    firstProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:1" });
    expect(seen).toEqual(["http://127.0.0.1:1"]);

    firstProcess.emit("exit", 0, null);
    vi.advanceTimersByTime(10);
    secondProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:2" });
    expect(seen).toEqual(["http://127.0.0.1:1", "http://127.0.0.1:2"]);

    // Stale message from the dead first fork must not fire listeners.
    // Use a syntactically valid loopback URL with a different port — the
    // protocol BrokerUrl brand rejects non-numeric ports, so a "STALE"
    // sentinel would be dropped by URL validation before the stale-sender
    // guard runs. Port 9999 exercises the actual stale-sender path.
    firstProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:9999" });
    expect(seen).toEqual(["http://127.0.0.1:1", "http://127.0.0.1:2"]);

    unsubscribe();
    secondProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:2-replay" });
    expect(seen).toEqual(["http://127.0.0.1:1", "http://127.0.0.1:2"]);

    vi.useRealTimers();
  });

  it("ready handshake still flushes waiters when the broker_ready logger throws", async () => {
    // CodeRabbit pass-3 regression: the previous ordering called
    // logger.info("broker_ready", ...) BEFORE flushReadyWaiters. A throw
    // from the logger (banned payload key, IO error, instrumentation
    // bug) aborted the message handler, leaving whenReady() waiters
    // hanging forever. The new ordering flushes first, then logs inside
    // a try/catch — so a logger fault can never regress readiness.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    // Throw only on the broker_ready event — other startup logs
    // (broker_starting, etc.) must still succeed so we reach the ready
    // path. The point of the test is that flushReadyWaiters runs even
    // when the broker_ready logger call specifically faults.
    const throwingLogger: Logger = {
      info: vi.fn().mockImplementation((event: string, _payload?: LogPayload) => {
        if (event === "broker_ready") {
          throw new Error("logger_kaboom");
        }
      }),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger: throwingLogger,
    });

    const ready = supervisor.whenReady();
    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });
    // whenReady must resolve even though the logger throws on broker_ready.
    await expect(ready).resolves.toBe("http://127.0.0.1:7891");
  });

  it("notifyReadyListeners snapshots — unsubscribe-during-callback doesn't skip the next listener", () => {
    // CodeRabbit pass-3 regression: notifyReadyListeners used to iterate
    // the live this.readyListeners array. A listener that unsubscribed
    // itself (or another listener) inside its callback would shift the
    // array and cause the for…of to skip the next slot. The new
    // implementation iterates a snapshot, so the third listener runs
    // regardless of mid-iteration unsubscribe.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    const seen: string[] = [];
    const unsubscribeA = supervisor.subscribeReady(() => {
      seen.push("A");
      // A unsubscribes itself mid-fire. With a live-array iterate the
      // second listener (B) would be skipped because indices shift.
      unsubscribeA();
    });
    supervisor.subscribeReady(() => {
      seen.push("B");
    });
    supervisor.subscribeReady(() => {
      seen.push("C");
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });

    expect(seen).toEqual(["A", "B", "C"]);
  });

  it("subscribeReady listener that throws does not break subsequent listeners or supervisor", () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });
    const seen: string[] = [];
    supervisor.subscribeReady(() => {
      throw new Error("listener_kaboom");
    });
    supervisor.subscribeReady((url) => {
      seen.push(url);
    });

    supervisor.start();
    expect(() =>
      processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:9" }),
    ).not.toThrow();
    expect(seen).toEqual(["http://127.0.0.1:9"]);
    expect(calls.some((c) => c.event === "broker_ready_listener_threw")).toBe(true);
  });

  it("ignores broker_log messages that arrive after stop()", async () => {
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      stopGraceMs: 10,
      forceStopGraceMs: 10,
    });

    supervisor.start();
    const stopPromise = supervisor.stop();
    processHandle.emit("message", {
      broker_log: "info",
      event: "listener_stopped",
      payload: {},
    });
    setImmediate(() => processHandle.emit("exit", 0, null));
    await stopPromise;

    // The stopping-gate above the broker_log handler should drop the
    // message entirely — no broker_listener_stopped should appear.
    expect(calls.some((c) => c.event === "broker_listener_stopped")).toBe(false);
  });

  // ────────────────────────────────────────────────────────────────────
  // Coverage backfill: the branches below are each one-arm conditionals
  // not naturally reached by the higher-level tests. Each maps to a
  // documented defensive guard in broker.ts; the tests pin one path per
  // guard so a future refactor doesn't quietly delete one.
  // ────────────────────────────────────────────────────────────────────

  it("alive after first alive does not re-emit broker_alive", () => {
    // Covers the false-arm of "status !== alive" in the alive handler:
    // the SECOND alive message must not re-emit broker_alive.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });
    processHandle.emit("message", { alive: true });
    processHandle.emit("message", { alive: true });

    const aliveCount = calls.filter((c) => c.event === "broker_alive").length;
    expect(aliveCount).toBe(1);
  });

  it("whenReady() rejects immediately when fatalReason is set", async () => {
    // Covers the fatalReason arm in whenReady(). Set the field directly
    // so the test stays focused on whenReady's behavior — the natural
    // paths into fatalReason (restart cap hit, exit cap hit) are
    // covered by their own dedicated tests.
    const { forkProcess } = createForkMock([]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });
    Reflect.set(supervisor, "fatalReason", "synthetic_fatal_for_test");

    await expect(supervisor.whenReady()).rejects.toThrow(/synthetic_fatal/);
  });

  it("subscribeReady unsubscribe is a no-op when called twice", () => {
    // Covers the false-arm of "idx >= 0" in the unsubscribe closure.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    const unsubscribe = supervisor.subscribeReady(() => {});
    unsubscribe();
    // Second unsubscribe — listener already removed; idx returns -1 and
    // the splice is skipped. Must not throw.
    expect(() => unsubscribe()).not.toThrow();
  });

  it("schedules a restart but logs reason='fatal' when fatalReason is set", () => {
    // Covers the ternary `stopping ? "stopping" : "fatal"` false-arm in
    // the restart-skipped log payload. We need scheduleRestart to fire
    // when fatalReason is set but stopping is false — that happens
    // briefly between handleExit (which sets fatalReason via the cap)
    // and the next scheduleRestart firing… in practice the cap means
    // scheduleRestart isn't called. Reach it directly via Reflect.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      firstBackoffMs: 1,
    });

    // Force fatalReason directly so we observe the "fatal" branch of
    // the reason ternary without also being in the stopping state.
    Reflect.set(supervisor, "fatalReason", "test_fatal");
    vi.useFakeTimers();
    const scheduleRestart = Reflect.get(supervisor, "scheduleRestart") as () => void;
    Reflect.apply(scheduleRestart, supervisor, []);
    vi.advanceTimersByTime(10);

    const skipped = calls.find((c) => c.event === "broker_restart_skipped");
    expect(skipped).toBeDefined();
    expect(skipped?.payload?.["reason"]).toBe("fatal");
    vi.useRealTimers();
  });

  it("startup-timeout watchdog bails when the closure-captured fork already settled", () => {
    // Covers the early-return arm of the startup-timer body: any of
    // (brokerProcess changed, brokerUrl set, stopping flipped,
    // fatalReason set) makes the timer fire as a no-op. Flip
    // `stopping` after the timer is armed but BEFORE the timer fires —
    // bypasses clearStartupTimer (which stop() would normally call) so
    // the timer reaches the bail-out branch organically.
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      startupTimeoutMs: 500,
    });

    supervisor.start();
    Reflect.set(supervisor, "stopping", true);
    vi.advanceTimersByTime(500);

    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(false);
    // The kill was never called because the bail returned early.
    expect(processHandle.kill).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("force-timer bails when brokerProcess has cycled to a new fork", () => {
    // Covers the false arm of "this.brokerProcess === brokerProcess" in
    // the force-stop timer body. Arm the force timer for fork A by
    // letting startup timeout, then substitute brokerProcess directly
    // BEFORE the force timer fires. Reflect.set avoids clearStartupTimer
    // (which any natural "restart" path would call and clear the force
    // timer along with it).
    vi.useFakeTimers();
    let nowMs = 0;
    const first = new FakeUtilityProcess(1);
    const second = new FakeUtilityProcess(2);
    const { forkProcess } = createForkMock([first]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      monotonicNow: () => nowMs,
      startupTimeoutMs: 500,
      forceStopGraceMs: 1_000,
    });

    supervisor.start();
    nowMs = 500;
    vi.advanceTimersByTime(500); // startup fires → SIGTERM + force timer armed
    expect(first.kill).toHaveBeenCalledTimes(1);
    // Swap brokerProcess out from under the force timer's closure.
    Reflect.set(supervisor, "brokerProcess", second);
    nowMs = 1_500;
    vi.advanceTimersByTime(1_000);
    // First fork must not have been killed a SECOND time — force timer
    // bailed because brokerProcess no longer matches the closure capture.
    expect(first.kill).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("clearStartupTimer is a no-op when no timer is armed", () => {
    // Covers the false-arm of "startupTimer !== null" / "startupForceTimer !== null"
    // in clearStartupTimer. Reach it directly so we don't have to race a
    // real timer's lifecycle.
    const { forkProcess } = createForkMock([]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });
    const clear = Reflect.get(supervisor, "clearStartupTimer") as () => void;
    expect(() => Reflect.apply(clear, supervisor, [])).not.toThrow();
    // Same call again is still a no-op (idempotent).
    expect(() => Reflect.apply(clear, supervisor, [])).not.toThrow();
  });

  it("clearStartupTimer clears an armed force timer when stop() runs mid-timeout", async () => {
    // Covers the true-arm of "startupForceTimer !== null" inside
    // clearStartupTimer — reachable only when the startup watchdog has
    // already fired (arming the force timer) but stop() runs before the
    // force timer fires.
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      startupTimeoutMs: 500,
      forceStopGraceMs: 5_000,
      stopGraceMs: 10,
    });

    supervisor.start();
    vi.advanceTimersByTime(500); // startup fires → arms force timer
    // At this point startupForceTimer is set. stop() will call
    // clearStartupTimer, which clears it (lines 692-693).
    const stopPromise = supervisor.stop();
    setImmediate(() => processHandle.emit("exit", 0, null));
    await vi.advanceTimersByTimeAsync(20);
    await stopPromise;
    vi.useRealTimers();
  });

  it("forwardBrokerLog emits without droppedKeys when every key is safe", () => {
    // Covers the false-arm of "droppedKeyCount > 0" in forwardBrokerLog
    // — payload made entirely of safe keys must NOT carry a `droppedKeys`
    // field (otherwise downstream consumers infer redaction happened
    // when it didn't).
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("message", {
      broker_log: "info",
      event: "listener_started",
      payload: { port: 7891, restartCount: 0 },
    });

    const forwarded = calls.find((c) => c.event === "broker_listener_started");
    expect(forwarded).toBeDefined();
    expect(forwarded?.payload).toEqual({ port: 7891, restartCount: 0 });
    expect(forwarded?.payload).not.toHaveProperty("droppedKeys");
  });

  it("handleExit reports null uptimeMs when startedAtMs was cleared before the exit fired", () => {
    // Covers the null-arm of `startedAtMs === null ? null : nowMs - startedAtMs`
    // in handleExit. Set up the supervisor with a brokerProcess (so the
    // sender-identity gate passes) but null startedAtMs (the path where
    // a stop() raced ahead of the exit handler and cleared the field).
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });
    Reflect.set(supervisor, "brokerProcess", processHandle);
    Reflect.set(supervisor, "startedAtMs", null);

    invokeHandleExit(supervisor, processHandle);
    const exited = calls.find((c) => c.event === "broker_exited");
    expect(exited).toBeDefined();
    expect(exited?.payload?.["uptimeMs"]).toBeNull();
  });

  it("startup-timeout reports null uptimeMs when startedAtMs was never set", () => {
    // Covers the null-arm of the same ternary inside the startup-timeout
    // watchdog body. Force the path: arm the timer with a supervisor
    // whose startedAtMs is null (override directly).
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      startupTimeoutMs: 500,
      maxRestartRetries: 0,
    });

    supervisor.start();
    // Override startedAtMs to null AFTER start arms the timer. The
    // timer body's `startedAtMs` snapshot will see null and emit the
    // ternary's null arm.
    Reflect.set(supervisor, "startedAtMs", null);
    vi.advanceTimersByTime(500);

    const timeout = calls.find((c) => c.event === "broker_ready_timeout");
    expect(timeout).toBeDefined();
    expect(timeout?.payload?.["uptimeMs"]).toBeNull();
    vi.useRealTimers();
  });

  it("requestProcessTermination on Windows skips taskkill when pid is null", () => {
    // Covers the false-arm of "pid !== null" inside the Windows branch
    // of requestProcessTermination. A FakeUtilityProcess can be coerced
    // to have a missing pid by reading it directly — but the type is
    // `readonly number`, so we substitute a no-pid stand-in.
    const processHandle = new FakeUtilityProcess(0);
    // Override pid to null via the structural cast that the supervisor's
    // helper uses (`getProcessPid` returns null for missing pids).
    Object.defineProperty(processHandle, "pid", { value: null });
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkillMock = vi.fn(() => Promise.resolve());
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "win32",
      runWindowsTaskkill: runWindowsTaskkillMock,
    });

    supervisor.start();
    const requestTerm = Reflect.get(supervisor, "requestProcessTermination") as (
      _: FakeUtilityProcess,
    ) => void;
    Reflect.apply(requestTerm, supervisor, [processHandle]);

    expect(runWindowsTaskkillMock).not.toHaveBeenCalled();
  });

  it("forceStop on Windows skips taskkill when pid is null and falls back to kill()", () => {
    // Covers the false-arm of "pid !== null" inside the Windows branch
    // of forceStop. Same structural shape as requestProcessTermination
    // — the fallthrough is to utilityProcess.kill().
    const processHandle = new FakeUtilityProcess(0);
    Object.defineProperty(processHandle, "pid", { value: null });
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkillMock = vi.fn(() => Promise.resolve());
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "win32",
      runWindowsTaskkill: runWindowsTaskkillMock,
    });

    supervisor.start();
    const forceStop = Reflect.get(supervisor, "forceStop") as (_: FakeUtilityProcess) => void;
    Reflect.apply(forceStop, supervisor, [processHandle]);

    expect(runWindowsTaskkillMock).not.toHaveBeenCalled();
    // POSIX-fallback path: utilityProcess.kill().
    expect(processHandle.kill).toHaveBeenCalled();
  });

  it("forceStop on POSIX falls back to utilityProcess.kill when pid is null", () => {
    // Covers the false-arm of "pid !== null" in the POSIX branch of
    // forceStop (the SIGKILL via process.kill is skipped, the helper
    // falls through to utilityProcess.kill()).
    const processHandle = new FakeUtilityProcess(0);
    Object.defineProperty(processHandle, "pid", { value: null });
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "linux",
      killProcess,
    });

    supervisor.start();
    const forceStop = Reflect.get(supervisor, "forceStop") as (_: FakeUtilityProcess) => void;
    Reflect.apply(forceStop, supervisor, [processHandle]);

    expect(killProcess).not.toHaveBeenCalled();
    expect(processHandle.kill).toHaveBeenCalled();
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
  readonly level: "info" | "warn" | "error";
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
      info: (event, payload) => push("info", event, payload),
      warn: (event, payload) => push("warn", event, payload),
      error: (event, payload) => push("error", event, payload),
    },
  };
}

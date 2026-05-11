import { EventEmitter } from "node:events";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  BrokerSupervisor,
  type BrokerSupervisorConfig,
  buildBrokerEnv,
  type ExecFileRunner,
  runWindowsTaskkill,
} from "../src/main/broker.ts";
import { isSafePayloadKey, type Logger, type LogPayload } from "../src/main/logger.ts";

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
  private readonly processPid: number | null;
  readonly kill = vi.fn<() => boolean>(() => true);
  readonly postMessage = vi.fn<(message: unknown) => void>();
  readonly stdout = new EventEmitter();
  readonly stderr = new EventEmitter();

  constructor(pid: number | null) {
    super();
    this.processPid = pid;
  }

  get pid(): number | null {
    return this.processPid;
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

  it("rejects waiters queued before start when the initial fork throws", async () => {
    const forkError = new Error("fork failed");
    const forkProcess = vi.fn(() => {
      throw forkError;
    }) as unknown as ForkProcess;
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    const ready = supervisor.whenReady();
    expect(() => supervisor.start()).toThrow(forkError);

    await expect(ready).rejects.toBe(forkError);
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("rejects waiters queued before start when the initial fork throws a non-Error", async () => {
    const forkProcess = vi.fn(() => {
      throw "fork failed";
    }) as unknown as ForkProcess;
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
    });

    const ready = supervisor.whenReady();
    let thrown: unknown;
    try {
      supervisor.start();
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBe("fork failed");
    await expect(ready).rejects.toThrow("fork failed");
    expect(supervisor.getStatus()).toBe("dead");
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

  it("logs utilityProcess error diagnostics without short-circuiting restart handling", () => {
    const diagnosticReport = [
      "Node.js diagnostic report",
      "cwd=/Users/fran/work/wuphf",
      "SECRET_TOKEN=must-not-leak",
    ].join("\n");
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("error", "FatalError", "v8.cc:123", diagnosticReport);

    expect(isSafePayloadKey("report")).toBe(false);
    expect(isSafePayloadKey("reportBytes")).toBe(true);
    const errorLog = calls.find((call) => call.event === "broker_process_error");
    expect(errorLog).toEqual({
      level: "error",
      event: "broker_process_error",
      payload: {
        type: "FatalError",
        location: "v8.cc:123",
        reportBytes: Buffer.byteLength(diagnosticReport, "utf8"),
        pid: 4321,
        restartCount: 0,
      },
    });
    expect(errorLog?.payload).not.toHaveProperty("report");
    expect(JSON.stringify(errorLog?.payload ?? {})).not.toContain(diagnosticReport);
    expect(supervisor.getStatus()).toBe("starting");
  });

  it("caps utilityProcess diagnostic report byte accounting before logging", () => {
    const diagnosticReport = "x".repeat(200 * 1024);
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("error", "FatalError", "v8.cc:123", diagnosticReport);

    const errorLog = calls.find((call) => call.event === "broker_process_error");
    expect(errorLog?.payload).toEqual({
      type: "FatalError",
      location: "v8.cc:123",
      reportBytes: 64 * 1024,
      pid: 4321,
      restartCount: 0,
    });
    expect(JSON.stringify(errorLog?.payload ?? {})).not.toContain(diagnosticReport);
  });

  it("redacts malformed utilityProcess error diagnostics without throwing", () => {
    const diagnosticReport = {
      env: "SECRET_TOKEN=must-not-leak",
    };
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("error", { kind: "FatalError" }, undefined, diagnosticReport);

    const errorLog = calls.find((call) => call.event === "broker_process_error");
    expect(errorLog).toEqual({
      level: "error",
      event: "broker_process_error",
      payload: {
        pid: 4321,
        restartCount: 0,
        droppedKeys: 2,
      },
    });
    expect(errorLog?.payload).not.toHaveProperty("report");
    expect(JSON.stringify(errorLog?.payload ?? {})).not.toContain("SECRET_TOKEN");
    expect(supervisor.getStatus()).toBe("starting");
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

  it("logs broker_ready_invalid for ready-shaped malformed brokerUrl without leaking it", () => {
    const rawBrokerUrl = "http://127.0.0.1:54321/";
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
    });

    supervisor.start();
    processHandle.emit("message", { ready: true, brokerUrl: rawBrokerUrl });

    const invalidLog = calls.find((call) => call.event === "broker_ready_invalid");
    expect(invalidLog).toEqual({
      level: "warn",
      event: "broker_ready_invalid",
      payload: { reason: "non_canonical_origin" },
    });
    expect(JSON.stringify(invalidLog?.payload ?? {})).not.toContain(rawBrokerUrl);
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

  it("does not go fatal or reschedule when restart fork stops before throwing", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const forkError = new Error("fork retry stopped");
    let supervisor!: BrokerSupervisor;
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
        void supervisor.stop();
        throw forkError;
      },
    ) as unknown as ForkProcess;
    const onFatal = vi.fn<(reason: string) => void>();
    const { logger, calls } = createMemoryLogger();
    supervisor = new BrokerSupervisor({
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
    await vi.advanceTimersByTimeAsync(1_000);

    expect(calls).toContainEqual({
      level: "error",
      event: "broker_restart_start_failed",
      payload: {
        error: "fork retry stopped",
        restartCount: 1,
        maxRestartRetries: 1,
        serviceName: "wuphf-broker",
      },
    });
    expect(onFatal).not.toHaveBeenCalled();
    expect(calls.filter((call) => call.event === "broker_restart_scheduled")).toHaveLength(1);
    expect(forkProcess).toHaveBeenCalledTimes(2);
    expect(supervisor.getStatus()).toBe("dead");
    await expect(supervisor.whenReady()).rejects.toThrow("broker_stopped");
    vi.useRealTimers();
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

  it("marks startup force-stop as fatal when Windows taskkill never yields an exit", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkillMock = vi.fn<NonNullable<BrokerSupervisorConfig["runWindowsTaskkill"]>>(
      () => Promise.reject(new Error("taskkill failed")),
    );
    const onFatal = vi.fn();
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      platform: "win32",
      runWindowsTaskkill: runWindowsTaskkillMock,
      onFatal,
      startupTimeoutMs: 500,
      forceStopGraceMs: 100,
      maxRestartRetries: 3,
    });

    supervisor.start();
    const ready = supervisor.whenReady();
    await vi.advanceTimersByTimeAsync(500);
    expect(runWindowsTaskkillMock).toHaveBeenNthCalledWith(1, 4321, { force: false });
    await vi.advanceTimersByTimeAsync(100);
    expect(runWindowsTaskkillMock).toHaveBeenNthCalledWith(2, 4321, { force: true });
    await vi.advanceTimersByTimeAsync(100);

    await expect(ready).rejects.toThrow("Broker startup force-stop deadline exceeded");
    expect(supervisor.getStatus()).toBe("dead");
    expect(supervisor.getPid()).toBeNull();
    expect(onFatal).toHaveBeenCalledWith("Broker startup force-stop deadline exceeded");
    expect(calls).toContainEqual({
      level: "error",
      event: "broker_startup_force_stop_failed",
      payload: {
        pid: 4321,
        restartCount: 0,
        reason: "Broker startup force-stop deadline exceeded",
      },
    });
    vi.useRealTimers();
  });

  it("startup final deadline bails when force-stop exit wins the queued-callback race", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>((_pid, signal) => {
      if (signal === "SIGKILL") {
        setTimeout(() => processHandle.emit("exit", -1), 0);
      }
    });
    const onFatal = vi.fn();
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      killProcess,
      onFatal,
      startupTimeoutMs: 500,
      forceStopGraceMs: 100,
      firstBackoffMs: 10_000,
      maxRestartRetries: 3,
    });

    supervisor.start();
    vi.advanceTimersByTime(500);
    const clearSpy = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => undefined);
    await vi.advanceTimersByTimeAsync(100);
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(100);
    clearSpy.mockRestore();

    expect(calls.some((call) => call.event === "broker_startup_force_stop_failed")).toBe(false);
    expect(onFatal).not.toHaveBeenCalled();
    await supervisor.stop();
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
    // Use a syntactically valid BrokerUrl with a different port from the
    // previous ready — the brand rejects non-numeric ports, so a sentinel
    // like ":2-replay" would short-circuit at readReadyMessage and never
    // exercise the listener-fanout path that unsubscribe gates. With a
    // valid URL, the test actually proves unsubscribe() suppressed the
    // listener instead of relying on URL validation as the suppressor.
    secondProcess.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:3" });
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

  it("whenReady() rejects when fatalReason is set via the natural exit-cap path", async () => {
    // With maxRestartRetries=0, the first exit hits the cap and
    // `handleExit` sets `fatalReason` without scheduling a restart. A
    // subsequent `whenReady()` call must short-circuit on the
    // `fatalReason !== null` arm.
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const onFatal = vi.fn();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      maxRestartRetries: 0,
      onFatal,
    });

    supervisor.start();
    processHandle.emit("exit", 0, null);
    expect(onFatal).toHaveBeenCalledTimes(1);
    await expect(supervisor.whenReady()).rejects.toThrow(/restart retries/);
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

  it("startup-timeout watchdog bails when the fork has cycled (sender-identity arm)", () => {
    // Natural race: an exit before the startup timeout fires nulls
    // `brokerProcess`. The startup timer then fires and the bail-out's
    // sender-identity arm (`this.brokerProcess !== brokerProcess` against
    // the closure-captured handle) is TRUE → no broker_ready_timeout
    // is logged. Covers the first arm of the 4-OR bail at broker.ts:691.
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
    // Exit at t=0 → handleExit nulls brokerProcess. The startup timer
    // is still armed; when it fires it sees brokerProcess (null) !==
    // closure capture (handle) and bails.
    processHandle.emit("exit", 0, null);
    vi.advanceTimersByTime(500);

    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(false);
    vi.useRealTimers();
  });

  it("startup-timeout watchdog bails when {ready} won the race (brokerUrl arm, queued callback)", () => {
    // Models the same real-Node race covered for the restart timer at
    // line 1078: the startup-timer callback was already queued onto the
    // event loop when the `{ready}` message handler ran, so the
    // handler's `clearStartupTimer()` cannot recall it. Stub clearTimeout
    // to a no-op so the deterministic fake-timer scheduler still fires
    // the startup callback after `{ready}` landed and brokerUrl was set.
    //
    // Without the `this.brokerUrl !== null` arm in the bail-out, the
    // queued callback would log `broker_ready_timeout` and kill a
    // broker that's already ready (regression flagged by pass-4
    // triangulation; see broker.ts startup-timer comment).
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
    const clearSpy = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => undefined);
    processHandle.emit("message", { ready: true, brokerUrl: "http://127.0.0.1:7891" });
    vi.advanceTimersByTime(500);
    clearSpy.mockRestore();

    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(false);
    // The bail-out must NOT have called requestProcessTermination on the
    // ready broker. processHandle.kill should not have been invoked from
    // the watchdog path. (No other call site touches kill in this test.)
    expect(processHandle.kill).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("startup-timeout watchdog bails when stop() won the race (stopping arm, queued callback)", async () => {
    // Same queued-callback shape as the brokerUrl-arm test, but with
    // stop() as the racing winner. stop() sets `stopping = true` and
    // calls `clearStartupTimer()`; we no-op clearTimeout so the queued
    // callback still fires and must take the `this.stopping` bail-out
    // arm.
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const { logger, calls } = createMemoryLogger();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      logger,
      startupTimeoutMs: 500,
      stopGraceMs: 10,
      maxRestartRetries: 0,
    });

    supervisor.start();
    const clearSpy = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => undefined);
    // stop() sets stopping=true synchronously, then awaits exit.
    const stopPromise = supervisor.stop();
    // Fire the queued startup callback BEFORE the subprocess exits.
    vi.advanceTimersByTime(500);
    // Settle stop() by emitting the exit it's waiting on.
    processHandle.emit("exit", 0, null);
    await vi.advanceTimersByTimeAsync(50);
    await stopPromise;
    clearSpy.mockRestore();

    // The bail-out must have taken the `this.stopping` arm: no
    // broker_ready_timeout logged. stop() itself calls kill via
    // requestGracefulStop, so we cannot use kill-count as the signal
    // — broker_ready_timeout's absence is the definitive evidence.
    expect(calls.some((c) => c.event === "broker_ready_timeout")).toBe(false);
    vi.useRealTimers();
  });

  // Note: `clearStartupTimer`'s null-arm (timer === null on entry) is
  // exercised by every test that calls `supervisor.start()` — `start()`
  // → `armStartupTimer()` → `clearStartupTimer()` runs with both timer
  // fields null on the first invocation. No dedicated test needed; a
  // Reflect-based one would duplicate that natural coverage and
  // re-introduce the private-method reflection the pass-3 review
  // otherwise eliminated.

  it("clearStartupTimer clears an armed force timer when stop() runs mid-timeout", async () => {
    // Covers the true-arm of "startupForceTimer !== null" inside
    // clearStartupTimer — reachable only when the startup watchdog has
    // already fired (arming the force timer) but stop() runs before the
    // force timer fires. Observable assertion: the original force timer
    // must not fire `killProcess(pid, "SIGKILL")` after stop() runs,
    // even after advancing past its original deadline.
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      startupTimeoutMs: 500,
      forceStopGraceMs: 5_000,
      stopGraceMs: 10,
    });

    supervisor.start();
    vi.advanceTimersByTime(500); // startup fires → SIGTERM + force timer
    // SIGTERM-equivalent already fired via utilityProcess.kill().
    const sigtermCalls = processHandle.kill.mock.calls.length;
    // stop() now runs while the force timer is still armed for t=5_500.
    // clearStartupTimer must clear it; the force callback must never run.
    const stopPromise = supervisor.stop();
    setImmediate(() => processHandle.emit("exit", 0, null));
    await vi.advanceTimersByTimeAsync(10_000); // well past 5_500
    await stopPromise;

    // The cleared force timer must not have called killProcess(pid, SIGKILL).
    expect(killProcess).not.toHaveBeenCalled();
    // utilityProcess.kill should not have been called AGAIN beyond the
    // initial SIGTERM-equivalent + stop()'s own request — neither of
    // those is the force-timer path.
    // We can only assert the LOWER bound: no extra force-kill happened.
    expect(processHandle.kill.mock.calls.length).toBeGreaterThanOrEqual(sigtermCalls);
    vi.useRealTimers();
  });

  it("startup force timer bails when startup termination exits before force escalation", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    processHandle.kill.mockImplementation(() => {
      setTimeout(() => processHandle.emit("exit", 0, null), 0);
      return true;
    });
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      startupTimeoutMs: 500,
      forceStopGraceMs: 100,
      firstBackoffMs: 10_000,
    });

    supervisor.start();
    const clearSpy = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => undefined);
    vi.advanceTimersByTime(500);
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(100);
    clearSpy.mockRestore();
    await supervisor.stop();

    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    expect(killProcess).not.toHaveBeenCalled();
    expect(supervisor.getStatus()).toBe("dead");
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

  it("forwardBrokerLog treats subprocess droppedKeys as supervisor-reserved", () => {
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
      payload: { port: 7891, droppedKeys: 999 },
    });

    const forwarded = calls.find((c) => c.event === "broker_listener_started");
    expect(forwarded).toBeDefined();
    expect(forwarded?.payload).toEqual({ port: 7891 });
    expect(forwarded?.payload).not.toHaveProperty("droppedKeys");
  });

  // Pass-3 triangulation (architecture/security/distsys/types consensus,
  // BLOCK): the two tests that previously asserted null-uptime logging
  // in `handleExit` and the startup-timeout watchdog have been removed.
  // Both `start()` sets `startedAtMs` before the exit/timer paths can
  // observe it, and every path that clears `startedAtMs` also changes
  // `brokerProcess`, which the sender-identity gates catch first. The
  // null arms in `broker.ts:462` and `broker.ts:678` are marked
  // `v8 ignore` and documented as defensive-only.

  it("requestProcessTermination on Windows skips taskkill when pid is null", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(null);
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkillMock = vi.fn(() => Promise.resolve());
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "win32",
      runWindowsTaskkill: runWindowsTaskkillMock,
      stopGraceMs: 10,
    });

    supervisor.start();
    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(10);

    expect(runWindowsTaskkillMock).not.toHaveBeenCalled();
    expect(processHandle.kill).not.toHaveBeenCalled();
    processHandle.emit("exit", 0, null);
    await stopPromise;
    vi.useRealTimers();
  });

  it("forceStop on Windows skips taskkill when pid is null and falls back to kill()", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(null);
    const { forkProcess } = createForkMock([processHandle]);
    const runWindowsTaskkillMock = vi.fn(() => Promise.resolve());
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "win32",
      runWindowsTaskkill: runWindowsTaskkillMock,
      stopGraceMs: 10,
      forceStopGraceMs: 20,
    });

    supervisor.start();
    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(30);
    await stopPromise;

    expect(runWindowsTaskkillMock).not.toHaveBeenCalled();
    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("forceStop on POSIX falls back to utilityProcess.kill when pid is null", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(null);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<KillProcess>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      platform: "linux",
      killProcess,
      stopGraceMs: 10,
      forceStopGraceMs: 20,
    });

    supervisor.start();
    const stopPromise = supervisor.stop();
    await vi.advanceTimersByTimeAsync(30);
    await stopPromise;

    expect(killProcess).not.toHaveBeenCalled();
    expect(processHandle.kill).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
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

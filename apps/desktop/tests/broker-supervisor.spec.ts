import { EventEmitter } from "node:events";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  BrokerSupervisor,
  type BrokerSupervisorConfig,
  buildBrokerEnv,
} from "../src/main/broker.ts";

vi.mock("electron", () => ({
  utilityProcess: {
    fork: vi.fn(),
  },
}));

type ForkProcess = NonNullable<BrokerSupervisorConfig["forkProcess"]>;
type ForkOptions = Parameters<ForkProcess>[2];
type ElectronUtilityProcess = ReturnType<ForkProcess>;

class FakeUtilityProcess extends EventEmitter {
  readonly pid: number;
  readonly kill = vi.fn<() => boolean>(() => true);
  readonly postMessage = vi.fn<(message: unknown) => void>();

  constructor(pid: number) {
    super();
    this.pid = pid;
  }
}

describe("BrokerSupervisor", () => {
  beforeEach(() => {
    vi.useRealTimers();
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

  it("requests graceful shutdown and repeats handle-bound kill after the grace window", async () => {
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
    expect(processHandle.kill).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(4_999);
    expect(processHandle.kill).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    await stopPromise;

    expect(processHandle.kill).toHaveBeenCalledTimes(2);
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("uses Windows taskkill for graceful and forced broker process-tree termination", async () => {
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
    expect(runWindowsTaskkill).toHaveBeenCalledWith(4321, { force: false });
    expect(processHandle.kill).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(5_000);
    await stopPromise;

    expect(runWindowsTaskkill).toHaveBeenCalledWith(4321, { force: true });
    expect(processHandle.kill).not.toHaveBeenCalled();
  });

  it("restarts crashed brokers with exponential backoff", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const secondProcess = new FakeUtilityProcess(1002);
    const { forkProcess, forkProcessMock } = createForkMock([firstProcess, secondProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
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
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
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
      const processHandle = queue.shift() ?? new FakeUtilityProcess(9999);
      return processHandle as unknown as ElectronUtilityProcess;
    },
  );

  return {
    forkProcess: forkProcessMock as unknown as ForkProcess,
    forkProcessMock,
  };
}

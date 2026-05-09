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

  it("sends SIGTERM, waits the grace window, then sends SIGKILL on stop", async () => {
    vi.useFakeTimers();
    const processHandle = new FakeUtilityProcess(4321);
    const { forkProcess } = createForkMock([processHandle]);
    const killProcess = vi.fn<(pid: number, signal: NodeJS.Signals) => void>();
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      killProcess,
      stopGraceMs: 5_000,
    });
    supervisor.start();

    const stopPromise = supervisor.stop();

    expect(killProcess).toHaveBeenCalledWith(4321, "SIGTERM");
    await vi.advanceTimersByTimeAsync(4_999);
    expect(killProcess).not.toHaveBeenCalledWith(4321, "SIGKILL");

    await vi.advanceTimersByTimeAsync(1);
    await stopPromise;

    expect(killProcess).toHaveBeenCalledWith(4321, "SIGKILL");
    expect(supervisor.getStatus()).toBe("dead");
  });

  it("restarts crashed brokers with exponential backoff", async () => {
    vi.useFakeTimers();
    const firstProcess = new FakeUtilityProcess(1001);
    const secondProcess = new FakeUtilityProcess(1002);
    const { forkProcess, forkProcessMock } = createForkMock([firstProcess, secondProcess]);
    const supervisor = new BrokerSupervisor({
      brokerEntryPath: "/app/out/main/broker-stub.js",
      forkProcess,
      initialBackoffMs: 250,
      maxBackoffMs: 60_000,
    });

    supervisor.start();
    firstProcess.emit("exit", 1);

    expect(supervisor.getRestartCount()).toBe(1);
    expect(supervisor.getStatus()).toBe("starting");
    expect(supervisor.getLastRestartScheduledAtMs()).not.toBeNull();
    await vi.advanceTimersByTimeAsync(499);
    expect(forkProcessMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    expect(forkProcessMock).toHaveBeenCalledTimes(2);
    expect(supervisor.getPid()).toBe(1002);
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
      initialBackoffMs: 1,
      maxRestartRetries: 2,
      onFatal,
    });

    supervisor.start();
    firstProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(2);
    secondProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(4);
    thirdProcess.emit("exit", 1);
    await vi.advanceTimersByTimeAsync(8);

    expect(forkProcessMock).toHaveBeenCalledTimes(3);
    expect(onFatal).toHaveBeenCalledWith("Broker exited after 2 restart retries");
    expect(supervisor.getStatus()).toBe("dead");
    expect(supervisor.getRestartCount()).toBe(2);
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

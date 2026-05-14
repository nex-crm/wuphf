import { asRunnerId } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { RunnerLifecycleError } from "../src/errors.ts";
import { LifecycleStateMachine } from "../src/lifecycle.ts";

const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");

describe("LifecycleStateMachine", () => {
  it("transitions spawn to run to terminate", async () => {
    const lifecycle = new LifecycleStateMachine(runnerId);

    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "pending" });
    lifecycle.markRunning();
    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "running" });
    expect(lifecycle.beginStopping()).toBe(true);
    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "stopping" });
    lifecycle.markStopped({ exitCode: 0 });
    await lifecycle.stopped();
    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "stopped", exitCode: 0 });
  });

  it("makes double terminate idempotent", async () => {
    const lifecycle = new LifecycleStateMachine(runnerId);
    lifecycle.markRunning();

    expect(lifecycle.beginStopping()).toBe(true);
    expect(lifecycle.beginStopping()).toBe(false);
    lifecycle.markStopped({ exitCode: 0 });
    lifecycle.markStopped({ exitCode: 1 });

    await lifecycle.stopped();
    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "stopped", exitCode: 0 });
  });

  it("atomically claims terminal emission from running only once", () => {
    const lifecycle = new LifecycleStateMachine(runnerId);
    lifecycle.markRunning();

    expect(lifecycle.tryTerminate("finished")).toBe(true);
    expect(lifecycle.tryTerminate("terminated_by_request")).toBe(false);
    expect(lifecycle.snapshot()).toEqual({
      runnerId,
      phase: "stopping",
      terminalClaim: "finished",
    });
  });

  it("supports terminate during spawn before running is marked", async () => {
    const lifecycle = new LifecycleStateMachine(runnerId);

    expect(lifecycle.beginStopping()).toBe(true);
    expect(lifecycle.snapshot()).toEqual({ runnerId, phase: "stopping" });
    lifecycle.markStopped({ error: "cancelled before spawn completed" });

    await lifecycle.stopped();
    expect(lifecycle.snapshot()).toEqual({
      runnerId,
      phase: "stopped",
      error: "cancelled before spawn completed",
    });
  });

  it("records subprocess crash during run", async () => {
    const lifecycle = new LifecycleStateMachine(runnerId);
    lifecycle.markRunning();
    lifecycle.markStopped({ exitCode: 2, error: "subprocess crashed" });

    await lifecycle.stopped();
    expect(lifecycle.snapshot()).toEqual({
      runnerId,
      phase: "stopped",
      exitCode: 2,
      error: "subprocess crashed",
    });
  });

  it("rejects invalid owner transitions", () => {
    const lifecycle = new LifecycleStateMachine(runnerId);

    lifecycle.beginStopping();
    expect(() => lifecycle.markRunning()).toThrow(RunnerLifecycleError);
  });
});

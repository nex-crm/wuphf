import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { createCtrlCHandler } from "../../../src/tui/hooks/use-cancellable.js";

// ─── createCtrlCHandler ───

describe("createCtrlCHandler", () => {
  it("returns 'pending_exit' on first idle press", () => {
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => false,
      () => { exitCalled = true; },
    );

    const result = handler.handle();
    assert.equal(result, "pending_exit");
    assert.ok(!exitCalled, "should not exit on first idle press");
  });

  it("exits on second idle press within window", () => {
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => false,
      () => { exitCalled = true; },
      1000,
    );

    // First press: pending
    const first = handler.handle();
    assert.equal(first, "pending_exit");
    assert.ok(!exitCalled);

    // Second press within window: exit
    const second = handler.handle();
    assert.equal(second, "exit");
    assert.ok(exitCalled, "should exit on double-press");
  });

  it("returns 'cancelled' when cancelFn succeeds", () => {
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => true,
      () => { exitCalled = true; },
    );

    const result = handler.handle();
    assert.equal(result, "cancelled");
    assert.ok(!exitCalled, "should not exit when operation was cancelled");
  });

  it("exits on press after cancel within window when nothing left to cancel", () => {
    let cancelCount = 0;
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => {
        cancelCount++;
        return cancelCount === 1; // first call cancels, second doesn't
      },
      () => { exitCalled = true; },
      1000,
    );

    // First press: cancels the operation
    const first = handler.handle();
    assert.equal(first, "cancelled");
    assert.ok(!exitCalled);

    // Second press within 1s: nothing to cancel → exit
    const second = handler.handle();
    assert.equal(second, "exit");
    assert.ok(exitCalled, "should exit after cancel + idle press within window");
  });

  it("uses custom window duration", () => {
    let cancelCount = 0;
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => {
        cancelCount++;
        return cancelCount === 1;
      },
      () => { exitCalled = true; },
      50,
    );

    handler.handle(); // cancel
    assert.ok(!exitCalled);

    // Second press immediately (within 50ms): exit
    handler.handle();
    assert.ok(exitCalled, "should exit within custom window");
  });

  it("resets to pending_exit after window expires", async () => {
    let exitCalled = false;
    const handler = createCtrlCHandler(
      () => false,
      () => { exitCalled = true; },
      50, // 50ms window
    );

    // First press: pending
    handler.handle();
    assert.ok(!exitCalled);

    // Wait for window to expire
    await new Promise((r) => setTimeout(r, 100));

    // Press again after window: should be pending again, not exit
    const result = handler.handle();
    assert.equal(result, "pending_exit");
    assert.ok(!exitCalled, "should not exit after window expired");
  });
});

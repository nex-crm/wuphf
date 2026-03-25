import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { TickManager } from '../../src/agent/tick-manager.js';

/** Minimal mock loop that records calls. */
function makeMockLoop() {
  let phase = 'idle';
  const calls: string[] = [];
  return {
    getState() { return { phase }; },
    start() { phase = 'idle'; calls.push('start'); },
    async tick() { phase = 'done'; calls.push('tick'); },
    setPhase(p: string) { phase = p; },
    calls,
  };
}

describe('TickManager', () => {
  let tm: TickManager;

  beforeEach(() => {
    tm = new TickManager(50); // fast ticks for tests
  });

  afterEach(() => {
    tm.stopAll();
  });

  it('startLoop registers a running loop', () => {
    const loop = makeMockLoop();
    tm.startLoop('agent-1', loop, () => true);
    assert.equal(tm.isRunning('agent-1'), true);
  });

  it('startLoop is idempotent', () => {
    const loop = makeMockLoop();
    tm.startLoop('agent-1', loop, () => true);
    tm.startLoop('agent-1', loop, () => true); // second call should be no-op
    assert.equal(tm.isRunning('agent-1'), true);
  });

  it('stopLoop stops a running loop', () => {
    const loop = makeMockLoop();
    tm.startLoop('agent-1', loop, () => true);
    tm.stopLoop('agent-1');
    assert.equal(tm.isRunning('agent-1'), false);
  });

  it('stopLoop is safe for non-existent slug', () => {
    tm.stopLoop('nonexistent'); // should not throw
    assert.equal(tm.isRunning('nonexistent'), false);
  });

  it('stopAll stops all loops', () => {
    const loop1 = makeMockLoop();
    const loop2 = makeMockLoop();
    tm.startLoop('agent-1', loop1, () => true);
    tm.startLoop('agent-2', loop2, () => true);
    tm.stopAll();
    assert.equal(tm.isRunning('agent-1'), false);
    assert.equal(tm.isRunning('agent-2'), false);
  });

  it('ticks the loop when hasWork returns true', async () => {
    const loop = makeMockLoop();
    tm.startLoop('agent-1', loop, () => true);

    // Wait for at least one tick
    await new Promise((r) => setTimeout(r, 120));
    tm.stopLoop('agent-1');

    assert.ok(loop.calls.includes('tick'), 'Expected at least one tick call');
  });

  it('skips tick when idle and hasWork returns false', async () => {
    const loop = makeMockLoop();
    loop.setPhase('idle');
    tm.startLoop('agent-1', loop, () => false);

    await new Promise((r) => setTimeout(r, 120));
    tm.stopLoop('agent-1');

    assert.ok(!loop.calls.includes('tick'), 'Should not tick when idle with no work');
  });

  it('calls start() to reset when phase is done and hasWork returns true', async () => {
    const loop = makeMockLoop();
    loop.setPhase('done');
    tm.startLoop('agent-1', loop, () => true);

    await new Promise((r) => setTimeout(r, 120));
    tm.stopLoop('agent-1');

    assert.ok(loop.calls.includes('start'), 'Expected start() to be called to reset from done');
  });

  it('calls start() to reset when phase is error and hasWork returns true', async () => {
    const loop = makeMockLoop();
    loop.setPhase('error');
    tm.startLoop('agent-1', loop, () => true);

    await new Promise((r) => setTimeout(r, 120));
    tm.stopLoop('agent-1');

    assert.ok(loop.calls.includes('start'), 'Expected start() to be called to reset from error');
  });

  it('swallows tick errors without crashing', async () => {
    const loop = makeMockLoop();
    loop.tick = async () => { throw new Error('boom'); };
    loop.setPhase('build_context'); // active phase

    tm.startLoop('agent-1', loop, () => true);

    // Should not throw — just wait for a few ticks
    await new Promise((r) => setTimeout(r, 120));
    tm.stopLoop('agent-1');

    assert.equal(tm.isRunning('agent-1'), false);
  });

  it('isRunning returns false for unknown slug', () => {
    assert.equal(tm.isRunning('unknown'), false);
  });
});

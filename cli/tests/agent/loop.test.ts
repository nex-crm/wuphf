import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { AgentLoop, createMockStreamFn } from '../../src/agent/loop.js';
import { ToolRegistry } from '../../src/agent/tools.js';
import { AgentSessionStore } from '../../src/agent/session-store.js';
import { MessageQueues } from '../../src/agent/queues.js';
import type { AgentConfig } from '../../src/agent/types.js';

describe('AgentLoop', () => {
  let tmpDir: string;
  let config: AgentConfig;
  let tools: ToolRegistry;
  let sessions: AgentSessionStore;
  let queues: MessageQueues;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-loop-test-'));
    config = {
      slug: 'test-agent',
      name: 'Test Agent',
      expertise: ['testing'],
    };
    tools = new ToolRegistry();
    sessions = new AgentSessionStore(tmpDir);
    queues = new MessageQueues();
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('initializes in idle phase', () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    assert.equal(loop.getState().phase, 'idle');
  });

  it('tick transitions through phases', async () => {
    const phases: string[] = [];
    const loop = new AgentLoop(config, tools, sessions, queues);
    loop.on('phase_change', (_prev, next) => {
      phases.push(next as string);
    });

    // idle -> build_context
    await loop.tick();
    assert.ok(phases.includes('build_context'));
  });

  it('stop sets phase to done', () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    loop.start();
    loop.stop();
    assert.equal(loop.getState().phase, 'done');
  });

  it('events fire on phase change', async () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    let firedCount = 0;
    loop.on('phase_change', () => { firedCount++; });
    await loop.tick();
    assert.ok(firedCount > 0, 'Expected at least one phase_change event');
  });

  it('pause prevents tick execution', async () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    loop.pause();
    const before = loop.getState().phase;
    await loop.tick();
    assert.equal(loop.getState().phase, before, 'Phase should not change while paused');
  });

  it('resume allows tick execution after pause', async () => {
    const phases: string[] = [];
    const loop = new AgentLoop(config, tools, sessions, queues);
    loop.on('phase_change', (_prev, next) => phases.push(next as string));
    loop.pause();
    await loop.tick();
    assert.equal(phases.length, 0);
    loop.resume();
    await loop.tick();
    assert.ok(phases.length > 0);
  });

  it('start resets done phase to idle', () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    loop.stop(); // sets to done
    assert.equal(loop.getState().phase, 'done');
    loop.start();
    assert.equal(loop.getState().phase, 'idle');
  });

  it('getState returns a copy', () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    const s1 = loop.getState();
    const s2 = loop.getState();
    assert.notEqual(s1, s2);
    assert.deepEqual(s1, s2);
  });

  it('off removes event handler', async () => {
    const loop = new AgentLoop(config, tools, sessions, queues);
    let count = 0;
    const handler = () => { count++; };
    loop.on('phase_change', handler);
    await loop.tick();
    const countAfterFirst = count;
    loop.off('phase_change', handler);
    loop.start(); // reset to idle
    await loop.tick();
    assert.equal(count, countAfterFirst, 'Handler should not fire after off()');
  });

  it('createMockStreamFn returns a function', () => {
    const fn = createMockStreamFn();
    assert.equal(typeof fn, 'function');
  });
});

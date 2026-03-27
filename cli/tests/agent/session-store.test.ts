import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { AgentSessionStore } from '../../src/agent/session-store.js';

describe('AgentSessionStore', () => {
  let tmpDir: string;
  let store: AgentSessionStore;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-agent-session-test-'));
    store = new AgentSessionStore(tmpDir);
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('create returns a session ID containing agent slug', () => {
    const id = store.create('test-agent');
    assert.ok(id.startsWith('test-agent_'));
  });

  it('append and getHistory round-trip', () => {
    const sessionId = store.create('myagent');
    store.append(sessionId, { type: 'user', content: 'Hello' });
    store.append(sessionId, { type: 'assistant', content: 'Hi there' });

    const history = store.getHistory(sessionId);
    assert.equal(history.length, 2);
    assert.equal(history[0].content, 'Hello');
    assert.equal(history[0].type, 'user');
    assert.equal(history[1].content, 'Hi there');
    assert.equal(history[1].type, 'assistant');
  });

  it('each entry gets a unique id and timestamp', () => {
    const sessionId = store.create('myagent');
    const e1 = store.append(sessionId, { type: 'user', content: 'A' });
    const e2 = store.append(sessionId, { type: 'user', content: 'B' });
    assert.ok(e1.id);
    assert.ok(e2.id);
    assert.notEqual(e1.id, e2.id);
    assert.ok(e1.timestamp > 0);
    assert.ok(e2.timestamp >= e1.timestamp);
  });

  it('getHistory with limit returns last N entries', () => {
    const sessionId = store.create('myagent');
    store.append(sessionId, { type: 'user', content: 'A' });
    store.append(sessionId, { type: 'user', content: 'B' });
    store.append(sessionId, { type: 'user', content: 'C' });

    const history = store.getHistory(sessionId, { limit: 2 });
    assert.equal(history.length, 2);
    assert.equal(history[0].content, 'B');
    assert.equal(history[1].content, 'C');
  });

  it('getHistory returns empty for nonexistent session', () => {
    const history = store.getHistory('fake-agent_nonexistent');
    assert.deepEqual(history, []);
  });

  it('branch creates new session with shared history up to branch point', () => {
    const sessionId = store.create('myagent');
    store.append(sessionId, { type: 'user', content: 'A' });
    const e2 = store.append(sessionId, { type: 'user', content: 'B' });
    store.append(sessionId, { type: 'user', content: 'C' });

    const branchId = store.branch(sessionId, e2.id);
    assert.notEqual(branchId, sessionId);

    const branchHistory = store.getHistory(branchId);
    assert.equal(branchHistory.length, 2);
    assert.equal(branchHistory[0].content, 'A');
    assert.equal(branchHistory[1].content, 'B');

    // Original session still has all 3
    const original = store.getHistory(sessionId);
    assert.equal(original.length, 3);
  });

  it('listSessions returns session IDs for agent', () => {
    store.create('agent-a');
    store.create('agent-a');
    store.create('agent-b');

    const sessionsA = store.listSessions('agent-a');
    assert.equal(sessionsA.length, 2);

    const sessionsB = store.listSessions('agent-b');
    assert.equal(sessionsB.length, 1);
  });

  it('listSessions returns empty for unknown agent', () => {
    const sessions = store.listSessions('ghost');
    assert.deepEqual(sessions, []);
  });
});

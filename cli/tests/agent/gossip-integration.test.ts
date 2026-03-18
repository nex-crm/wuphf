import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { AgentLoop } from '../../src/agent/loop.js';
import { ToolRegistry, createGossipTools } from '../../src/agent/tools.js';
import { AgentSessionStore } from '../../src/agent/session-store.js';
import { MessageQueues } from '../../src/agent/queues.js';
import { CredibilityTracker } from '../../src/agent/adoption.js';
import type { AgentConfig, AgentTool, StreamFn } from '../../src/agent/types.js';
import type { GossipLayer, GossipInsight } from '../../src/agent/gossip.js';

// -- Helpers ------------------------------------------------------------------

/** Creates a mock GossipLayer that records calls and returns configurable results. */
function createMockGossipLayer(
  queryResults: GossipInsight[] = [],
): GossipLayer & { publishCalls: Array<{ slug: string; insight: string; context?: string }>; queryCalls: Array<{ slug: string; topic: string }> } {
  const publishCalls: Array<{ slug: string; insight: string; context?: string }> = [];
  const queryCalls: Array<{ slug: string; topic: string }> = [];

  return {
    publishCalls,
    queryCalls,
    async publish(agentSlug: string, insight: string, context?: string) {
      publishCalls.push({ slug: agentSlug, insight, context });
      return 'mock-id';
    },
    async query(agentSlug: string, topic: string) {
      queryCalls.push({ slug: agentSlug, topic });
      return queryResults;
    },
  };
}

/** Creates a simple stream function that yields a text response. */
function textStreamFn(text: string): StreamFn {
  return async function* (_messages, _tools) {
    yield { type: 'text' as const, content: text };
  };
}

/** Creates a stream function that yields a tool call. */
function toolCallStreamFn(toolName: string, params: Record<string, unknown>): StreamFn {
  let called = false;
  return async function* (_messages, _tools) {
    if (!called) {
      called = true;
      yield { type: 'tool_call' as const, toolName, toolParams: params };
    } else {
      yield { type: 'text' as const, content: 'Done after tool call.' };
    }
  };
}

// -- Tests --------------------------------------------------------------------

describe('Gossip integration in AgentLoop', () => {
  let tmpDir: string;
  let config: AgentConfig;
  let tools: ToolRegistry;
  let sessions: AgentSessionStore;
  let queues: MessageQueues;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'nex-gossip-test-'));
    config = {
      slug: 'test-agent',
      name: 'Test Agent',
      expertise: ['sales', 'analytics'],
    };
    tools = new ToolRegistry();
    sessions = new AgentSessionStore(tmpDir);
    queues = new MessageQueues();
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('buildContext queries gossip layer for insights', async () => {
    const gossip = createMockGossipLayer([]);
    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('hello'), gossip,
    );

    // tick from idle -> buildContext
    await loop.tick();

    assert.equal(gossip.queryCalls.length, 1);
    assert.equal(gossip.queryCalls[0].slug, 'test-agent');
    assert.equal(gossip.queryCalls[0].topic, 'sales, analytics');
  });

  it('buildContext injects adopted insights (score > 0.7) as system messages', async () => {
    // Insight with high relevance = high total score -> adopt
    const highScoreInsight: GossipInsight = {
      content: 'Customers prefer monthly billing',
      source: 'billing-agent',
      timestamp: Date.now(),
      relevance: 0.95,
    };
    const gossip = createMockGossipLayer([highScoreInsight]);
    const credTracker = new CredibilityTracker(join(tmpDir, 'cred'));
    // Give billing-agent high credibility
    credTracker.recordOutcome('billing-agent', true);
    credTracker.recordOutcome('billing-agent', true);
    credTracker.recordOutcome('billing-agent', true);
    credTracker.recordOutcome('billing-agent', true);

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('ok'), gossip, credTracker,
    );

    // idle -> buildContext
    await loop.tick();

    // Get the session ID from state
    const state = loop.getState();
    assert.ok(state.sessionId, 'Session should be created');

    const history = sessions.getHistory(state.sessionId);
    const gossipEntries = history.filter(e => e.content.includes('[GOSSIP:ADOPTED]'));
    assert.equal(gossipEntries.length, 1, 'Should have one ADOPTED gossip entry');
    assert.ok(gossipEntries[0].content.includes('Customers prefer monthly billing'));
    assert.ok(gossipEntries[0].content.includes('billing-agent'));
  });

  it('buildContext injects test insights (score 0.4-0.7) as secondary context', async () => {
    // Medium relevance -> test decision
    const mediumInsight: GossipInsight = {
      content: 'New CRM feature released',
      source: 'unknown-agent',
      timestamp: Date.now(),
      relevance: 0.6,
    };
    const gossip = createMockGossipLayer([mediumInsight]);

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('ok'), gossip,
    );

    await loop.tick();

    const state = loop.getState();
    assert.ok(state.sessionId);
    const history = sessions.getHistory(state.sessionId);
    const testEntries = history.filter(e => e.content.includes('[GOSSIP:TEST]'));
    assert.equal(testEntries.length, 1, 'Should have one TEST gossip entry');
    assert.ok(testEntries[0].content.includes('New CRM feature released'));
  });

  it('buildContext skips rejected insights (score < 0.4)', async () => {
    // Very low relevance + unknown source -> reject
    const lowInsight: GossipInsight = {
      content: 'Irrelevant noise',
      source: 'unknown-source',
      timestamp: Date.now() - 30 * 24 * 60 * 60 * 1000, // 30 days old
      relevance: 0.1,
    };
    const gossip = createMockGossipLayer([lowInsight]);

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('ok'), gossip,
    );

    await loop.tick();

    const state = loop.getState();
    assert.ok(state.sessionId);
    const history = sessions.getHistory(state.sessionId);
    const gossipEntries = history.filter(
      e => e.content.includes('[GOSSIP:ADOPTED]') || e.content.includes('[GOSSIP:TEST]'),
    );
    assert.equal(gossipEntries.length, 0, 'No gossip entries for rejected insights');
  });

  it('gossip query failure is non-fatal', async () => {
    const gossip = createMockGossipLayer([]);
    // Override query to throw
    gossip.query = async () => { throw new Error('network error'); };

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('ok'), gossip,
    );

    // Should not throw
    await loop.tick();
    assert.equal(loop.getState().phase, 'build_context');
  });
});

describe('Gossip publishing in handleDone', () => {
  let tmpDir: string;
  let config: AgentConfig;
  let tools: ToolRegistry;
  let sessions: AgentSessionStore;
  let queues: MessageQueues;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'nex-gossip-done-'));
    config = {
      slug: 'publisher-agent',
      name: 'Publisher Agent',
      expertise: ['publishing'],
    };
    tools = new ToolRegistry();
    sessions = new AgentSessionStore(tmpDir);
    queues = new MessageQueues();
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('handleDone publishes collected insights via gossip', async () => {
    const gossip = createMockGossipLayer([]);
    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('result'), gossip,
    );

    // Run through: idle -> buildContext (resets collectedInsights)
    await loop.tick();

    // Push insights after buildContext, simulating insights collected during execution
    loop.addInsight('Revenue increased 20% in Q4');
    loop.addInsight('New market segment identified');

    await loop.tick(); // buildContext -> streamLlm
    await loop.tick(); // streamLlm -> handleDone (no pending tool call)

    assert.equal(gossip.publishCalls.length, 2, 'Both insights should be published');
    assert.equal(gossip.publishCalls[0].slug, 'publisher-agent');
    assert.equal(gossip.publishCalls[0].insight, 'Revenue increased 20% in Q4');
    assert.equal(gossip.publishCalls[1].insight, 'New market segment identified');
  });

  it('handleDone does not publish when no insights collected', async () => {
    const gossip = createMockGossipLayer([]);
    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('result'), gossip,
    );

    // Full cycle without adding insights
    await loop.tick(); // idle -> buildContext
    await loop.tick(); // buildContext -> streamLlm
    await loop.tick(); // streamLlm -> handleDone

    assert.equal(gossip.publishCalls.length, 0, 'No insights to publish');
  });

  it('gossip publish failure is non-fatal', async () => {
    const gossip = createMockGossipLayer([]);
    gossip.publish = async () => { throw new Error('publish failed'); };

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('result'), gossip,
    );

    loop.addInsight('will fail to publish');

    await loop.tick(); // idle -> buildContext
    await loop.tick(); // buildContext -> streamLlm
    await loop.tick(); // streamLlm -> handleDone

    assert.equal(loop.getState().phase, 'done');
  });
});

describe('CredibilityTracker updates on task completion', () => {
  let tmpDir: string;
  let config: AgentConfig;
  let tools: ToolRegistry;
  let sessions: AgentSessionStore;
  let queues: MessageQueues;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'nex-cred-test-'));
    config = {
      slug: 'cred-agent',
      name: 'Cred Agent',
      expertise: ['testing'],
    };
    tools = new ToolRegistry();
    sessions = new AgentSessionStore(tmpDir);
    queues = new MessageQueues();
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('records success when task completes without errors', async () => {
    const credTracker = new CredibilityTracker(join(tmpDir, 'cred'));
    const loop = new AgentLoop(
      config, tools, sessions, queues,
      textStreamFn('success'), undefined, credTracker,
    );

    await loop.tick(); // idle -> buildContext
    await loop.tick(); // buildContext -> streamLlm
    await loop.tick(); // streamLlm -> handleDone

    // Default credibility is 0.5, after one success should be 1.0
    assert.equal(credTracker.getCredibility('cred-agent'), 1.0);
  });

  it('records failure when task had tool errors', async () => {
    const credTracker = new CredibilityTracker(join(tmpDir, 'cred'));

    // Stream function that triggers a tool call to a non-existent tool
    const streamFn = toolCallStreamFn('nonexistent_tool', { q: 'test' });

    const loop = new AgentLoop(
      config, tools, sessions, queues,
      streamFn, undefined, credTracker,
    );

    await loop.tick(); // idle -> buildContext
    await loop.tick(); // buildContext -> streamLlm (yields tool_call)
    await loop.tick(); // streamLlm -> executeTool (unknown tool error)
    await loop.tick(); // execute_tool -> streamLlm (second call)
    await loop.tick(); // streamLlm -> handleDone (text response)

    // After one failure: 0 / 1 = 0.0
    assert.equal(credTracker.getCredibility('cred-agent'), 0.0);
  });
});

describe('Gossip tools registration', () => {
  it('createGossipTools returns publish and query tools', () => {
    const gossip = createMockGossipLayer([]);
    const gossipTools = createGossipTools(gossip, 'my-agent');

    const names = gossipTools.map(t => t.name).sort();
    assert.deepEqual(names, ['nex_gossip_publish', 'nex_gossip_query']);
  });

  it('gossip tools have correct schemas', () => {
    const gossip = createMockGossipLayer([]);
    const gossipTools = createGossipTools(gossip, 'my-agent');

    const publishTool = gossipTools.find(t => t.name === 'nex_gossip_publish')!;
    assert.ok(publishTool);
    assert.deepEqual(
      (publishTool.schema as Record<string, unknown>).required,
      ['insight'],
    );

    const queryTool = gossipTools.find(t => t.name === 'nex_gossip_query')!;
    assert.ok(queryTool);
    assert.deepEqual(
      (queryTool.schema as Record<string, unknown>).required,
      ['topic'],
    );
  });

  it('gossip tools are registerable and callable in ToolRegistry', async () => {
    const gossip = createMockGossipLayer([]);
    const gossipTools = createGossipTools(gossip, 'my-agent');
    const registry = new ToolRegistry();

    for (const tool of gossipTools) {
      registry.register(tool);
    }

    assert.ok(registry.has('nex_gossip_publish'));
    assert.ok(registry.has('nex_gossip_query'));

    // Validate params
    const publishValid = registry.validate('nex_gossip_publish', { insight: 'test insight' });
    assert.equal(publishValid.valid, true);

    const publishMissing = registry.validate('nex_gossip_publish', {});
    assert.equal(publishMissing.valid, false);

    const queryValid = registry.validate('nex_gossip_query', { topic: 'sales' });
    assert.equal(queryValid.valid, true);

    const queryMissing = registry.validate('nex_gossip_query', {});
    assert.equal(queryMissing.valid, false);
  });

  it('nex_gossip_publish tool calls gossipLayer.publish', async () => {
    const gossip = createMockGossipLayer([]);
    const gossipTools = createGossipTools(gossip, 'my-agent');
    const publishTool = gossipTools.find(t => t.name === 'nex_gossip_publish')!;

    const controller = new AbortController();
    const result = await publishTool.execute(
      { insight: 'test finding', context: 'during analysis' },
      controller.signal,
      () => {},
    );

    assert.equal(gossip.publishCalls.length, 1);
    assert.equal(gossip.publishCalls[0].slug, 'my-agent');
    assert.equal(gossip.publishCalls[0].insight, 'test finding');
    assert.equal(gossip.publishCalls[0].context, 'during analysis');

    const parsed = JSON.parse(result);
    assert.equal(parsed.published, true);
    assert.equal(parsed.id, 'mock-id');
  });

  it('nex_gossip_query tool calls gossipLayer.query', async () => {
    const mockInsights: GossipInsight[] = [
      { content: 'Some insight', source: 'other-agent', timestamp: Date.now(), relevance: 0.8 },
    ];
    const gossip = createMockGossipLayer(mockInsights);
    const gossipTools = createGossipTools(gossip, 'my-agent');
    const queryTool = gossipTools.find(t => t.name === 'nex_gossip_query')!;

    const controller = new AbortController();
    const result = await queryTool.execute(
      { topic: 'revenue trends' },
      controller.signal,
      () => {},
    );

    assert.equal(gossip.queryCalls.length, 1);
    assert.equal(gossip.queryCalls[0].slug, 'my-agent');
    assert.equal(gossip.queryCalls[0].topic, 'revenue trends');

    const parsed = JSON.parse(result);
    assert.equal(parsed.count, 1);
    assert.equal(parsed.insights[0].content, 'Some insight');
  });
});

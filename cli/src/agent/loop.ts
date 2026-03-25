/**
 * Pi-style state machine agent loop.
 * Runs idle -> build_context -> stream_llm -> execute_tool -> done cycle.
 * Default streamFn uses WUPHF Ask API until real LLM provider is wired.
 */

import type { AgentConfig, AgentState, AgentPhase, AgentTool, StreamFn, ToolCall } from './types.js';
import type { ToolRegistry } from './tools.js';
import type { AgentSessionStore } from './session-store.js';
import type { MessageQueues } from './queues.js';
import type { NexClient } from '../lib/client.js';
import type { GossipLayer, GossipInsight } from './gossip.js';
import { scoreInsight } from './adoption.js';
import type { CredibilityTracker } from './adoption.js';

type EventName = 'phase_change' | 'tool_call' | 'message' | 'error' | 'done';
type EventHandler = (...args: unknown[]) => void;

/**
 * Create a streamFn backed by the WUPHF Ask API.
 * Uses the context graph AI to generate responses.
 * Falls back to a simple echo if no client is available.
 */
export function createNexAskStreamFn(client?: NexClient): StreamFn {
  return async function* nexAskStream(
    messages: Array<{ role: string; content: string }>,
    _tools: AgentTool[],
  ) {
    const lastMsg = messages[messages.length - 1];
    const content = lastMsg?.content ?? '';

    if (client?.isAuthenticated) {
      try {
        const result = await client.post('/ask', {
          question: content,
          context: messages.slice(0, -1).map(m => `${m.role}: ${m.content}`).join('\n').slice(-2000),
        }) as { answer?: string };
        yield {
          type: 'text' as const,
          content: (result as Record<string, unknown>).answer as string ?? JSON.stringify(result),
        };
        return;
      } catch (err) {
        yield {
          type: 'text' as const,
          content: `Error: ${err instanceof Error ? err.message : String(err)}`,
        };
        return;
      }
    }

    // No client — echo response
    yield {
      type: 'text' as const,
      content: `[No API key configured] ${content}`,
    };
  };
}

/**
 * @deprecated Use createNexAskStreamFn instead.
 */
export function createMockStreamFn(): StreamFn {
  return createNexAskStreamFn();
}

export class AgentLoop {
  private state: AgentState;
  private tools: ToolRegistry;
  private sessions: AgentSessionStore;
  private queues: MessageQueues;
  private streamFn: StreamFn;
  private gossipLayer: GossipLayer | null;
  private credibilityTracker: CredibilityTracker | null;
  private running = false;
  private paused = false;
  private eventHandlers = new Map<string, Set<EventHandler>>();
  private pendingToolCall: ToolCall | null = null;
  private abortController: AbortController | null = null;
  /** Tracks whether the current task had any errors (tool failures, LLM errors). */
  private taskHadError = false;
  /** Insights collected during this execution cycle (from tool results, gossip publish calls). */
  private collectedInsights: string[] = [];

  constructor(
    config: AgentConfig,
    tools: ToolRegistry,
    sessions: AgentSessionStore,
    queues: MessageQueues,
    streamFn?: StreamFn,
    gossipLayer?: GossipLayer,
    credibilityTracker?: CredibilityTracker,
  ) {
    this.tools = tools;
    this.sessions = sessions;
    this.queues = queues;
    this.streamFn = streamFn ?? createMockStreamFn();
    this.gossipLayer = gossipLayer ?? null;
    this.credibilityTracker = credibilityTracker ?? null;

    this.state = {
      phase: 'idle',
      config,
      tokensUsed: 0,
      costUsd: 0,
    };
  }

  private setPhase(phase: AgentPhase): void {
    const prev = this.state.phase;
    this.state.phase = phase;
    this.emit('phase_change', prev, phase);
  }

  private emit(event: string, ...args: unknown[]): void {
    const handlers = this.eventHandlers.get(event);
    if (handlers) {
      for (const handler of handlers) {
        try {
          handler(...args);
        } catch {
          // swallow handler errors
        }
      }
    }
  }

  on(event: EventName, handler: EventHandler): void {
    let set = this.eventHandlers.get(event);
    if (!set) {
      set = new Set();
      this.eventHandlers.set(event, set);
    }
    set.add(handler);
  }

  off(event: string, handler: EventHandler): void {
    const set = this.eventHandlers.get(event);
    if (set) {
      set.delete(handler);
    }
  }

  getState(): AgentState {
    return { ...this.state };
  }

  async tick(): Promise<void> {
    if (this.paused) return;

    // Check for steer interrupts
    const steerMsg = this.queues.drainSteer(this.state.config.slug);
    if (steerMsg) {
      if (this.state.sessionId) {
        this.sessions.append(this.state.sessionId, {
          type: 'system',
          content: `[STEER] ${steerMsg}`,
        });
      }
    }

    switch (this.state.phase) {
      case 'idle':
        await this.buildContext();
        break;
      case 'build_context':
        await this.streamLlm();
        break;
      case 'stream_llm':
        if (this.pendingToolCall) {
          await this.executeTool();
        } else {
          await this.handleDone();
        }
        break;
      case 'execute_tool':
        await this.streamLlm();
        break;
      case 'done':
        await this.handleDone();
        break;
      case 'error':
        // Stay in error until reset
        break;
    }
  }

  private async buildContext(): Promise<void> {
    this.setPhase('build_context');
    this.taskHadError = false;
    this.collectedInsights = [];

    if (!this.state.sessionId) {
      this.state.sessionId = this.sessions.create(this.state.config.slug);
    }

    // Inject follow-up messages as context
    const followUp = this.queues.drainFollowUp(this.state.config.slug);
    if (followUp) {
      this.sessions.append(this.state.sessionId, {
        type: 'user',
        content: followUp,
      });
    }

    // Query gossip layer for relevant insights from other agents
    if (this.gossipLayer) {
      await this.injectGossipInsights();
    }

    this.setPhase('build_context');
  }

  /**
   * Query the gossip layer, score each insight, and inject adopted/test insights
   * into the session as system messages.
   */
  private async injectGossipInsights(): Promise<void> {
    if (!this.gossipLayer || !this.state.sessionId) return;

    const topic = this.state.config.expertise.join(', ');
    let insights: GossipInsight[];
    try {
      insights = await this.gossipLayer.query(this.state.config.slug, topic);
    } catch {
      // Gossip query failure is non-fatal; continue without insights
      return;
    }

    if (insights.length === 0) return;

    const adopted: Array<{ insight: GossipInsight; score: number }> = [];
    const experimental: Array<{ insight: GossipInsight; score: number }> = [];

    for (const insight of insights) {
      const sourceCredibility = this.credibilityTracker
        ? this.credibilityTracker.getCredibility(insight.source)
        : undefined;
      const score = scoreInsight(insight, topic, sourceCredibility);

      if (score.decision === 'adopt') {
        adopted.push({ insight, score: score.total });
      } else if (score.decision === 'test') {
        experimental.push({ insight, score: score.total });
      }
      // 'reject' insights are silently dropped
    }

    if (adopted.length > 0) {
      const lines = adopted.map(
        a => `- [${a.insight.source}] (score ${a.score.toFixed(2)}): ${a.insight.content}`,
      );
      this.sessions.append(this.state.sessionId, {
        type: 'system',
        content: `[GOSSIP:ADOPTED] Trusted insights from other agents:\n${lines.join('\n')}`,
      });
    }

    if (experimental.length > 0) {
      const lines = experimental.map(
        e => `- [${e.insight.source}] (score ${e.score.toFixed(2)}): ${e.insight.content}`,
      );
      this.sessions.append(this.state.sessionId, {
        type: 'system',
        content: `[GOSSIP:TEST] Consider verifying these insights experimentally:\n${lines.join('\n')}`,
      });
    }
  }

  private async streamLlm(): Promise<void> {
    this.setPhase('stream_llm');

    const history = this.state.sessionId
      ? this.sessions.getHistory(this.state.sessionId)
      : [];

    const messages = history.map(e => ({
      role: e.type === 'user' ? 'user' : 'assistant',
      content: e.content,
    }));

    if (messages.length === 0) {
      messages.push({
        role: 'system',
        content: `You are ${this.state.config.name}. Expertise: ${this.state.config.expertise.join(', ')}.${this.state.config.personality ? ' ' + this.state.config.personality : ''}`,
      });
    }

    this.abortController = new AbortController();
    const availableTools = this.tools.list().filter(
      t => !this.state.config.tools || this.state.config.tools.includes(t.name),
    );

    let fullText = '';
    this.pendingToolCall = null;

    try {
      for await (const chunk of this.streamFn(messages, availableTools)) {
        if (this.abortController.signal.aborted) break;

        if (chunk.type === 'text' && chunk.content) {
          fullText += chunk.content;
          this.emit('message', chunk.content);
        } else if (chunk.type === 'tool_call' && chunk.toolName) {
          this.pendingToolCall = {
            toolName: chunk.toolName,
            params: chunk.toolParams ?? {},
            startedAt: Date.now(),
          };
          break;
        }
      }
    } catch (err) {
      this.state.error = err instanceof Error ? err.message : String(err);
      this.setPhase('error');
      this.emit('error', this.state.error);
      return;
    }

    if (fullText && this.state.sessionId) {
      this.sessions.append(this.state.sessionId, {
        type: 'assistant',
        content: fullText,
      });
    }
  }

  private async executeTool(): Promise<void> {
    if (!this.pendingToolCall) {
      this.setPhase('stream_llm');
      return;
    }

    this.setPhase('execute_tool');
    const call = this.pendingToolCall;
    this.emit('tool_call', call.toolName, call.params);

    const tool = this.tools.get(call.toolName);
    if (!tool) {
      call.error = `Unknown tool: ${call.toolName}`;
      call.completedAt = Date.now();
      this.taskHadError = true;
      if (this.state.sessionId) {
        this.sessions.append(this.state.sessionId, {
          type: 'tool_result',
          content: call.error,
          metadata: { toolName: call.toolName, error: true },
        });
      }
      this.pendingToolCall = null;
      return;
    }

    const validation = this.tools.validate(call.toolName, call.params);
    if (!validation.valid) {
      call.error = `Validation failed: ${validation.errors?.join(', ')}`;
      call.completedAt = Date.now();
      this.taskHadError = true;
      if (this.state.sessionId) {
        this.sessions.append(this.state.sessionId, {
          type: 'tool_result',
          content: call.error,
          metadata: { toolName: call.toolName, error: true },
        });
      }
      this.pendingToolCall = null;
      return;
    }

    if (this.state.sessionId) {
      this.sessions.append(this.state.sessionId, {
        type: 'tool_call',
        content: JSON.stringify({ tool: call.toolName, params: call.params }),
        metadata: { toolName: call.toolName },
      });
    }

    const controller = this.abortController ?? new AbortController();
    try {
      call.result = await tool.execute(call.params, controller.signal, (partial) => {
        this.emit('message', partial);
      });
      call.completedAt = Date.now();

      if (this.state.sessionId) {
        this.sessions.append(this.state.sessionId, {
          type: 'tool_result',
          content: call.result,
          metadata: { toolName: call.toolName },
        });
      }
    } catch (err) {
      call.error = err instanceof Error ? err.message : String(err);
      call.completedAt = Date.now();
      this.taskHadError = true;

      if (this.state.sessionId) {
        this.sessions.append(this.state.sessionId, {
          type: 'tool_result',
          content: call.error,
          metadata: { toolName: call.toolName, error: true },
        });
      }
    }

    // Collect insight from gossip_publish tool calls
    if (call.toolName === 'nex_gossip_publish' && call.result && !call.error) {
      const insight = call.params.insight;
      if (typeof insight === 'string') {
        this.collectedInsights.push(insight);
      }
    }

    this.pendingToolCall = null;
  }

  private async handleDone(): Promise<void> {
    // Check if there's more work from queues
    if (this.queues.hasSteer(this.state.config.slug) || this.queues.hasFollowUp(this.state.config.slug)) {
      this.setPhase('idle');
      return;
    }

    // Publish any collected insights via gossip layer
    if (this.gossipLayer && this.collectedInsights.length > 0) {
      for (const insight of this.collectedInsights) {
        try {
          await this.gossipLayer.publish(this.state.config.slug, insight);
        } catch {
          // Gossip publish failure is non-fatal
        }
      }
      this.collectedInsights = [];
    }

    // Update credibility based on task outcome
    if (this.credibilityTracker) {
      this.credibilityTracker.recordOutcome(
        this.state.config.slug,
        !this.taskHadError,
      );
    }

    this.setPhase('done');
    this.state.lastHeartbeat = Date.now();
    this.emit('done');
  }

  start(): void {
    this.running = true;
    this.paused = false;
    if (this.state.phase === 'done' || this.state.phase === 'error') {
      this.setPhase('idle');
    }
  }

  stop(): void {
    this.running = false;
    this.paused = false;
    if (this.abortController) {
      this.abortController.abort();
    }
    this.setPhase('done');
  }

  pause(): void {
    this.paused = true;
  }

  resume(): void {
    this.paused = false;
  }

  /** Allow external code or tools to push insights for gossip publishing at done phase. */
  addInsight(insight: string): void {
    this.collectedInsights.push(insight);
  }
}

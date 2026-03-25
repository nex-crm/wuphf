/**
 * Singleton service that manages agent instances and exposes state for the TUI.
 * Bridges the agent runtime (loop, tools, queues, sessions) to the view layer.
 */

import { AgentLoop, createNexAskStreamFn } from "../../agent/loop.js";
import { createGeminiStreamFn } from "../../agent/providers/gemini.js";
import { createClaudeCodeStreamFn } from "../../agent/providers/claude-code.js";
import { ToolRegistry, createBuiltinTools } from "../../agent/tools.js";
import { AgentSessionStore } from "../../agent/session-store.js";
import { MessageQueues } from "../../agent/queues.js";
import { TickManager } from "../../agent/tick-manager.js";
import { NexClient } from "../../lib/client.js";
import { resolveApiKey, loadConfig } from "../../lib/config.js";
import { templates } from "../../agent/templates.js";
import type { AgentConfig, AgentState } from "../../agent/types.js";
import { getChatService } from "./chat-service.js";

export interface ManagedAgent {
  config: AgentConfig;
  state: AgentState;
  loop: AgentLoop;
}

export class AgentService {
  private agents = new Map<string, ManagedAgent>();
  private toolRegistry: ToolRegistry;
  private sessionStore: AgentSessionStore;
  private queues: MessageQueues;
  private tickManager = new TickManager(1000); // 1s tick rate for LLM providers
  private client: NexClient | undefined;
  private listeners: Array<() => void> = [];

  constructor(
    toolRegistry?: ToolRegistry,
    sessionStore?: AgentSessionStore,
    queues?: MessageQueues,
  ) {
    if (toolRegistry) {
      this.toolRegistry = toolRegistry;
    } else {
      this.client = new NexClient(resolveApiKey());
      this.toolRegistry = new ToolRegistry();
      for (const tool of createBuiltinTools(this.client)) {
        this.toolRegistry.register(tool);
      }
    }
    this.sessionStore = sessionStore ?? new AgentSessionStore();
    this.queues = queues ?? new MessageQueues();
  }

  /** Resolve the StreamFn based on configured LLM provider. */
  private resolveStreamFn(agentSlug: string): import("../../agent/types.js").StreamFn {
    let config: Record<string, unknown>;
    try { config = loadConfig() as Record<string, unknown>; } catch { config = {}; }

    const provider = config.llm_provider as string | undefined;
    const geminiKey = config.gemini_api_key as string | undefined;

    if (provider === "gemini" && geminiKey) {
      return createGeminiStreamFn(geminiKey);
    }

    if (provider === "claude-code") {
      return createClaudeCodeStreamFn(agentSlug);
    }

    // Fallback: WUPHF Ask API (uses context graph AI)
    return createNexAskStreamFn(this.client);
  }

  /** Create an agent from a full config. */
  create(config: AgentConfig): ManagedAgent {
    if (this.agents.has(config.slug)) {
      throw new Error(`Agent "${config.slug}" already exists.`);
    }

    const loop = new AgentLoop(
      config,
      this.toolRegistry,
      this.sessionStore,
      this.queues,
      this.resolveStreamFn(config.slug),
    );

    // Wire phase_change events to notify TUI listeners
    loop.on("phase_change", () => {
      const managed = this.agents.get(config.slug);
      if (managed) {
        managed.state = loop.getState();
        this.notify();
      }
    });

    // Wire agent message output back to the DM channel in chat service
    loop.on("message", (content: unknown) => {
      if (typeof content !== "string" || !content.trim()) return;
      try {
        const chatService = getChatService();
        // Ensure DM channel exists (creates if needed) then send reply
        const dmChannel = chatService.ensureChannel(`dm-${config.slug}`);
        chatService.send(dmChannel.id, content, config.name);
      } catch {
        // Chat service not available — ignore
      }
    });

    // Ensure DM channel exists in chat service immediately so it shows in sidebar
    try {
      getChatService().ensureChannel(`dm-${config.slug}`);
    } catch {
      // Chat service may not be ready yet — the message event handler will create it on first reply
    }

    const managed: ManagedAgent = {
      config,
      state: loop.getState(),
      loop,
    };

    this.agents.set(config.slug, managed);
    this.notify();
    return managed;
  }

  /** Create an agent from a pre-built template. */
  createFromTemplate(slug: string, templateName: string): ManagedAgent {
    const tmpl = templates[templateName];
    if (!tmpl) {
      throw new Error(`Unknown template: "${templateName}". Available: ${Object.keys(templates).join(", ")}`);
    }

    const config: AgentConfig = { slug, ...tmpl };
    return this.create(config);
  }

  /** Start the agent loop (sets phase to idle if done/error). */
  async start(slug: string): Promise<void> {
    const managed = this.requireAgent(slug);
    managed.loop.start();
    managed.state = managed.loop.getState();
    this.notify();

    // Run one tick to kick off the cycle
    await managed.loop.tick();
    managed.state = managed.loop.getState();
    this.notify();
  }

  /** Stop the agent loop. */
  stop(slug: string): void {
    const managed = this.requireAgent(slug);
    managed.loop.stop();
    managed.state = managed.loop.getState();
    this.notify();
  }

  /** Send a high-priority steer message to interrupt the agent. */
  steer(slug: string, message: string): void {
    this.requireAgent(slug);
    this.queues.steer(slug, message);
  }

  /** Queue a follow-up message for the agent's next turn. */
  followUp(slug: string, message: string): void {
    this.requireAgent(slug);
    this.queues.followUp(slug, message);
  }

  /** Ensure an agent's tick loop is running via TickManager (idempotent). */
  ensureRunning(slug: string): void {
    const managed = this.agents.get(slug);
    if (!managed) return;
    this.tickManager.startLoop(slug, managed.loop, () => {
      return this.queues.hasMessages(slug);
    });
  }

  /** Get a managed agent by slug. */
  get(slug: string): ManagedAgent | undefined {
    return this.agents.get(slug);
  }

  /** List all managed agents. */
  list(): ManagedAgent[] {
    return Array.from(this.agents.values());
  }

  /** Get the current state snapshot for an agent. */
  getState(slug: string): AgentState | undefined {
    return this.agents.get(slug)?.state;
  }

  /**
   * Subscribe to state changes. Returns an unsubscribe function.
   * The listener fires whenever any agent's state changes (create, start, stop, phase change).
   */
  subscribe(listener: () => void): () => void {
    this.listeners.push(listener);
    return () => {
      const idx = this.listeners.indexOf(listener);
      if (idx >= 0) this.listeners.splice(idx, 1);
    };
  }

  /** Get available template names. */
  getTemplateNames(): string[] {
    return Object.keys(templates);
  }

  /** Get a template config (without slug). */
  getTemplate(name: string): Omit<AgentConfig, "slug"> | undefined {
    return templates[name];
  }

  private notify(): void {
    for (const listener of this.listeners) {
      try {
        listener();
      } catch {
        // swallow listener errors
      }
    }
  }

  /** Remove an agent. Stops its loop first. */
  remove(slug: string): void {
    const managed = this.requireAgent(slug);
    try { managed.loop.stop(); } catch { /* already stopped */ }
    this.agents.delete(slug);
    this.notify();
  }

  /** Update mutable fields of an agent's config. */
  updateConfig(
    slug: string,
    updates: Partial<Pick<AgentConfig, "name" | "expertise" | "heartbeatCron">>,
  ): void {
    const managed = this.requireAgent(slug);
    if (updates.name !== undefined) managed.config.name = updates.name;
    if (updates.expertise !== undefined) managed.config.expertise = updates.expertise;
    if (updates.heartbeatCron !== undefined) managed.config.heartbeatCron = updates.heartbeatCron;
    this.notify();
  }

  private requireAgent(slug: string): ManagedAgent {
    const managed = this.agents.get(slug);
    if (!managed) {
      throw new Error(`Agent "${slug}" not found.`);
    }
    return managed;
  }
}

// ── Singleton ──

let instance: AgentService | undefined;

export function getAgentService(): AgentService {
  if (!instance) {
    instance = new AgentService();
  }
  return instance;
}

/** Reset the singleton (for testing). */
export function resetAgentService(): void {
  instance = undefined;
}

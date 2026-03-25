/**
 * Core type definitions for the Pi-based agent runtime.
 */

export type AgentPhase = 'idle' | 'build_context' | 'stream_llm' | 'execute_tool' | 'done' | 'error';

export interface AgentConfig {
  slug: string;
  name: string;
  expertise: string[];
  personality?: string;
  heartbeatCron?: string;
  tools?: string[];
  budget?: { maxTokens: number; maxCostUsd: number };
  autoDecideTimeout?: number;
}

export interface AgentState {
  phase: AgentPhase;
  config: AgentConfig;
  sessionId?: string;
  currentTask?: string;
  tokensUsed: number;
  costUsd: number;
  lastHeartbeat?: number;
  nextHeartbeat?: number;
  error?: string;
}

export interface AgentTool {
  name: string;
  description: string;
  schema: Record<string, unknown>;
  execute: (
    params: Record<string, unknown>,
    signal: AbortSignal,
    onUpdate: (partial: string) => void,
  ) => Promise<string>;
}

export interface ToolCall {
  toolName: string;
  params: Record<string, unknown>;
  result?: string;
  error?: string;
  startedAt: number;
  completedAt?: number;
}

export interface SessionEntry {
  id: string;
  parentId?: string;
  type: 'user' | 'assistant' | 'tool_call' | 'tool_result' | 'system';
  content: string;
  timestamp: number;
  metadata?: Record<string, unknown>;
}

export type StreamFn = (
  messages: Array<{ role: string; content: string }>,
  tools: AgentTool[],
) => AsyncGenerator<{
  type: 'text' | 'tool_call';
  content?: string;
  toolName?: string;
  toolParams?: Record<string, unknown>;
}>;

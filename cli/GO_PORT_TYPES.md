# TypeScript Source Files for Go Port

Complete type definitions, interfaces, and function signatures for porting WUPHF CLI to Go.

---

## 1. src/lib/client.ts (HTTP Client Implementation)

```typescript
/**
 * HTTP client for the WUPHF Developer API.
 * Hardcoded base URL with all HTTP methods.
 */

export class NexClient {
  private apiKey: string | undefined;
  private timeoutMs: number;

  constructor(apiKey?: string, timeoutMs = 120_000)

  get isAuthenticated(): boolean

  setApiKey(key: string): void

  private requireAuth(): void

  private async request<T = unknown>(
    method: string,
    path: string,
    body?: unknown,
    timeoutMs?: number,
  ): Promise<T>

  async register(email: string, name?: string, companyName?: string): Promise<Record<string, unknown>>

  async get<T = unknown>(path: string, timeoutMs?: number): Promise<T>

  async getRaw(path: string, timeoutMs?: number): Promise<string>

  async post<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T>

  async put<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T>

  async patch<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T>

  async delete<T = unknown>(path: string, timeoutMs?: number): Promise<T>
}
```

---

## 2. src/lib/config.ts (Configuration)

```typescript
/**
 * Configuration resolution: CLI flags > env vars > config file.
 * Base URL is hardcoded to production (WUPHF_DEV_URL escape hatch for local dev).
 */

export const CONFIG_PATH = join(homedir(), ".wuphf", "config.json")
export const BASE_URL = process.env.WUPHF_DEV_URL ?? loadConfig().dev_url ?? "https://app.nex.ai"
export const API_BASE = `${BASE_URL}/api/developers`
export const REGISTER_URL = `${BASE_URL}/api/v1/agents/register`

export interface NexConfig {
  api_key?: string;
  email?: string;
  workspace_id?: string;
  workspace_slug?: string;
  default_format?: string;
  default_timeout?: number;
  [key: string]: unknown;
}

export function loadConfig(): NexConfig
export function saveConfig(config: NexConfig): void
export function resolveApiKey(flagValue?: string): string | undefined
export function resolveFormat(flagValue?: string): string
export function resolveTimeout(flagValue?: string): number
export function persistRegistration(data: Record<string, unknown>): void
```

---

## 3. src/orchestration/types.ts (Core Types)

```typescript
/**
 * Core types for the multi-agent orchestration engine.
 * Follows Paperclip patterns: flat task pool, expertise-based routing,
 * atomic checkout, budget tracking.
 */

export interface SkillDeclaration {
  name: string;
  description: string;
  proficiency: number; // 0-1
}

export interface TaskDefinition {
  id: string;
  title: string;
  description: string;
  requiredSkills: string[];
  parentGoalId?: string;    // goal ancestry -- why this task exists
  priority: 'low' | 'medium' | 'high' | 'critical';
  status: 'pending' | 'locked' | 'in_progress' | 'completed' | 'failed';
  assignedAgent?: string;   // agent slug
  budget?: { maxTokens: number; maxCostUsd: number };
  createdAt: number;
  completedAt?: number;
  result?: string;
}

export interface GoalDefinition {
  id: string;
  title: string;
  description: string;
  projectId?: string;
  tasks: string[];          // task IDs
  status: 'active' | 'completed' | 'paused';
  createdAt: number;
}

export interface OrchestratorConfig {
  maxConcurrentAgents: number;  // default 3
  globalBudget: { maxTokens: number; maxCostUsd: number };
  taskTimeout: number;          // ms, default 5 minutes
  autoRetry: boolean;
  maxRetries: number;           // default 2
}

export interface BudgetSnapshot {
  agentSlug: string;
  tokensUsed: number;
  costUsd: number;
  budgetLimit: { maxTokens: number; maxCostUsd: number };
  percentUsed: number;
  warning: boolean;    // true if > 80%
  exceeded: boolean;   // true if > 100%
}
```

---

## 4. src/orchestration/router.ts (Task Router)

```typescript
/**
 * Expertise-based task routing.
 * Matches agent skills to task requirements using fuzzy string matching
 * weighted by proficiency scores.
 */

interface AgentRegistration {
  slug: string;
  skills: SkillDeclaration[];
}

function similarity(a: string, b: string): number
  // Simple normalized string similarity (Dice coefficient on bigrams).
  // Returns 0-1, where 1 is a perfect match.

export class TaskRouter {
  private agents: Map<string, AgentRegistration> = new Map()

  registerAgent(agentSlug: string, skills: SkillDeclaration[]): void

  unregisterAgent(agentSlug: string): void

  scoreMatch(agentSlug: string, task: TaskDefinition): number

  findBestAgent(task: TaskDefinition): { agentSlug: string; score: number } | null

  findCapableAgents(task: TaskDefinition): Array<{ agentSlug: string; score: number }>
}
```

---

## 5. src/orchestration/executor.ts (Executor)

```typescript
/**
 * Concurrent agent executor for multi-agent orchestration.
 * Manages task checkout/release, concurrency limits, and lifecycle events.
 */

export type ExecutorEvent =
  | { type: 'task:start'; taskId: string; agentSlug: string }
  | { type: 'task:complete'; taskId: string; result?: string }
  | { type: 'task:fail'; taskId: string; error: string }
  | { type: 'task:timeout'; taskId: string };

export class OrchestratorExecutor {
  private config: OrchestratorConfig;
  private tasks: Map<string, TaskDefinition> = new Map();
  private locks: Map<string, string> = new Map(); // taskId -> agentSlug
  private activeCount = 0;

  constructor(config: OrchestratorConfig)

  on(handler: (event: ExecutorEvent) => void): () => void

  private emit(event: ExecutorEvent): void

  async checkout(taskId: string, agentSlug: string): Promise<boolean>
    // Atomic task checkout -- prevents duplicate assignment.
    // Returns true if the task was successfully locked for this agent.

  async release(taskId: string, result?: string, error?: string): Promise<void>
    // Release a task on completion or failure.

  async submit(task: TaskDefinition): Promise<void>
    // Submit a task for execution.

  async runBatch(tasks: TaskDefinition[]): Promise<Map<string, TaskDefinition>>
    // Run multiple tasks concurrently, respecting maxConcurrentAgents.

  getActive(): TaskDefinition[]
    // Get currently active (in_progress) tasks.

  async stopAll(): Promise<void>
    // Stop all running tasks.
}
```

---

## 6. src/orchestration/budget.ts (Budget Tracking)

```typescript
/**
 * Budget tracking for multi-agent orchestration.
 * Tracks per-agent token/cost usage against global and per-agent limits.
 */

interface AgentUsage {
  tokensUsed: number;
  costUsd: number;
}

export class BudgetTracker {
  private globalBudget: { maxTokens: number; maxCostUsd: number };
  private usage: Map<string, AgentUsage> = new Map();

  constructor(globalBudget: { maxTokens: number; maxCostUsd: number })

  record(agentSlug: string, tokens: number, costUsd: number): void
    // Track usage for an agent.

  getSnapshot(agentSlug: string): BudgetSnapshot
    // Get budget snapshot for a single agent.

  getAllSnapshots(): BudgetSnapshot[]
    // Get snapshots for all tracked agents.

  canProceed(agentSlug: string): boolean
    // Check if agent can proceed (not over budget).

  isWarning(agentSlug: string): boolean
    // Check if agent is in warning zone (>80% usage).

  reset(agentSlug: string): void
    // Reset tracked usage for an agent.

  getGlobalUsage(): {
    tokens: number;
    cost: number;
    percentTokens: number;
    percentCost: number;
  }
    // Get aggregate global usage across all agents.
}
```

---

## 7. src/orchestration/templates.ts (Workflow Templates)

```typescript
/**
 * Pre-built workflow templates for common multi-agent workflows.
 */

export interface WorkflowTemplate {
  name: string;
  description: string;
  goals: Omit<GoalDefinition, 'id' | 'createdAt' | 'tasks'>[];
  tasks: Omit<TaskDefinition, 'id' | 'createdAt' | 'status' | 'completedAt' | 'result'>[];
}

export const workflows: Record<string, WorkflowTemplate>
  // Templates:
  // - 'seo-audit': SEO Audit workflow
  // - 'lead-gen-pipeline': Lead Generation Pipeline
  // - 'enrichment-batch': Data Enrichment Batch
```

---

## 8. src/agent/types.ts (Agent Types)

```typescript
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
```

---

## 9. src/agent/providers/types.ts

```typescript
export type LLMProvider = 'gemini' | 'claude-code';

export interface ProviderConfig {
  provider: LLMProvider;
  geminiApiKey?: string;
}
```

---

## 10. src/agent/providers/claude-code.ts

```typescript
/**
 * Claude Code provider — spawns a bridge server and communicates via HTTP fetch.
 */

export function startBridge(): Promise<number>
  // Start the bridge server if not already running. Returns the port.

export function startBridgeSync(): number
  // Start the bridge synchronously — call BEFORE Ink takes over.

export function stopBridge(): void
  // Stop the bridge server.

export function getBridgePort(): number | null
  // Get the current bridge port, or null if not running.

export function createClaudeCodeStreamFn(
  _agentSlug?: string,
  _cwd?: string,
  _model?: string,
): StreamFn
  // Create a StreamFn backed by Claude Code bridge.

export function clearClaudeSession(_agentSlug: string): void
```

---

## 11. src/agent/providers/claude-bridge.ts

```typescript
/**
 * Claude Bridge — standalone HTTP server that spawns `claude -p` and returns the response.
 * Runs as a separate Node.js process.
 */

// HTTP Server endpoints:
// - GET  /health           → { ok: true }
// - POST /invoke           → { prompt: string } → { text: string; sessionId?: string }

interface InvokeRequest {
  prompt: string;
}

function parseClaudeOutput(stdout: string): { text: string; sessionId?: string }

function buildCleanEnv(): NodeJS.ProcessEnv
  // Build clean env (strip Claude nesting vars)

function handleInvoke(body: InvokeRequest): Promise<{ text: string; sessionId?: string }>
```

---

## 12. src/agent/providers/gemini.ts

```typescript
/**
 * Gemini LLM stream function for the Pi-style agent loop.
 * Uses @google/generative-ai SDK to stream responses from Gemini.
 */

export function createGeminiStreamFn(apiKey: string, model = "gemini-2.5-flash"): StreamFn

function convertSchema(schema: Record<string, unknown>): Record<string, unknown>
  // Convert a JSON Schema object to Gemini's FunctionDeclarationSchema format.

function mapType(jsonType?: string): SchemaType
  // Map JSON Schema types to Gemini SchemaType enum.

function sanitizeContents(
  contents: Array<{ role: string; parts: Array<{ text: string }> }>,
): Array<{ role: string; parts: Array<{ text: string }> }>
  // Ensure contents alternate between user/model roles (Gemini requirement).
```

---

## 13. src/agent/loop.ts (Agent Loop - First 150 Lines)

```typescript
/**
 * Pi-style state machine agent loop.
 * Runs idle -> build_context -> stream_llm -> execute_tool -> done cycle.
 */

type EventName = 'phase_change' | 'tool_call' | 'message' | 'error' | 'done';
type EventHandler = (...args: unknown[]) => void;

export function createNexAskStreamFn(client?: NexClient): StreamFn
  // Create a streamFn backed by the WUPHF Ask API.

export function createMockStreamFn(): StreamFn
  // @deprecated Use createNexAskStreamFn instead.

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
  private taskHadError = false;
  private collectedInsights: string[] = [];

  constructor(
    config: AgentConfig,
    tools: ToolRegistry,
    sessions: AgentSessionStore,
    queues: MessageQueues,
    streamFn?: StreamFn,
    gossipLayer?: GossipLayer,
    credibilityTracker?: CredibilityTracker,
  )

  private setPhase(phase: AgentPhase): void
  private emit(event: string, ...args: unknown[]): void
  on(event: EventName, handler: EventHandler): void
  off(event: string, handler: EventHandler): void
  getState(): AgentState
  async tick(): Promise<void>
  // ... (continues for ~1344 more lines)
}
```

---

## 14. src/agent/message-router.ts

```typescript
/**
 * MessageRouter: routes incoming messages to the best-fit agent
 * using TaskRouter for skill scoring and thread-detection heuristics
 * for follow-up messages.
 */

interface ThreadContext {
  agentSlug: string;
  lastActivity: number;
}

export interface RoutingResult {
  primary: string; // agent slug
  collaborators: string[];
  isFollowUp: boolean;
  teamLeadAware: boolean;
}

export class MessageRouter {
  private router = new TaskRouter();
  private recentThreads = new Map<string, ThreadContext>();
  private FOLLOWUP_WINDOW_MS = 30_000;

  registerAgent(slug: string, expertise: string[]): void

  unregisterAgent(slug: string): void

  recordAgentActivity(agentSlug: string): void

  route(
    message: string,
    availableAgents: Array<{ slug: string; expertise: string[] }>,
  ): RoutingResult

  private detectFollowUp(message: string): string | null

  extractSkills(message: string): string[]
}
```

---

## 15. src/agent/session-store.ts

```typescript
/**
 * DAG-based session persistence using JSONL files.
 * Each session is a JSONL file at ~/.wuphf/sessions/<agentSlug>/<sessionId>.jsonl
 * Branching creates a new session that copies history up to the branch point.
 */

export class AgentSessionStore {
  private baseDir: string;

  constructor(baseDir?: string)

  private agentDir(agentSlug: string): string

  private sessionPath(agentSlug: string, sessionId: string): string

  private extractAgentSlug(sessionId: string, fallback?: string): string

  create(agentSlug: string): string

  append(
    sessionId: string,
    entry: Omit<SessionEntry, 'id' | 'timestamp'>,
  ): SessionEntry

  getHistory(
    sessionId: string,
    options?: { limit?: number; fromId?: string },
  ): SessionEntry[]

  branch(sessionId: string, fromEntryId: string): string

  listSessions(agentSlug: string): string[]
}
```

---

## 16. src/agent/queues.ts

```typescript
/**
 * Steer + FollowUp message queues for agent control.
 * - steer: high-priority interrupts that preempt current execution
 * - followUp: normal-priority messages queued for the next turn
 */

export class MessageQueues {
  private steerQueues = new Map<string, string[]>();
  private followUpQueues = new Map<string, string[]>();

  private getQueue(map: Map<string, string[]>, agentSlug: string): string[]

  steer(agentSlug: string, message: string): void

  followUp(agentSlug: string, message: string): void

  drainSteer(agentSlug: string): string | undefined

  drainFollowUp(agentSlug: string): string | undefined

  hasSteer(agentSlug: string): boolean

  hasFollowUp(agentSlug: string): boolean

  hasMessages(agentSlug: string): boolean
}
```

---

## 17. src/agent/tools.ts (First 100 Lines - Tool Registry)

```typescript
/**
 * Runtime tool registry and built-in WUPHF tools.
 */

export class ToolRegistry {
  private tools = new Map<string, AgentTool>();

  register(tool: AgentTool): void

  unregister(name: string): void

  get(name: string): AgentTool | undefined

  list(): AgentTool[]

  has(name: string): boolean

  validate(toolName: string, params: unknown): { valid: boolean; errors?: string[] }
}

export function createBuiltinTools(client: NexClient): AgentTool[]
  // Built-in tools:
  // - nex_search: Search organizational knowledge base
  // - nex_ask: Ask a question to the organizational AI
  // - ... (more tools defined)
```

---

## 18. src/commands/dispatch.ts (Key Functions)

```typescript
/**
 * Command dispatch layer — executes CLI commands and returns structured results.
 * Bridge for both Commander CLI and Ink TUI.
 */

export interface CommandResult {
  output: string;
  data?: unknown;
  exitCode: number;
  error?: string;
  sessionId?: string;
  nav?: {
    objectSlug?: string;
    recordId?: string;
  };
}

export interface CommandContext {
  apiKey?: string;
  format?: "json" | "text" | "quiet";
  timeout?: number;
  sessionId?: string;
  debug?: boolean;
}

interface CommandEntry {
  execute: (args: string[], ctx: CommandContext) => Promise<CommandResult>;
  description: string;
  category: "query" | "write" | "config" | "ai" | "graph" | "agent";
  usage?: string;
}

// Helper functions:
function makeClient(ctx: CommandContext): NexClient
function fmt(data: unknown, ctx: CommandContext): string
function ok(data: unknown, ctx: CommandContext, extra?: Partial<CommandResult>): CommandResult
async function triggerCompounding(client: NexClient): Promise<void>
function fail(error: string, exitCode = 1): CommandResult
function wrapError(err: unknown): CommandResult
function extractOpts(args: string[]): { positional: string[]; opts: Record<string, string | true> }

// Command executors (all async):
async function executeAsk(args: string[], ctx: CommandContext): Promise<CommandResult>
async function executeRemember(args: string[], ctx: CommandContext): Promise<CommandResult>
// ... (many more command executors)

// Dispatcher:
export async function dispatch(input: string, ctx: CommandContext): Promise<CommandResult>
export async function dispatchTokens(args: string[], ctx: CommandContext): Promise<CommandResult>
export const commandNames: string[]
```

---

## 19. src/commands/register.ts

```typescript
/**
 * wuphf register — register a new developer account.
 */

program
  .command("register")
  .description("Register a new WUPHF workspace and get an API key")
  .requiredOption("--email <email>", "Email address")
  .option("--name <name>", "Your name")
  .option("--company <company>", "Company name")
  .action(async (opts: { email: string; name?: string; company?: string }) => {
    // Implementation
  });
```

---

## 20. src/index.ts (Entry Point)

```typescript
/**
 * Entry point.
 *
 * Interactive terminal → TUI (default)
 * --cmd <input>       → single command, print result, exit
 * Piped stdin         → read input, dispatch, exit
 */

function extractGlobalFlags(args: string[]): {
  cleanArgs: string[];
  ctx: {
    format: Format;
    apiKey?: string;
    timeout?: number
  }
}

function emitAndExit(result: {
  output: string;
  error?: string;
  exitCode: number
}): never

async function main(): Promise<void>
  // Main entry point with:
  // - --version flag
  // - --help flag
  // - --cmd "command" dispatcher
  // - Interactive TUI or Commander dispatch
  // - Piped stdin handling
```

---

## Error Classes (src/lib/errors.ts)

```typescript
export class AuthError extends Error
export class RateLimitError extends Error
export class ServerError extends Error
```

---

## Key Patterns

### 1. Event-Driven Architecture
- `ExecutorEvent` unions for task lifecycle
- `EventEmitter` pattern in `AgentLoop`
- Message queue pattern with `steer` and `followUp` priorities

### 2. Session Management
- JSONL-based persistence
- Session IDs prefixed with agent slug: `<slug>_<uuid>`
- DAG-based branching for session history

### 3. Provider Pattern
- `StreamFn` async generator for unified LLM interface
- Multiple providers: Claude Code, Gemini, WUPHF Ask
- Tool schema validation via JSON Schema

### 4. Configuration Resolution
- Priority: CLI flags > env vars > config file
- Single config at `~/.wuphf/config.json`
- Environment escape hatches (WUPHF_DEV_URL, WUPHF_API_KEY)

### 5. Multi-Agent Orchestration
- **Paperclip patterns**: flat task pool, expertise routing, atomic checkout
- **Budget tracking**: per-agent and global budgets with warning/exceeded thresholds
- **Task router**: fuzzy skill matching with proficiency weighting
- **Concurrency limits**: configurable max concurrent agents

### 6. Message Routing
- Thread detection for follow-up messages (30-second window)
- Skill extraction from message keywords
- Team-lead triage for unmatched messages

---

## Notes for Go Port

1. **No API types defined in CLI** — All API request/response types are implicit in dispatch.ts
2. **dispatch.ts is 1494 lines** — Contains all CLI command implementations
3. **Three LLM providers** — Claude Code (HTTP bridge), Gemini (SDK), WUPHF Ask (internal API)
4. **Session persistence** — JSONL format, agent-slug-prefixed IDs, branching support
5. **Budget tracking** — Two-level (per-agent + global), percentage-based warnings
6. **Configuration** — Single JSON file with optional nested config.dev_url for local dev


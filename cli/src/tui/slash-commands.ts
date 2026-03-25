/**
 * Slash command registry for the conversation-first TUI.
 *
 * Slash commands (/help, /agents, etc.) are the explicit navigation model.
 * Natural language input goes to the Pi agent via dispatch("ask ...").
 */

import type { ViewEntry } from "./store.js";
import type { CommandResult } from "../commands/dispatch.js";
import type { DetectedPlatform } from "../commands/init.js";
import type { SelectOption } from "./components/inline-select.js";

// ── Public types ────────────────────────────────────────────────────

export interface SlashCommand {
  name: string;
  description: string;
  usage?: string;
  execute: (args: string, context: SlashCommandContext) => Promise<SlashCommandResult>;
}

export interface SlashCommandContext {
  push: (view: ViewEntry) => void;
  dispatch: (input: string) => Promise<CommandResult>;
  addMessage: (msg: ConversationMessage) => void;
  setLoading: (loading: boolean, hint?: string) => void;
  /** Show an arrow-key navigable inline select picker. */
  showPicker: (title: string, options: SelectOption[], onSelect: (value: string) => void) => void;
  /** Clear the current picker. */
  clearPicker: () => void;
  /** Show an inline y/n confirm prompt. */
  showConfirm: (question: string, onConfirm: (confirmed: boolean) => void) => void;
  /** Clear the current confirm prompt. */
  clearConfirm: () => void;
}

export interface SlashCommandResult {
  output?: string;
  silent?: boolean;
}

export type ConversationMessage = {
  id: string;
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  timestamp: number;
  toolName?: string;
  isError?: boolean;
  /** When true, MessageBubble shows a trailing cursor block ▌ */
  isStreaming?: boolean;
};

// ── Registry ────────────────────────────────────────────────────────

const commands = new Map<string, SlashCommand>();

export function registerSlashCommand(cmd: SlashCommand): void {
  commands.set(cmd.name, cmd);
}

export function getSlashCommand(name: string): SlashCommand | undefined {
  return commands.get(name);
}

export function listSlashCommands(): SlashCommand[] {
  return Array.from(commands.values());
}

/**
 * Parse raw input to determine if it is a slash command.
 * Returns `{ isSlash: true, command, args }` for "/foo bar baz",
 * or `{ isSlash: false }` for everything else.
 */
export function parseSlashInput(input: string): {
  isSlash: boolean;
  command?: string;
  args?: string;
} {
  const trimmed = input.trim();
  if (!trimmed.startsWith("/")) return { isSlash: false };

  const withoutSlash = trimmed.slice(1);
  const spaceIdx = withoutSlash.indexOf(" ");
  if (spaceIdx === -1) {
    return { isSlash: true, command: withoutSlash, args: "" };
  }
  return {
    isSlash: true,
    command: withoutSlash.slice(0, spaceIdx),
    args: withoutSlash.slice(spaceIdx + 1).trim(),
  };
}

// ── Init flow state machine ─────────────────────────────────────────

export type InitState =
  | { phase: "idle" }
  | { phase: "awaiting_email"; loginOnly?: boolean }
  | { phase: "awaiting_new_email"; loginOnly?: boolean }
  | { phase: "awaiting_gemini_key" }
  | { phase: "awaiting_agent_prompt" };

let initState: InitState = { phase: "idle" };

export function getInitState(): InitState {
  return initState;
}

export function resetInitState(): void {
  initState = { phase: "idle" };
}

/**
 * Run platform detection and installation (non-interactive).
 * Called after we have a validated API key.
 */
async function runPlatformInstall(
  apiKey: string,
  ctx: SlashCommandContext,
): Promise<void> {
  ctx.setLoading(true, "Detecting platforms...");

  const { detectPlatforms, installForPlatform } = await import("../commands/init.js");
  const { syncApiKeyToMcpConfig } = await import("../lib/installers.js");

  // Sync API key to ~/.wuphf/config.json
  syncApiKeyToMcpConfig(apiKey);

  let platforms: DetectedPlatform[];
  try {
    platforms = detectPlatforms().filter((p) => p.detected);
  } catch (err) {
    ctx.setLoading(false);
    ctx.addMessage({
      id: `init-detect-err-${Date.now()}`,
      role: "system",
      content: `Platform detection failed: ${err instanceof Error ? err.message : String(err)}`,
      timestamp: Date.now(),
      isError: true,
    });
    return;
  }

  ctx.setLoading(false);

  if (platforms.length === 0) {
    ctx.addMessage({
      id: `init-no-platforms-${Date.now()}`,
      role: "system",
      content: "No supported AI coding platforms detected. API key is saved and ready to use.",
      timestamp: Date.now(),
    });
    promptProviderChoice(ctx);
    return;
  }

  const lines: string[] = [`Found ${platforms.length} platform(s):\n`];

  for (const platform of platforms) {
    ctx.setLoading(true, `Installing for ${platform.name}...`);
    const results: string[] = [];

    try {
      installForPlatform(platform, apiKey, (progress) => {
        if (progress.detail) results.push(progress.detail);
      });
    } catch {
      // Installation errors are reported via the callback
    }

    const status =
      results.length > 0
        ? results.join(", ")
        : platform.nexInstalled
          ? "already installed"
          : "detected";
    lines.push(`  - ${platform.name}: ${status}`);
  }

  ctx.setLoading(false);
  ctx.addMessage({
    id: `init-platforms-${Date.now()}`,
    role: "system",
    content: lines.join("\n"),
    timestamp: Date.now(),
  });

  promptProviderChoice(ctx);
}

/**
 * Show the LLM provider choice picker after platform install.
 */
function promptProviderChoice(ctx: SlashCommandContext): void {
  ctx.addMessage({
    id: `init-provider-${Date.now()}`,
    role: "system",
    content: "Choose how your agents will think:",
    timestamp: Date.now(),
  });

  ctx.showPicker(
    "LLM Provider",
    [
      { label: "Gemini via WUPHF", value: "gemini", description: "Google Gemini powers your agents (default)" },
      { label: "Claude Code", value: "claude-code", description: "Use Claude Code with WUPHF as a plugin" },
    ],
    (choice) => handleProviderChoice(choice, ctx),
  );
}

async function handleProviderChoice(choice: string, ctx: SlashCommandContext): Promise<void> {
  if (choice === "gemini") {
    ctx.addMessage({
      id: `init-gemini-key-${Date.now()}`,
      role: "system",
      content: "Enter your Gemini API key (from ai.google.dev):",
      timestamp: Date.now(),
    });
    initState = { phase: "awaiting_gemini_key" };
    return;
  }

  if (choice === "claude-code") {
    ctx.setLoading(true, "Installing WUPHF plugin for Claude Code...");
    try {
      const { installClaudeCodePlugin } = await import("../lib/claude-code-installer.js");
      const result = installClaudeCodePlugin();
      ctx.setLoading(false);

      // Save provider choice
      const { loadConfig, saveConfig } = await import("../lib/config.js");
      const config = loadConfig();
      (config as Record<string, unknown>).llm_provider = "claude-code";
      saveConfig(config);

      ctx.addMessage({
        id: `init-claude-code-${Date.now()}`,
        role: "system",
        content: result.success
          ? result.message
          : `Plugin install failed: ${result.message}`,
        timestamp: Date.now(),
        isError: !result.success,
      });
    } catch (err) {
      ctx.setLoading(false);
      ctx.addMessage({
        id: `init-claude-err-${Date.now()}`,
        role: "system",
        content: `Failed: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
    }
    promptAgentChoice(ctx);
    return;
  }
}

/**
 * Show the agent creation choice picker after provider selection.
 * Skips the picker if agents already exist.
 */
async function promptAgentChoice(ctx: SlashCommandContext): Promise<void> {
  const { getAgentService } = await import("./services/agent-service.js");
  const existingAgents = getAgentService().list();
  if (existingAgents.length > 0) {
    ctx.addMessage({
      id: `init-agents-exist-${Date.now()}`,
      role: "system",
      content: `Your team is ready! You have ${existingAgents.length} agent(s). Use /agents to manage them.`,
      timestamp: Date.now(),
    });
    initState = { phase: "idle" };
    return;
  }

  ctx.addMessage({
    id: `init-agent-choice-${Date.now()}`,
    role: "system",
    content:
      "Setup complete! Your workspace is ready.\n\n" +
      "Pick an AI agent to start with — your first team member.\n" +
      "It will work autonomously on your behalf. You can add more agents later.",
    timestamp: Date.now(),
  });

  ctx.showPicker(
    "Which agent should join your team first?",
    [
      { label: "Pick from specialists", value: "templates", description: "SEO, Lead Gen, Research, Customer Success, etc." },
      { label: "Describe what you need", value: "custom", description: "Tell us in plain English — AI builds the agent" },
      { label: "Team Lead", value: "founding", description: "A generalist that handles everything until you add specialists" },
      { label: "Skip — I'll add agents later", value: "skip" },
    ],
    (choice) => handleAgentChoice(choice, ctx),
  );
}

/**
 * Show the template selection picker.
 */
function promptTemplateChoice(ctx: SlashCommandContext): void {
  // Picker handles interaction — no state machine phase needed

  const templateOptions: SelectOption[] = TEMPLATE_MENU.map((t) => {
    const tmpl = TEMPLATE_LABELS[t.templateKey];
    return {
      label: tmpl?.name ?? t.templateKey,
      value: t.templateKey,
      description: tmpl?.expertise ?? "",
    };
  });

  // Add a back option
  templateOptions.push({ label: "Back", value: "back" });

  ctx.showPicker("Pick a specialist agent:", templateOptions, (value) => {
    if (value === "back") {
      promptAgentChoice(ctx);
      return;
    }
    handleTemplateSelect(value, ctx);
  });
}

/**
 * Show confirmation after creating an agent.
 */
function showAgentCreated(ctx: SlashCommandContext, name: string, slug: string, expertise: string[], cron: string): void {
  ctx.addMessage({
    id: `init-agent-created-${Date.now()}`,
    role: "system",
    content:
      `Created "${name}" agent (${slug})\n` +
      `  Expertise: ${expertise.join(", ")}\n` +
      `  Schedule: ${cron} heartbeat\n\n` +
      `Your agent is ready! It will start working on its next heartbeat.\n` +
      `Use /agents to see your team, or just start chatting.`,
    timestamp: Date.now(),
  });
  initState = { phase: "idle" };
}

/** Template slugs indexed by menu number. */
const TEMPLATE_MENU: Array<{ slug: string; templateKey: string }> = [
  { slug: "seo-agent", templateKey: "seo-agent" },
  { slug: "lead-gen", templateKey: "lead-gen" },
  { slug: "enrichment", templateKey: "enrichment" },
  { slug: "research", templateKey: "research" },
  { slug: "customer-success", templateKey: "customer-success" },
];

/** Display labels for template picker options. */
const TEMPLATE_LABELS: Record<string, { name: string; expertise: string }> = {
  "seo-agent": { name: "SEO Analyst", expertise: "keyword research, content analysis, search visibility" },
  "lead-gen": { name: "Lead Generator", expertise: "prospecting, enrichment, outreach" },
  "enrichment": { name: "Data Enricher", expertise: "research, validation, missing data" },
  "research": { name: "Research Analyst", expertise: "market research, competitive analysis, trends" },
  "customer-success": { name: "Customer Success", expertise: "relationship management, health scoring, renewals" },
};

/**
 * Handle agent choice picker selection.
 */
async function handleAgentChoice(choice: string, ctx: SlashCommandContext): Promise<void> {
  if (choice === "templates") {
    promptTemplateChoice(ctx);
    return;
  }

  if (choice === "custom") {
    ctx.addMessage({
      id: `init-agent-prompt-${Date.now()}`,
      role: "system",
      content:
        'Describe what you want your agent to do:\n(e.g., "Monitor competitor pricing and alert me to changes")',
      timestamp: Date.now(),
    });
    initState = { phase: "awaiting_agent_prompt" };
    return;
  }

  if (choice === "founding") {
    ctx.setLoading(true, "Creating Team Lead...");
    try {
      const { getAgentService } = await import("./services/agent-service.js");
      const service = getAgentService();
      const managed = service.createFromTemplate("founding-agent", "founding-agent");
      ctx.setLoading(false);
      showAgentCreated(
        ctx,
        managed.config.name,
        managed.config.slug,
        managed.config.expertise,
        managed.config.heartbeatCron ?? "daily",
      );
    } catch (err) {
      ctx.setLoading(false);
      ctx.addMessage({
        id: `init-agent-err-${Date.now()}`,
        role: "system",
        content: `Failed to create agent: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
      initState = { phase: "idle" };
    }
    return;
  }

  if (choice === "skip") {
    ctx.addMessage({
      id: `init-skip-agent-${Date.now()}`,
      role: "system",
      content: "Skipped agent creation. Use /agents or /agent create <slug> --template <name> anytime.",
      timestamp: Date.now(),
    });
    initState = { phase: "idle" };
    return;
  }
}

/**
 * Handle template picker selection.
 */
async function handleTemplateSelect(templateKey: string, ctx: SlashCommandContext): Promise<void> {
  const entry = TEMPLATE_MENU.find((t) => t.templateKey === templateKey);
  if (!entry) {
    promptTemplateChoice(ctx);
    return;
  }

  ctx.setLoading(true, "Creating agent...");
  try {
    const { getAgentService } = await import("./services/agent-service.js");
    const service = getAgentService();
    const managed = service.createFromTemplate(entry.slug, entry.templateKey);
    ctx.setLoading(false);
    showAgentCreated(
      ctx,
      managed.config.name,
      managed.config.slug,
      managed.config.expertise,
      managed.config.heartbeatCron ?? "daily",
    );
  } catch (err) {
    ctx.setLoading(false);
    ctx.addMessage({
      id: `init-template-err-${Date.now()}`,
      role: "system",
      content: `Failed to create agent: ${err instanceof Error ? err.message : String(err)}`,
      timestamp: Date.now(),
      isError: true,
    });
    initState = { phase: "idle" };
  }
}

/**
 * Handle expired key choice via picker.
 */
async function handleRegenChoice(
  choice: string,
  currentEmail: string | undefined,
  currentKey: string,
  ctx: SlashCommandContext,
): Promise<void> {
  if (choice === "regenerate") {
    if (!currentEmail) {
      ctx.addMessage({
        id: `init-no-email-${Date.now()}`,
        role: "system",
        content: "No email on file. What's your email address?",
        timestamp: Date.now(),
      });
      initState = { phase: "awaiting_email" };
      return;
    }

    ctx.setLoading(true, "Generating new key...");
    try {
      const { registerUser } = await import("../commands/init.js");
      const result = await registerUser(currentEmail);
      ctx.setLoading(false);

      const maskedKey = `${result.apiKey.slice(0, 6)}...${result.apiKey.slice(-4)}`;
      ctx.addMessage({
        id: `init-regen-${Date.now()}`,
        role: "system",
        content: `New key generated: ${maskedKey}`,
        timestamp: Date.now(),
      });

      await runPlatformInstall(result.apiKey, ctx);
    } catch (err) {
      ctx.setLoading(false);
      ctx.addMessage({
        id: `init-regen-err-${Date.now()}`,
        role: "system",
        content: `Key regeneration failed: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
      initState = { phase: "idle" };
    }
    return;
  }

  if (choice === "new-email") {
    ctx.addMessage({
      id: `init-new-email-${Date.now()}`,
      role: "system",
      content: "What's your email address?",
      timestamp: Date.now(),
    });
    initState = { phase: "awaiting_new_email" };
    return;
  }

  // "skip" — keep current key
  ctx.addMessage({
    id: `init-keep-${Date.now()}`,
    role: "system",
    content: "Keeping current key.",
    timestamp: Date.now(),
  });
  await runPlatformInstall(currentKey, ctx);
}

/**
 * Handle agent confirm via inline confirm.
 */
async function handleAgentConfirm(
  confirmed: boolean,
  config: Record<string, unknown>,
  ctx: SlashCommandContext,
): Promise<void> {
  if (confirmed) {
    ctx.setLoading(true, "Creating agent...");
    try {
      const { getAgentService } = await import("./services/agent-service.js");
      const service = getAgentService();
      const agentConfig = {
        slug: config.slug as string,
        name: config.name as string,
        expertise: config.expertise as string[],
        personality: config.personality as string,
        heartbeatCron: config.heartbeatCron as string,
        tools: config.tools as string[],
      };
      const managed = service.create(agentConfig);
      ctx.setLoading(false);
      showAgentCreated(
        ctx,
        managed.config.name,
        managed.config.slug,
        managed.config.expertise,
        managed.config.heartbeatCron ?? "daily",
      );
    } catch (err) {
      ctx.setLoading(false);
      ctx.addMessage({
        id: `init-agent-create-err-${Date.now()}`,
        role: "system",
        content: `Failed to create agent: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
      initState = { phase: "idle" };
    }
    return;
  }

  // Declined
  ctx.addMessage({
    id: `init-agent-declined-${Date.now()}`,
    role: "system",
    content: "Agent creation cancelled.",
    timestamp: Date.now(),
  });
  promptAgentChoice(ctx);
}

/**
 * Build an agent config from a free-text user prompt.
 */
function buildAgentFromPrompt(prompt: string): Record<string, unknown> {
  const slug = prompt
    .split(/\s+/)
    .slice(0, 3)
    .join("-")
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "");

  // Capitalize first letter of each of the first few words for the name
  const name = prompt
    .split(/\s+/)
    .slice(0, 4)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
    .join(" ");

  // Extract expertise keywords: split on common delimiters, take meaningful words
  const stopWords = new Set(["and", "the", "a", "an", "to", "for", "of", "in", "on", "my", "me", "i", "is", "it", "that", "this", "with"]);
  const expertise = prompt
    .toLowerCase()
    .split(/[\s,;.]+/)
    .filter((w) => w.length > 2 && !stopWords.has(w))
    .slice(0, 5);

  return {
    slug: slug || "custom-agent",
    name: name || "Custom Agent",
    expertise: expertise.length > 0 ? expertise : ["general"],
    personality: `You are an AI agent specialized in: ${prompt}`,
    heartbeatCron: "daily",
    tools: ["nex_search", "nex_ask", "nex_remember"],
  };
}

/**
 * Handle user input when the init flow is awaiting a text response.
 * Called by the home adapter when `getInitState().phase !== 'idle'`.
 *
 * Picker-based choices (regen, agent, template) are now handled via
 * showPicker callbacks -- only text-input phases remain here.
 */
export async function handleInitInput(
  input: string,
  context: SlashCommandContext,
): Promise<void> {
  if (initState.phase === "awaiting_email" || initState.phase === "awaiting_new_email") {
    const email = input.trim();
    if (!email || !email.includes("@")) {
      context.addMessage({
        id: `init-email-err-${Date.now()}`,
        role: "system",
        content: "Please enter a valid email address.",
        timestamp: Date.now(),
      });
      return;
    }

    context.setLoading(true, "Registering...");
    try {
      const { registerUser } = await import("../commands/init.js");
      const result = await registerUser(email);
      context.setLoading(false);

      const maskedKey = `${result.apiKey.slice(0, 6)}...${result.apiKey.slice(-4)}`;
      const isLoginOnly = (initState as { loginOnly?: boolean }).loginOnly;
      context.addMessage({
        id: `init-registered-${Date.now()}`,
        role: "system",
        content:
          `Logged in! API key: ${maskedKey}\n` +
          `Workspace: ${result.workspaceSlug}\n\n` +
          `Saved to ~/.wuphf/config.json` +
          (isLoginOnly ? "\n\nRun /init to install WUPHF into your AI agents." : ""),
        timestamp: Date.now(),
      });

      // Proceed to platform installation (unless login-only)
      if (!isLoginOnly) {
        await runPlatformInstall(result.apiKey, context);
        // runPlatformInstall may have advanced state to agent onboarding
        return;
      }
    } catch (err) {
      context.setLoading(false);
      context.addMessage({
        id: `init-reg-err-${Date.now()}`,
        role: "system",
        content: `Registration failed: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
    }

    initState = { phase: "idle" };
    return;
  }

  if (initState.phase === "awaiting_gemini_key") {
    const key = input.trim();
    if (!key) {
      context.addMessage({
        id: `init-gemini-empty-${Date.now()}`,
        role: "system",
        content: "Please enter your Gemini API key.",
        timestamp: Date.now(),
      });
      return;
    }

    try {
      const { loadConfig, saveConfig } = await import("../lib/config.js");
      const config = loadConfig();
      (config as Record<string, unknown>).gemini_api_key = key;
      (config as Record<string, unknown>).llm_provider = "gemini";
      saveConfig(config);

      const masked = `${key.slice(0, 6)}...${key.slice(-4)}`;
      context.addMessage({
        id: `init-gemini-saved-${Date.now()}`,
        role: "system",
        content: `Gemini API key saved: ${masked}`,
        timestamp: Date.now(),
      });
    } catch (err) {
      context.addMessage({
        id: `init-gemini-err-${Date.now()}`,
        role: "system",
        content: `Failed to save key: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
    }

    initState = { phase: "idle" };
    promptAgentChoice(context);
    return;
  }

  if (initState.phase === "awaiting_agent_prompt") {
    const prompt = input.trim();
    if (!prompt) {
      context.addMessage({
        id: `init-prompt-empty-${Date.now()}`,
        role: "system",
        content: "Please describe what you want your agent to do.",
        timestamp: Date.now(),
      });
      return;
    }

    // Try AI-powered generation via dispatch, fall back to local parsing
    context.setLoading(true, "Generating agent profile...");
    let config: Record<string, unknown>;

    try {
      const aiResult = await context.dispatch(
        `ask Generate an agent profile as JSON for this task: "${prompt}". Return ONLY JSON with fields: name (string), expertise (string[]), personality (string), heartbeatCron (string), tools (string[] from: nex_search, nex_ask, nex_remember, nex_record_list, nex_record_get, nex_record_create, nex_record_update)`,
      );

      // Try to parse AI result as JSON
      let parsed: Record<string, unknown> | null = null;
      if (aiResult.output) {
        try {
          // Extract JSON from the response (may be wrapped in markdown code blocks)
          const jsonMatch = aiResult.output.match(/\{[\s\S]*\}/);
          if (jsonMatch) {
            parsed = JSON.parse(jsonMatch[0]) as Record<string, unknown>;
          }
        } catch {
          // JSON parse failed, fall through to local builder
        }
      }

      if (parsed && typeof parsed.name === "string" && Array.isArray(parsed.expertise)) {
        const slug = (parsed.name as string)
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-|-$/g, "");
        config = {
          slug: slug || "custom-agent",
          name: parsed.name,
          expertise: parsed.expertise,
          personality: (parsed.personality as string) ?? `You are an AI agent specialized in: ${prompt}`,
          heartbeatCron: (parsed.heartbeatCron as string) ?? "daily",
          tools: Array.isArray(parsed.tools) ? parsed.tools : ["nex_search", "nex_ask", "nex_remember"],
        };
      } else {
        config = buildAgentFromPrompt(prompt);
      }
    } catch {
      // AI generation failed, use local builder
      config = buildAgentFromPrompt(prompt);
    }

    context.setLoading(false);

    // Show generated profile and ask for confirmation via inline confirm
    const expertise = config.expertise as string[];
    context.addMessage({
      id: `init-agent-preview-${Date.now()}`,
      role: "system",
      content:
        `Here's the agent I'll create:\n\n` +
        `  Name:      ${config.name as string}\n` +
        `  Slug:      ${config.slug as string}\n` +
        `  Expertise: ${expertise.join(", ")}\n` +
        `  Schedule:  ${config.heartbeatCron as string} heartbeat`,
      timestamp: Date.now(),
    });

    // ConfirmInput handles interaction — set idle so text input isn't blocked
    initState = { phase: "idle" };

    context.showConfirm(
      "Create this agent?",
      (confirmed) => handleAgentConfirm(confirmed, config, context),
    );
    return;
  }
}

// ── Agent Wizard ─────────────────────────────────────────────────────

export type AgentWizardState =
  | { phase: "idle" }
  | { phase: "agent_create_name" }
  | { phase: "agent_create_expertise"; slug: string; name: string }
  | { phase: "agent_create_describe" }
  | { phase: "agent_edit_name"; slug: string }
  | { phase: "agent_edit_expertise"; slug: string };

let agentWizardState: AgentWizardState = { phase: "idle" };

export function getAgentWizardState(): AgentWizardState {
  return agentWizardState;
}

export function resetAgentWizardState(): void {
  agentWizardState = { phase: "idle" };
}

const SCHEDULE_OPTIONS: SelectOption[] = [
  { label: "Hourly", value: "hourly", description: "every hour" },
  { label: "Every 4 hours", value: "0 */4 * * *", description: "6x per day" },
  { label: "Every 6 hours", value: "0 */6 * * *", description: "4x per day" },
  { label: "Daily", value: "daily", description: "once per day" },
  { label: "Weekly (Mon 9am)", value: "0 9 * * 1", description: "every Monday" },
  { label: "Manual only", value: "manual", description: "no automatic runs" },
];

function formatSchedule(cron?: string): string {
  if (!cron) return "daily";
  const found = SCHEDULE_OPTIONS.find((o) => o.value === cron);
  return found ? found.label : cron;
}

function slugify(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "agent";
}

async function createAgentFromConfig(
  config: Record<string, unknown>,
  ctx: SlashCommandContext,
): Promise<void> {
  ctx.setLoading(true, "Creating agent...");
  try {
    const { getAgentService } = await import("./services/agent-service.js");
    const service = getAgentService();
    const managed = service.create({
      slug: config.slug as string,
      name: config.name as string,
      expertise: config.expertise as string[],
      personality:
        (config.personality as string) ??
        `You are an AI agent specializing in: ${(config.expertise as string[]).join(", ")}`,
      heartbeatCron: (config.heartbeatCron as string) ?? "daily",
      tools: Array.isArray(config.tools)
        ? (config.tools as string[])
        : ["nex_search", "nex_ask", "nex_remember"],
    });
    ctx.setLoading(false);
    ctx.addMessage({
      id: `agent-created-${Date.now()}`,
      role: "system",
      content:
        `Created "${managed.config.name}" (${managed.config.slug})\n` +
        `  Expertise: ${managed.config.expertise.join(", ")}\n` +
        `  Schedule:  ${formatSchedule(managed.config.heartbeatCron)}\n\n` +
        `Agent is ready and will run on its next heartbeat.`,
      timestamp: Date.now(),
    });
  } catch (err) {
    ctx.setLoading(false);
    ctx.addMessage({
      id: `agent-create-err-${Date.now()}`,
      role: "system",
      content: `Failed to create agent: ${err instanceof Error ? err.message : String(err)}`,
      timestamp: Date.now(),
      isError: true,
    });
  }
}

async function createFromTemplateKey(templateKey: string, ctx: SlashCommandContext): Promise<void> {
  const { getAgentService } = await import("./services/agent-service.js");
  const service = getAgentService();
  const template = service.getTemplate(templateKey);
  if (!template) {
    ctx.addMessage({
      id: `agent-tmpl-err-${Date.now()}`,
      role: "system",
      content: `Unknown template: ${templateKey}`,
      timestamp: Date.now(),
      isError: true,
    });
    return;
  }

  ctx.addMessage({
    id: `agent-tmpl-preview-${Date.now()}`,
    role: "system",
    content:
      `About to create:\n\n` +
      `  Name:      ${template.name}\n` +
      `  Expertise: ${template.expertise.join(", ")}\n` +
      `  Schedule:  ${formatSchedule(template.heartbeatCron)}`,
    timestamp: Date.now(),
  });

  ctx.showConfirm(`Create "${template.name}" agent?`, async (confirmed) => {
    if (!confirmed) {
      showAgentCreatePicker(ctx);
      return;
    }
    const existing = service.get(templateKey);
    const slug = existing ? `${templateKey}-${Date.now()}` : templateKey;
    await createAgentFromConfig(
      {
        slug,
        name: template.name,
        expertise: template.expertise,
        personality: template.personality,
        heartbeatCron: template.heartbeatCron,
        tools: template.tools,
      },
      ctx,
    );
  });
}

function showAgentCreatePicker(ctx: SlashCommandContext): void {
  ctx.showPicker(
    "Create an agent:",
    [
      { label: "Team Lead", value: "tmpl:founding-agent", description: "generalist — handles everything" },
      { label: "SEO Analyst", value: "tmpl:seo-agent", description: "keywords, content, search visibility" },
      { label: "Lead Generator", value: "tmpl:lead-gen", description: "prospecting, enrichment, outreach" },
      { label: "Data Enricher", value: "tmpl:enrichment", description: "research, validation, missing data" },
      { label: "Research Analyst", value: "tmpl:research", description: "market research, trends, analysis" },
      { label: "Customer Success", value: "tmpl:customer-success", description: "health scoring, renewals" },
      { label: "Describe what you need (AI generates)", value: "custom:describe", description: "" },
      { label: "Fill in details manually", value: "custom:manual", description: "step-by-step" },
      { label: "Back", value: "back" },
    ],
    (value) => { void handleCreatePickerSelect(value, ctx); },
  );
}

async function handleCreatePickerSelect(value: string, ctx: SlashCommandContext): Promise<void> {
  if (value === "back") {
    await openAgentsManager(ctx);
    return;
  }
  if (value.startsWith("tmpl:")) {
    await createFromTemplateKey(value.slice(5), ctx);
    return;
  }
  if (value === "custom:describe") {
    agentWizardState = { phase: "agent_create_describe" };
    ctx.addMessage({
      id: `agent-wizard-desc-${Date.now()}`,
      role: "system",
      content:
        "Describe what you want this agent to do:\n" +
        "(e.g., \"Monitor competitor pricing and alert me to changes\")",
      timestamp: Date.now(),
    });
    return;
  }
  if (value === "custom:manual") {
    agentWizardState = { phase: "agent_create_name" };
    ctx.addMessage({
      id: `agent-wizard-name-${Date.now()}`,
      role: "system",
      content: "What should this agent be called?\n(e.g., Sales Assistant, Content Writer)",
      timestamp: Date.now(),
    });
  }
}

async function showAgentManagePicker(slug: string, ctx: SlashCommandContext): Promise<void> {
  const { getAgentService } = await import("./services/agent-service.js");
  const service = getAgentService();
  const managed = service.get(slug);
  if (!managed) {
    ctx.addMessage({
      id: `agent-not-found-${Date.now()}`,
      role: "system",
      content: `Agent "${slug}" not found.`,
      timestamp: Date.now(),
      isError: true,
    });
    await openAgentsManager(ctx);
    return;
  }

  const isRunning = managed.state.phase !== "idle" && managed.state.phase !== "done" && managed.state.phase !== "error";
  ctx.showPicker(
    `Manage "${managed.config.name}":`,
    [
      isRunning
        ? { label: "Stop", value: "stop", description: "pause the agent" }
        : { label: "Start", value: "start", description: "run a heartbeat tick" },
      { label: "Edit name", value: "edit:name", description: `currently: ${managed.config.name}` },
      {
        label: "Edit expertise",
        value: "edit:expertise",
        description: managed.config.expertise.slice(0, 3).join(", "),
      },
      {
        label: "Edit schedule",
        value: "edit:schedule",
        description: formatSchedule(managed.config.heartbeatCron),
      },
      { label: "Delete agent", value: "delete", description: "remove permanently" },
      { label: "Back", value: "back" },
    ],
    (value) => { void handleManageAction(value, slug, ctx); },
  );
}

async function handleManageAction(
  value: string,
  slug: string,
  ctx: SlashCommandContext,
): Promise<void> {
  if (value === "back") {
    await openAgentsManager(ctx);
    return;
  }

  const { getAgentService } = await import("./services/agent-service.js");
  const service = getAgentService();
  const managed = service.get(slug);
  if (!managed) {
    ctx.addMessage({
      id: `agent-gone-${Date.now()}`,
      role: "system",
      content: `Agent "${slug}" no longer exists.`,
      timestamp: Date.now(),
      isError: true,
    });
    return;
  }

  if (value === "start") {
    ctx.setLoading(true, `Starting ${managed.config.name}...`);
    try {
      await service.start(slug);
      ctx.setLoading(false);
      ctx.addMessage({
        id: `agent-start-${Date.now()}`,
        role: "system",
        content: `${managed.config.name} started.`,
        timestamp: Date.now(),
      });
    } catch (err) {
      ctx.setLoading(false);
      ctx.addMessage({
        id: `agent-start-err-${Date.now()}`,
        role: "system",
        content: `Start failed: ${err instanceof Error ? err.message : String(err)}`,
        timestamp: Date.now(),
        isError: true,
      });
    }
    return;
  }

  if (value === "stop") {
    service.stop(slug);
    ctx.addMessage({
      id: `agent-stop-${Date.now()}`,
      role: "system",
      content: `${managed.config.name} stopped.`,
      timestamp: Date.now(),
    });
    return;
  }

  if (value === "edit:name") {
    agentWizardState = { phase: "agent_edit_name", slug };
    ctx.addMessage({
      id: `agent-edit-name-${Date.now()}`,
      role: "system",
      content: `New name for "${managed.config.name}":`,
      timestamp: Date.now(),
    });
    return;
  }

  if (value === "edit:expertise") {
    agentWizardState = { phase: "agent_edit_expertise", slug };
    ctx.addMessage({
      id: `agent-edit-exp-${Date.now()}`,
      role: "system",
      content:
        `Current expertise: ${managed.config.expertise.join(", ")}\n\n` +
        `Enter new expertise (comma-separated):`,
      timestamp: Date.now(),
    });
    return;
  }

  if (value === "edit:schedule") {
    ctx.showPicker(
      `Schedule for "${managed.config.name}":`,
      SCHEDULE_OPTIONS.map((o) => ({
        ...o,
        label: o.value === managed.config.heartbeatCron ? `${o.label} ✓` : o.label,
      })),
      (scheduleValue) => {
        service.updateConfig(slug, { heartbeatCron: scheduleValue });
        ctx.addMessage({
          id: `agent-schedule-${Date.now()}`,
          role: "system",
          content: `${managed.config.name} schedule updated to: ${formatSchedule(scheduleValue)}`,
          timestamp: Date.now(),
        });
      },
    );
    return;
  }

  if (value === "delete") {
    ctx.showConfirm(
      `Delete "${managed.config.name}" permanently?`,
      async (confirmed) => {
        if (!confirmed) {
          await showAgentManagePicker(slug, ctx);
          return;
        }
        const name = managed.config.name;
        service.remove(slug);
        ctx.addMessage({
          id: `agent-deleted-${Date.now()}`,
          role: "system",
          content: `${name} has been removed.`,
          timestamp: Date.now(),
        });
      },
    );
  }
}

/**
 * Main entry point for the /agents interactive manager.
 * Exported so SlackHome can call it from the sidebar `[a]` shortcut.
 */
export async function openAgentsManager(ctx: SlashCommandContext): Promise<void> {
  const { getAgentService } = await import("./services/agent-service.js");
  const service = getAgentService();
  const agents = service.list();

  if (agents.length > 0) {
    const lines = agents.map((a) => {
      const phase = a.state.phase;
      const icon = (phase !== "idle" && phase !== "done" && phase !== "error") ? "●" : phase === "idle" ? "○" : "◐";
      const expertise = a.config.expertise.slice(0, 3).join(", ");
      return `  ${icon} ${a.config.name} (${a.config.slug})  [${phase}]  ${expertise}`;
    });
    ctx.addMessage({
      id: `agents-list-${Date.now()}`,
      role: "system",
      content: `Your agents (${agents.length}):\n\n${lines.join("\n")}`,
      timestamp: Date.now(),
    });
  }

  const agentOptions: SelectOption[] = agents.map((a) => ({
    label: a.config.name,
    value: `manage:${a.config.slug}`,
    description: `[${a.state.phase}]  ${a.config.expertise.slice(0, 2).join(", ")}`,
  }));

  ctx.showPicker(
    agents.length === 0 ? "No agents yet. Create your first one:" : "Agent Manager:",
    [
      { label: "+ Create new agent", value: "create" },
      ...agentOptions,
      { label: "Done", value: "cancel" },
    ],
    (value) => { void handleAgentsPickerSelect(value, ctx); },
  );
}

async function handleAgentsPickerSelect(value: string, ctx: SlashCommandContext): Promise<void> {
  if (value === "create") {
    showAgentCreatePicker(ctx);
    return;
  }
  if (value.startsWith("manage:")) {
    await showAgentManagePicker(value.slice(7), ctx);
  }
  // "cancel" → picker closes, no further action
}

/**
 * Handle text input when the agent wizard is awaiting user input.
 * Called by SlackHome's handleSend when agentWizardState.phase !== "idle".
 */
export async function handleAgentWizardInput(
  input: string,
  ctx: SlashCommandContext,
): Promise<void> {
  const state = agentWizardState;

  if (state.phase === "agent_create_describe") {
    const prompt = input.trim();
    if (!prompt) {
      ctx.addMessage({
        id: `aw-empty-${Date.now()}`,
        role: "system",
        content: "Please describe what you want the agent to do.",
        timestamp: Date.now(),
      });
      return;
    }
    agentWizardState = { phase: "idle" };
    ctx.setLoading(true, "Generating agent profile...");
    let config: Record<string, unknown>;
    try {
      const aiResult = await ctx.dispatch(
        `ask Generate an agent profile as JSON for: "${prompt}". Return ONLY JSON with: name (string), expertise (string[]), heartbeatCron (string), tools (string[]).`,
      );
      const jsonMatch = aiResult.output?.match(/\{[\s\S]*\}/);
      const parsed = jsonMatch ? (JSON.parse(jsonMatch[0]) as Record<string, unknown>) : null;
      if (parsed && typeof parsed.name === "string" && Array.isArray(parsed.expertise)) {
        config = {
          slug: slugify(parsed.name as string) || "custom-agent",
          name: parsed.name,
          expertise: parsed.expertise,
          personality: `You are an AI agent specialized in: ${prompt}`,
          heartbeatCron: (parsed.heartbeatCron as string) ?? "daily",
          tools: Array.isArray(parsed.tools) ? parsed.tools : ["nex_search", "nex_ask", "nex_remember"],
        };
      } else {
        config = buildAgentFromPrompt(prompt);
      }
    } catch {
      config = buildAgentFromPrompt(prompt);
    }
    ctx.setLoading(false);
    ctx.addMessage({
      id: `aw-preview-${Date.now()}`,
      role: "system",
      content:
        `Agent profile:\n\n` +
        `  Name:      ${config.name as string}\n` +
        `  Expertise: ${(config.expertise as string[]).join(", ")}\n` +
        `  Schedule:  ${formatSchedule(config.heartbeatCron as string)}`,
      timestamp: Date.now(),
    });
    ctx.showConfirm("Create this agent?", async (confirmed) => {
      if (!confirmed) { showAgentCreatePicker(ctx); return; }
      await createAgentFromConfig(config, ctx);
    });
    return;
  }

  if (state.phase === "agent_create_name") {
    const name = input.trim();
    if (!name) {
      ctx.addMessage({
        id: `aw-name-empty-${Date.now()}`,
        role: "system",
        content: "Please enter a name for the agent.",
        timestamp: Date.now(),
      });
      return;
    }
    agentWizardState = { phase: "agent_create_expertise", slug: slugify(name), name };
    ctx.addMessage({
      id: `aw-expertise-prompt-${Date.now()}`,
      role: "system",
      content: "Enter areas of expertise (comma-separated):\n(e.g., sales, outreach, lead-generation)",
      timestamp: Date.now(),
    });
    return;
  }

  if (state.phase === "agent_create_expertise") {
    const expertiseRaw = input.trim();
    if (!expertiseRaw) {
      ctx.addMessage({
        id: `aw-exp-empty-${Date.now()}`,
        role: "system",
        content: "Please enter at least one area of expertise.",
        timestamp: Date.now(),
      });
      return;
    }
    const expertise = expertiseRaw.split(",").map((s) => s.trim()).filter(Boolean);
    const { slug, name } = state;
    agentWizardState = { phase: "idle" };
    ctx.showPicker(
      "How often should this agent run?",
      SCHEDULE_OPTIONS,
      async (scheduleValue) => {
        ctx.addMessage({
          id: `aw-preview-manual-${Date.now()}`,
          role: "system",
          content:
            `Agent summary:\n\n` +
            `  Name:      ${name}\n` +
            `  Expertise: ${expertise.join(", ")}\n` +
            `  Schedule:  ${formatSchedule(scheduleValue)}`,
          timestamp: Date.now(),
        });
        ctx.showConfirm("Create this agent?", async (confirmed) => {
          if (!confirmed) { showAgentCreatePicker(ctx); return; }
          await createAgentFromConfig(
            {
              slug,
              name,
              expertise,
              heartbeatCron: scheduleValue,
            },
            ctx,
          );
        });
      },
    );
    return;
  }

  if (state.phase === "agent_edit_name") {
    const newName = input.trim();
    if (!newName) {
      ctx.addMessage({
        id: `aw-edit-name-empty-${Date.now()}`,
        role: "system",
        content: "Please enter a new name.",
        timestamp: Date.now(),
      });
      return;
    }
    agentWizardState = { phase: "idle" };
    const { getAgentService } = await import("./services/agent-service.js");
    getAgentService().updateConfig(state.slug, { name: newName });
    ctx.addMessage({
      id: `aw-edit-name-done-${Date.now()}`,
      role: "system",
      content: `Agent renamed to "${newName}".`,
      timestamp: Date.now(),
    });
    return;
  }

  if (state.phase === "agent_edit_expertise") {
    const expertiseRaw = input.trim();
    if (!expertiseRaw) {
      ctx.addMessage({
        id: `aw-edit-exp-empty-${Date.now()}`,
        role: "system",
        content: "Please enter at least one area of expertise.",
        timestamp: Date.now(),
      });
      return;
    }
    const expertise = expertiseRaw.split(",").map((s) => s.trim()).filter(Boolean);
    agentWizardState = { phase: "idle" };
    const { getAgentService } = await import("./services/agent-service.js");
    getAgentService().updateConfig(state.slug, { expertise });
    ctx.addMessage({
      id: `aw-edit-exp-done-${Date.now()}`,
      role: "system",
      content: `Expertise updated to: ${expertise.join(", ")}.`,
      timestamp: Date.now(),
    });
  }
}

// ── Built-in commands ───────────────────────────────────────────────

registerSlashCommand({
  name: "help",
  description: "Show all slash commands and keybindings",
  execute: async () => {
    const lines = listSlashCommands().map((cmd) => {
      const usage = cmd.usage ? `  ${cmd.usage}` : "";
      return `  /${cmd.name}${usage}  — ${cmd.description}`;
    });
    const header = "Available commands:\n";
    const footer = "\nKeybindings: Esc=back  Ctrl+C=quit";
    return { output: header + lines.join("\n") + footer };
  },
});

registerSlashCommand({
  name: "init",
  description: "Run setup flow to configure API key and install integrations",
  usage: "/init",
  execute: async (_args, ctx) => {
    // Reset any stale init state
    resetInitState();

    ctx.setLoading(true, "Checking configuration...");
    try {
      const { resolveApiKey, loadConfig } = await import("../lib/config.js");
      const apiKey = resolveApiKey();

      if (!apiKey) {
        // No key at all — start registration flow
        ctx.setLoading(false);
        ctx.addMessage({
          id: `init-welcome-${Date.now()}`,
          role: "system",
          content:
            "Welcome to WUPHF! Let's get you set up.\n\nWhat's your email address?",
          timestamp: Date.now(),
        });
        initState = { phase: "awaiting_email" };
        return { silent: true };
      }

      // Key exists — validate it with a lightweight API call
      ctx.setLoading(true, "Validating API key...");
      const testResult = await ctx.dispatch("object list");

      if (
        testResult.exitCode === 2 ||
        (testResult.error && testResult.error.includes("API key"))
      ) {
        // Key is expired/invalid — offer to regenerate via picker
        ctx.setLoading(false);
        const config = loadConfig();

        ctx.addMessage({
          id: `init-expired-${Date.now()}`,
          role: "system",
          content: `Your API key is expired or invalid.${config.email ? ` (${config.email})` : ""}`,
          timestamp: Date.now(),
        });

        const regenOption = config.email
          ? { label: `Generate new key for ${config.email}`, value: "regenerate" }
          : { label: "Generate new key (enter email)", value: "regenerate" };

        ctx.showPicker(
          "What would you like to do?",
          [
            regenOption,
            { label: "Use a different email", value: "new-email" },
            { label: "Keep current key and skip", value: "skip" },
          ],
          (choice) => handleRegenChoice(choice, config.email, apiKey, ctx),
        );
        return { silent: true };
      }

      // Key is valid — proceed directly to platform installation
      ctx.setLoading(false);
      ctx.addMessage({
        id: `init-valid-${Date.now()}`,
        role: "system",
        content: "API key is valid. Running platform installation...",
        timestamp: Date.now(),
      });
      await runPlatformInstall(apiKey, ctx);
      return { silent: true };
    } catch (err) {
      ctx.setLoading(false);
      return {
        output: `Setup error: ${err instanceof Error ? err.message : String(err)}`,
      };
    }
  },
});

// ── /login — auth only, no platform install ───────────────────────

registerSlashCommand({
  name: "login",
  description: "Log in or sign up (register only, no platform install)",
  usage: "/login [email]",
  execute: async (args, ctx) => {
    resetInitState();

    const email = args.trim();

    // If email provided inline, register immediately
    if (email && email.includes("@")) {
      ctx.setLoading(true, "Registering...");
      try {
        const { registerUser } = await import("../commands/init.js");
        const result = await registerUser(email);
        ctx.setLoading(false);

        const maskedKey = `${result.apiKey.slice(0, 6)}...${result.apiKey.slice(-4)}`;
        return {
          output:
            `Logged in!\n\n` +
            `  API key:   ${maskedKey}\n` +
            `  Workspace: ${result.workspaceSlug}\n` +
            `  Email:     ${email}\n\n` +
            `Saved to ~/.wuphf/config.json\n` +
            `Run /init to install WUPHF into your AI agents.`,
        };
      } catch (err) {
        ctx.setLoading(false);
        return {
          output: `Login failed: ${err instanceof Error ? err.message : String(err)}`,
        };
      }
    }

    // Check if already logged in
    const { resolveApiKey, loadConfig } = await import("../lib/config.js");
    const apiKey = resolveApiKey();

    if (apiKey) {
      // Validate the key
      ctx.setLoading(true, "Checking session...");
      const testResult = await ctx.dispatch("object list");
      ctx.setLoading(false);

      if (testResult.exitCode !== 2) {
        const config = loadConfig();
        const maskedKey = `${apiKey.slice(0, 6)}...${apiKey.slice(-4)}`;
        return {
          output:
            `Already logged in.\n\n` +
            `  API key:   ${maskedKey}\n` +
            `  Email:     ${config.email || "unknown"}\n` +
            `  Workspace: ${config.workspace_slug || "unknown"}\n\n` +
            `To switch accounts: /login new@email.com`,
        };
      }

      // Key expired — prompt for email
      const config = loadConfig();
      ctx.addMessage({
        id: `login-expired-${Date.now()}`,
        role: "system",
        content:
          `Your session has expired.${config.email ? ` (${config.email})` : ""}\n\n` +
          `Enter your email to log in again:`,
        timestamp: Date.now(),
      });
      initState = { phase: "awaiting_email", loginOnly: true };
      return { silent: true };
    }

    // No key — ask for email
    ctx.addMessage({
      id: `login-prompt-${Date.now()}`,
      role: "system",
      content: "Enter your email to log in or sign up:",
      timestamp: Date.now(),
    });
    initState = { phase: "awaiting_email", loginOnly: true };
    return { silent: true };
  },
});

registerSlashCommand({
  name: "ask",
  description: "Query the context graph",
  usage: "/ask <query>",
  execute: async (args, ctx) => {
    if (!args.trim()) {
      return { output: "Usage: /ask <question>" };
    }
    ctx.setLoading(true, "thinking...");
    try {
      const result = await ctx.dispatch(`ask ${args}`);
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "(no response)" };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "search",
  description: "Fuzzy search across records",
  usage: "/search <query>",
  execute: async (args, ctx) => {
    if (!args.trim()) {
      return { output: "Usage: /search <query>" };
    }
    ctx.setLoading(true, "searching...");
    try {
      const result = await ctx.dispatch(`search ${args}`);
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "(no results)" };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "agents",
  description: "Manage agents — create, start, stop, edit",
  execute: async (_args, ctx) => {
    await openAgentsManager(ctx);
    return { silent: true };
  },
});

registerSlashCommand({
  name: "agent",
  description: "Manage agents",
  usage: "/agent <create|start|stop> <slug> [--template <name>]",
  execute: async (args, ctx) => {
    if (!args.trim()) {
      return { output: "Usage: /agent <create|start|stop> <slug> [options]" };
    }
    ctx.setLoading(true, "running agent command...");
    try {
      const result = await ctx.dispatch(`agent ${args}`);
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "(done)" };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "chat",
  description: "Open chat view",
  execute: async (_args, ctx) => {
    ctx.push({ name: "chat" });
    return { output: "Switched to chat view.", silent: true };
  },
});

registerSlashCommand({
  name: "calendar",
  description: "Open full calendar view",
  execute: async (_args, ctx) => {
    ctx.push({ name: "calendar" });
    return { output: "Switched to calendar view.", silent: true };
  },
});

registerSlashCommand({
  name: "cal",
  description: "Toggle calendar strip on home screen",
  execute: async () => {
    const toggleFn = (globalThis as Record<string, unknown>).__nexHomeCalendarToggle as
      | (() => void)
      | undefined;
    if (toggleFn) {
      toggleFn();
      return { output: "Calendar toggled.", silent: true };
    }
    return { output: "Calendar toggle only available on home screen." };
  },
});

registerSlashCommand({
  name: "orchestration",
  description: "Open orchestration view",
  execute: async (_args, ctx) => {
    ctx.push({ name: "orchestration" });
    return { output: "Switched to orchestration view.", silent: true };
  },
});

registerSlashCommand({
  name: "orch",
  description: "Open orchestration view (alias)",
  execute: async (_args, ctx) => {
    ctx.push({ name: "orchestration" });
    return { output: "Switched to orchestration view.", silent: true };
  },
});

registerSlashCommand({
  name: "insights",
  description: "Open insights dashboard",
  usage: "/insights [--last <dur>]",
  execute: async (args, ctx) => {
    ctx.setLoading(true, "fetching insights...");
    try {
      const cmdArgs = args ? `insight list ${args}` : "insight list";
      const result = await ctx.dispatch(cmdArgs);
      if (result.error) return { output: `Error: ${result.error}` };

      // Parse API response into view props
      const raw = result.data as { insights?: Array<Record<string, unknown>> } | undefined;
      const insights = (raw?.insights ?? []).map((item: Record<string, unknown>) => ({
        id: String(item.id ?? ""),
        title: String(item.title ?? item.content ?? ""),
        body: String(item.body ?? item.summary ?? ""),
        priority: normalizePriority(String(item.priority ?? "medium")),
        category: String(item.category ?? item.type ?? "general"),
        recordIds: Array.isArray(item.record_ids) ? item.record_ids.map(String) : [],
        timestamp: String(item.created_at ?? item.timestamp ?? new Date().toISOString()),
      }));

      ctx.push({ name: "insights", props: { insights } });
      return { output: "Switched to insights view.", silent: true };
    } finally {
      ctx.setLoading(false);
    }
  },
});

function normalizePriority(p: string): "critical" | "high" | "medium" | "low" {
  const lower = p.toLowerCase();
  if (lower === "critical" || lower === "crit") return "critical";
  if (lower === "high") return "high";
  if (lower === "low") return "low";
  return "medium";
}

registerSlashCommand({
  name: "graph",
  description: "Open workspace graph in browser",
  execute: async (_args, ctx) => {
    ctx.setLoading(true, "opening graph...");
    try {
      const result = await ctx.dispatch("graph");
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "Graph opened." };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "objects",
  description: "List object types",
  execute: async (_args, ctx) => {
    ctx.setLoading(true, "fetching objects...");
    try {
      const result = await ctx.dispatch("object list");
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "(no objects)" };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "records",
  description: "List records for an object",
  usage: "/records <object-slug>",
  execute: async (args, ctx) => {
    if (!args.trim()) {
      return { output: "Usage: /records <object-slug>" };
    }
    ctx.setLoading(true, "fetching records...");
    try {
      const result = await ctx.dispatch(`record list ${args}`);
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "(no records)" };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "remember",
  description: "Store content in context graph",
  usage: "/remember <content>",
  execute: async (args, ctx) => {
    if (!args.trim()) {
      return { output: "Usage: /remember <content>" };
    }
    ctx.setLoading(true, "storing...");
    try {
      const result = await ctx.dispatch(`remember ${args}`);
      if (result.error) return { output: `Error: ${result.error}` };
      return { output: result.output || "Stored." };
    } finally {
      ctx.setLoading(false);
    }
  },
});

registerSlashCommand({
  name: "clear",
  description: "Clear conversation history",
  execute: async () => {
    // Handled specially by the conversation view — it checks for /clear
    // and resets messages. We return a sentinel here.
    return { output: "__CLEAR__", silent: true };
  },
});

registerSlashCommand({
  name: "quit",
  description: "Exit WUPHF",
  execute: async () => {
    process.exit(0);
    return { output: "" };
  },
});

registerSlashCommand({
  name: "q",
  description: "Exit WUPHF (alias)",
  execute: async () => {
    process.exit(0);
    return { output: "" };
  },
});

registerSlashCommand({
  name: "provider",
  description: "Switch LLM provider (Gemini / Claude Code)",
  execute: async (_args, ctx) => {
    const { loadConfig } = await import("../lib/config.js");
    const config = loadConfig();
    const current = (config as Record<string, unknown>).llm_provider ?? "gemini";

    ctx.addMessage({
      id: `provider-current-${Date.now()}`,
      role: "system",
      content: `Current provider: ${current}`,
      timestamp: Date.now(),
    });

    ctx.showPicker(
      "Switch LLM provider",
      [
        { label: "Gemini via WUPHF", value: "gemini", description: current === "gemini" ? "(current)" : "Google Gemini powers your agents" },
        { label: "Claude Code", value: "claude-code", description: current === "claude-code" ? "(current)" : "Use Claude Code with WUPHF as a plugin" },
      ],
      async (choice) => {
        if (choice === current) {
          ctx.addMessage({
            id: `provider-same-${Date.now()}`,
            role: "system",
            content: `Already using ${choice}.`,
            timestamp: Date.now(),
          });
          return;
        }
        await handleProviderChoice(choice, ctx);
      },
    );
    return { silent: true };
  },
});

import type {
  BlueprintAgent,
  BlueprintCategoryKey,
  BlueprintDisplay,
  MemoryBackend,
  RuntimeSpec,
  WizardStep,
} from "./types";

// Step order: company info before blueprint. The blueprint picker is a
// decision about how the office starts; it makes more sense after the
// user has anchored who they are than as the very first question.
// `ready` is the final-step readiness summary matching the TUI's InitDone
// phase (see internal/tui/init_flow.go readinessChecks()) — shows the user
// exactly what's configured before we submit.
export const STEP_ORDER: readonly WizardStep[] = [
  "welcome",
  "identity",
  "templates",
  "team",
  "setup",
  "task",
  "ready",
] as const;

export const RUNTIMES: readonly RuntimeSpec[] = [
  {
    label: "Claude Code",
    binary: "claude",
    installUrl: "https://claude.ai/code",
    provider: "claude-code",
  },
  {
    label: "Codex",
    binary: "codex",
    installUrl: "https://github.com/openai/codex",
    provider: "codex",
  },
  {
    label: "Opencode",
    binary: "opencode",
    installUrl: "https://opencode.ai",
    provider: "opencode",
  },
  {
    label: "Cursor",
    binary: "cursor",
    installUrl: "https://cursor.com/",
    provider: null,
  },
  {
    label: "Windsurf",
    binary: "windsurf",
    installUrl: "https://codeium.com/windsurf",
    provider: null,
  },
] as const;

// "Start from scratch" starter roster. Mirrors scratchFoundingTeamBlueprint
// in internal/team/broker_onboarding.go — the broker seeds these exact slugs
// when the wizard POSTs blueprint:null. Kept in sync manually; backend is the
// source of truth, this is just the Team-step preview so users don't see an
// empty roster before confirming.
export const SCRATCH_FOUNDING_TEAM: readonly BlueprintAgent[] = [
  { slug: "ceo", name: "CEO", role: "lead", checked: true, built_in: true },
  { slug: "gtm-lead", name: "GTM Lead", role: "go-to-market", checked: true },
  {
    slug: "founding-engineer",
    name: "Founding Engineer",
    role: "engineering",
    checked: true,
  },
  { slug: "pm", name: "Product Manager", role: "product", checked: true },
  { slug: "designer", name: "Designer", role: "design", checked: true },
];

// Display overrides for blueprints. Backend names/descriptions are long-form
// ("Bookkeeping and Invoicing Service", "Template for a bookkeeping operation
// that handles recurring books..."). For the onboarding picker we want short,
// scannable copy and visible categorization. Overrides are keyed by blueprint
// id (see templates/operations/*/blueprint.yaml). If a blueprint isn't in the
// map we fall back to the backend name + description, so new blueprints still
// render without frontend changes.
export const BLUEPRINT_CATEGORIES: ReadonlyArray<{
  key: BlueprintCategoryKey;
  label: string;
  hint: string;
}> = [
  {
    key: "services",
    label: "Services",
    hint: "Client work, done by your office",
  },
  {
    key: "media",
    label: "Media & Community",
    hint: "Content or community as the business",
  },
  { key: "product", label: "Products", hint: "Software you build and sell" },
] as const;

export const BLUEPRINT_DISPLAY: Record<string, BlueprintDisplay> = {
  "bookkeeping-invoicing-service": {
    category: "services",
    shortDescription: "Books · invoices · monthly close",
    icon: "📊",
  },
  "local-business-ai-package": {
    category: "services",
    shortDescription: "Intake · booking · follow-up",
    icon: "🏪",
  },
  "multi-agent-workflow-consulting": {
    category: "services",
    shortDescription: "Client engagements · workflow delivery",
    icon: "💼",
  },
  "niche-crm": {
    category: "product",
    shortDescription: "Build & launch a focused CRM",
    icon: "🎯",
  },
  "paid-discord-community": {
    category: "media",
    shortDescription: "Moderation · onboarding · engagement",
    icon: "💬",
  },
  "youtube-factory": {
    category: "media",
    shortDescription: "Script · film · publish · analyze",
    icon: "📹",
  },
};

// API_KEY_FIELDS: each provider's auth has two valid paths — log in via
// the provider's CLI (claude login, codex login, etc.) or paste an API
// key here. The wizard defaults to CLI login because it's the existing
// primary path for most users; clicking "Use API key" reveals the
// password input so users can paste a key without it being on screen
// the whole time.
export const API_KEY_FIELDS = [
  {
    key: "ANTHROPIC_API_KEY",
    label: "Anthropic",
    hint: "Powers Claude-based agents",
    cliLoginCmd: "claude login",
  },
  {
    key: "OPENAI_API_KEY",
    label: "OpenAI",
    hint: "Powers GPT-based agents",
    cliLoginCmd: "codex login",
  },
  {
    key: "GOOGLE_API_KEY",
    label: "Google",
    hint: "Powers Gemini-based agents",
    cliLoginCmd: "gcloud auth application-default login",
  },
] as const;

// LOCAL_PROVIDER_LABELS is shared between LocalLLMPicker.tsx (the picker
// itself), the SetupStep tile that summarizes the user's pick, and the
// ReadyStep readiness summary. Keeping it here avoids three-way drift if
// a runtime is renamed or added.
export const LOCAL_PROVIDER_LABELS: ReadonlyArray<{
  kind: string;
  label: string;
  blurb: string;
}> = [
  { kind: "mlx-lm", label: "MLX-LM", blurb: "macOS · Apple Silicon" },
  { kind: "ollama", label: "Ollama", blurb: "macOS / Linux" },
  { kind: "exo", label: "Exo", blurb: "Multi-device pool" },
];

export const MEMORY_BACKEND_OPTIONS: ReadonlyArray<{
  value: MemoryBackend;
  label: string;
  hint: string;
}> = [
  {
    value: "markdown",
    label: "Team wiki (default)",
    hint: 'A living knowledge graph for your team. Agents record typed facts as git commits, the LLM rewrites briefs under the "archivist" identity, and every claim has a citation. `/lookup` answers questions with sources. `/lint` flags contradictions, orphans, and stale facts. File-over-app, `git clone`-able, no API key needed.',
  },
  {
    value: "nex",
    label: "Nex",
    hint: "Hosted memory graph that streams in HubSpot, Slack, Gmail, Calendar, and more — your tools become entities agents already know. Free tier available; needs NEX_API_KEY.",
  },
  {
    value: "gbrain",
    label: "GBrain",
    hint: "Local graph over Postgres. Needs an LLM key for embeddings.",
  },
  {
    value: "none",
    label: "None",
    hint: "Skip shared memory. Agents work with only per-turn context.",
  },
] as const;

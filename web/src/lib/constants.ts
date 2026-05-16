// Wiki unifies three surfaces behind one sidebar entry: the canonical
// team wiki, per-agent notebooks (drafts), and the promotion review queue.
// Each surface gets its own tab inside the Wiki app; notebooks/reviews
// have no top-level sidebar entries of their own.
// Inbox is hoisted out into a dedicated prominent button at the top of
// the sidebar (see components/sidebar/InboxButton). It used to live in
// this list as just another app; promoting it reflects that the inbox
// is the primary attention surface, not a secondary tool.
export const SIDEBAR_APPS = [
  { id: "overview", icon: "\uD83C\uDFE0", name: "Overview" },
  { id: "wiki", icon: "\uD83D\uDCD6", name: "Wiki" },
  { id: "console", icon: ">", name: "Console" },
  { id: "tasks", icon: "\u2705", name: "Tasks" },
  { id: "requests", icon: "\uD83D\uDCCB", name: "Requests" },
  { id: "graph", icon: "\uD83D\uDD78", name: "Graph" },
  { id: "policies", icon: "\uD83D\uDEE1", name: "Policies" },
  { id: "calendar", icon: "\uD83D\uDCC5", name: "Calendar" },
  { id: "skills", icon: "\u26A1", name: "Skills" },
  { id: "activity", icon: "\uD83D\uDCE6", name: "Activity" },
  { id: "receipts", icon: "\uD83E\uDDFE", name: "Receipts" },
  { id: "health-check", icon: "\uD83D\uDCF6", name: "Access & Health" },
  { id: "settings", icon: "\u2699", name: "Settings" },
] as const;

export function appTitle(app: string): string {
  return (
    SIDEBAR_APPS.find((item) => item.id === app)?.name ??
    app.replace(/-/g, " ").replace(/\b\w/g, (char) => char.toUpperCase())
  );
}

export const ONBOARDING_COPY = {
  step1_headline: "AI employees with a shared brain",
  step1_subhead:
    "A collaborative office where AI agents like Claude Code, Codex, and OpenClaw learn your work playbooks, build personalized skills, and execute, 24x7.\nEach agent is backed by its own knowledge graph.",
  step1_cta: "Continue",
  step2_headline: "Name your office",
  step2_subhead: "This becomes the workspace your agents call home.",
  step2_cta: "Continue",
  step3_headline: "Pick a blueprint",
  step3_subhead: "Pre-built teams and workflows. Start here, customize later.",
  step3_cta: "Continue",
  step4_headline: "Meet your team",
  step4_subhead:
    "These specialists ship work while you sleep. Toggle anyone you don't need.",
  step4_cta: "Continue",
  step5_headline: "Connect a provider",
  step5_subhead:
    "Pick one or more providers your agents can use. Drag to set fallback order.",
  step5_cta: "Continue",
  step6_headline: "Power up with Nex",
  step6_subhead:
    "Shared memory, entity briefs, and integrations. Optional but powerful.",
  step7_headline: "First assignment",
  step7_subhead: "Give your team something real to work on.",
  step7_placeholder:
    "e.g. Sign our first three pilot customers in the next two weeks.",
  step7_skip: "Skip for now",
  step7_cta: "Review setup",
  step8_headline: "Ready to launch",
  step8_subhead: "Here's what's configured. Fix anything later from Settings.",
  step8_cta: "Launch office",
} as const;

export const DISCONNECT_THRESHOLD = 3;
export const MESSAGE_POLL_INTERVAL = 2000;
export const MEMBER_POLL_INTERVAL = 5000;
export const REQUEST_POLL_INTERVAL = 3000;

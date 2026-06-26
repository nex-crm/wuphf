// Sidebar TOOLS entries (labels, ids, order) live in
// `routes/routeRegistry.ts`. This file historically re-exported a
// duplicate `SIDEBAR_APPS` list — that was deleted to keep the registry
// the single source of truth. Resolve labels through `APP_LABELS` /
// `SIDEBAR_TOOLS` and read the displayed order from `SIDEBAR_TOOLS`.
import {
  APP_LABELS,
  type AppPanelId,
  type FirstClassAppId,
  isAppPanelId,
  isFirstClassAppId,
} from "../routes/routeRegistry";

export function appTitle(app: string): string {
  if (isAppPanelId(app) || isFirstClassAppId(app)) {
    return APP_LABELS[app as AppPanelId | FirstClassAppId];
  }
  return app.replace(/-/g, " ").replace(/\b\w/g, (char) => char.toUpperCase());
}

export const ONBOARDING_COPY = {
  step1_headline: "AI employees with a shared brain",
  step1_subhead:
    "A collaborative office where AI agents like Claude Code, Codex, and Opencode learn your work playbooks, build personalized skills, and execute, 24x7.\nEach agent is backed by its own knowledge graph.",
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

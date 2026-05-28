import type { ConfigSnapshot, LocalProviderStatus } from "../../../api/client";

// IntegrationCategory groups cards by the role they play in the team. The
// app renders each category as a labelled section. Add a new category only
// when an integration genuinely doesn't fit any of the existing ones — most
// new integrations should land in an existing bucket so users always know
// where to look.
//
//   - external-agents: Gateways that import existing agents into the team
//     (OpenClaw, Hermes). The agent's runtime is gateway-managed; WUPHF
//     speaks to the gateway's transport rather than dispatching directly.
//
//   - channels: Inbound messaging streams that become channels in the
//     office (Telegram today; Slack / Discord / WhatsApp can plug in
//     under this category later).
export type IntegrationCategory = "external-agents" | "channels";

export interface IntegrationCategoryMeta {
  id: IntegrationCategory;
  title: string;
  description: string;
}

export const INTEGRATION_CATEGORIES: readonly IntegrationCategoryMeta[] = [
  {
    id: "external-agents",
    title: "External Agents",
    description:
      "Gateways that import agents from another system into the team. The imported agent's runtime is managed by the gateway, not WUPHF.",
  },
  {
    id: "channels",
    title: "Channels",
    description:
      "Inbound messaging streams that surface as channels in the office.",
  },
] as const;

// Per-integration runtime context. The registry hands each render() call
// the current /config snapshot and any local-runtime probes so cards don't
// re-implement data fetching. Add new fields here when a category needs
// fresh inputs (e.g. an OAuth-status query) rather than letting individual
// cards call hooks ad-hoc.
export interface IntegrationContext {
  cfg: ConfigSnapshot;
  localStatuses: LocalProviderStatus[];
}

// IntegrationDescriptor is the registry entry shape. Keep it data-shaped
// (no hooks, no React state) — the only React-y field is the render
// function. This makes the registry safe to enumerate, snapshot, and test
// without mounting the app.
export interface IntegrationDescriptor {
  id: string;
  category: IntegrationCategory;
  title: string;
  // isAvailable reports whether the build/Go layer supports this
  // integration at all. False entries are filtered out before any cards
  // render so we don't promise a Connect button the backend can't
  // honor. Use it for compile-time gates (`gateway_kinds` membership for
  // gateways, future build-tag checks for compose-only integrations).
  isAvailable: (ctx: IntegrationContext) => boolean;
  // render returns the card body. The IntegrationsApp wraps it in the
  // shared CardShell so card files only worry about their own state.
  render: (ctx: IntegrationContext) => React.ReactNode;
}

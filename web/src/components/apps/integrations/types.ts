import type { ReactElement, ReactNode } from "react";

import type {
  ConfigSnapshot,
  LocalProviderStatus,
} from "../../../api/client";

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

// IntegrationStatus is the connectivity verdict the list view + detail
// header both render. tone drives the LED + uppercase status pill; label
// is the short uppercase string ("CONNECTED", "NOT CONFIGURED").
export type IntegrationStatusTone =
  | "connected"
  | "available"
  | "unconfigured"
  | "warning";

export interface IntegrationStatus {
  tone: IntegrationStatusTone;
  label: string;
}

// IntegrationDescriptor is the registry entry shape. Keep it data-shaped
// (no hooks, no React state) — the only React-y fields are the logo
// component and the render function. This makes the registry safe to
// enumerate, snapshot, and test without mounting the app.
//
// The split between list and detail rendering:
//   - List view (default) renders {logo, title, summary, status}.
//   - Detail view renders {logo, title, status, render(ctx)}. The render
//     fn returns only the form body — back button + header chrome live
//     in the detail layout, not in each card.
export interface IntegrationDescriptor {
  id: string;
  category: IntegrationCategory;
  title: string;
  // summary is the one-liner shown under the title in the list view. Aim
  // for ≤ 80 chars so a row never wraps to two lines.
  summary: string;
  // logo returns the branded glyph rendered at 28px in both the list row
  // and the detail header. Use IntegrationLogos.tsx exports when possible.
  logo: () => ReactElement;
  // isAvailable reports whether the build/Go layer supports this
  // integration at all. False entries are filtered out before any rows
  // render so we don't promise a Connect button the backend can't honor.
  isAvailable: (ctx: IntegrationContext) => boolean;
  // status returns the connectivity verdict from the current /config
  // snapshot + probes. Used by both list and detail. Must be pure — no
  // hooks, no side effects.
  status: (ctx: IntegrationContext) => IntegrationStatus;
  // render returns the detail-view body (form fields + action buttons).
  // The IntegrationsApp wraps it in the back-button chrome.
  render: (ctx: IntegrationContext) => ReactNode;
}

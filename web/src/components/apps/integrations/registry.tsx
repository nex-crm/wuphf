import { HermesCard } from "./HermesCard";
import { OpenClawCard } from "./OpenClawCard";
import { TelegramCard } from "./TelegramCard";
import type { IntegrationContext, IntegrationDescriptor } from "./types";

// INTEGRATIONS is the single source of truth for what the Integrations app
// renders. Adding a new integration is a four-line change here plus a new
// card file — no IntegrationsApp edits needed. The descriptor's category
// places the card in the correct section; isAvailable hides the card on
// builds where the backend can't actually service it.
//
// Ordering note: descriptors render in array order within a category. Put
// the most common / well-trodden integration first; rare or
// experimentally-supported ones after.
export const INTEGRATIONS: readonly IntegrationDescriptor[] = [
  // ── External Agents ──────────────────────────────────────────────
  {
    id: "openclaw",
    category: "external-agents",
    title: "OpenClaw",
    isAvailable: ({ cfg }: IntegrationContext) => {
      const kinds = cfg.gateway_kinds ?? ["openclaw"];
      return kinds.includes("openclaw") || kinds.includes("openclaw-http");
    },
    render: ({ cfg }) => <OpenClawCard cfg={cfg} />,
  },
  {
    id: "hermes",
    category: "external-agents",
    title: "Hermes",
    isAvailable: ({ cfg }: IntegrationContext) => {
      const kinds = cfg.gateway_kinds ?? ["hermes-agent"];
      return kinds.includes("hermes-agent");
    },
    render: ({ localStatuses }) => <HermesCard statuses={localStatuses} />,
  },

  // ── Channels ─────────────────────────────────────────────────────
  {
    id: "telegram",
    category: "channels",
    title: "Telegram",
    // Telegram is always available — the modal handles the
    // server-not-reachable case at submit time, and there's no compile
    // flag that strips Telegram support today.
    isAvailable: () => true,
    render: ({ cfg }) => <TelegramCard cfg={cfg} />,
  },
] as const;

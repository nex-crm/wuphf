import { HermesDetail, hermesStatus } from "./HermesCard";
import {
  HermesLogo,
  OpenClawLogo,
  SlackLogo,
  TelegramLogo,
} from "./IntegrationLogos";
import { OpenClawDetail, openClawStatus } from "./OpenClawCard";
import { SlackDetail, slackStatus } from "./SlackCard";
import { TelegramDetail, telegramStatus } from "./TelegramCard";
import type { IntegrationContext, IntegrationDescriptor } from "./types";

// INTEGRATIONS is the single source of truth for what the Integrations app
// renders. Adding a new integration is a single descriptor entry plus
// optional logo + detail-component additions. The IntegrationsApp itself
// has no per-integration knowledge.
//
// Ordering: descriptors render in array order within a category. Put the
// most common / well-trodden integration first; rare or experimentally-
// supported ones after.
export const INTEGRATIONS: readonly IntegrationDescriptor[] = [
  // ── External Agents ──────────────────────────────────────────────
  {
    id: "openclaw",
    category: "external-agents",
    title: "OpenClaw",
    summary:
      "Bridge OpenClaw-controlled agent sessions into the office over a WebSocket gateway.",
    logo: OpenClawLogo,
    isAvailable: ({ cfg }: IntegrationContext) => {
      const kinds = cfg.gateway_kinds ?? ["openclaw"];
      return kinds.includes("openclaw") || kinds.includes("openclaw-http");
    },
    status: ({ cfg }) => openClawStatus(cfg),
    render: ({ cfg }) => <OpenClawDetail cfg={cfg} />,
  },
  {
    id: "hermes",
    category: "external-agents",
    title: "Hermes",
    summary:
      "Route imported Hermes agents through a local Hermes gateway's OpenAI-compatible server.",
    logo: HermesLogo,
    isAvailable: ({ cfg }: IntegrationContext) => {
      const kinds = cfg.gateway_kinds ?? ["hermes-agent"];
      return kinds.includes("hermes-agent");
    },
    status: ({ localStatuses }) => hermesStatus(localStatuses),
    render: ({ localStatuses }) => <HermesDetail statuses={localStatuses} />,
  },

  // ── Channels ─────────────────────────────────────────────────────
  {
    id: "slack",
    category: "channels",
    title: "Slack",
    summary:
      "Bring WUPHF agents into Slack and make your other AI agents work together.",
    logo: SlackLogo,
    // Slack is always offered — the wizard validates tokens live and the
    // backend reports reachability; there's no compile flag that strips it.
    isAvailable: () => true,
    status: ({ cfg }) => slackStatus(cfg),
    render: ({ cfg }) => <SlackDetail cfg={cfg} />,
  },
  {
    id: "telegram",
    category: "channels",
    title: "Telegram",
    summary:
      "Bring a Telegram chat into the office as a channel; replies route through a bot you control.",
    logo: TelegramLogo,
    // Telegram is always available — the modal handles the
    // server-not-reachable case at submit time, and there's no compile
    // flag that strips Telegram support today.
    isAvailable: () => true,
    status: ({ cfg }) => telegramStatus(cfg),
    render: ({ cfg }) => <TelegramDetail cfg={cfg} />,
  },
] as const;

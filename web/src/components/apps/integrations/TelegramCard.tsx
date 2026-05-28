import type { ConfigSnapshot } from "../../../api/client";
import { useAppStore } from "../../../stores/app";
import type { IntegrationStatus } from "./types";

// TelegramCard wraps the existing /connect telegram modal so the
// Integrations app is the single discoverable entry point. The slash
// command continues to work — they call the same openConnectWizard
// store action, so users have parallel UI + command-line paths.

export function telegramStatus(cfg: ConfigSnapshot): IntegrationStatus {
  return cfg.telegram_token_set
    ? { tone: "connected", label: "Bot connected" }
    : { tone: "unconfigured", label: "Not configured" };
}

export function TelegramDetail({ cfg }: { cfg: ConfigSnapshot }) {
  const openConnectWizard = useAppStore((s) => s.openConnectWizard);
  const tokenSet = Boolean(cfg.telegram_token_set);
  return (
    <div>
      <p className="op-card-blurb">
        Bring a Telegram chat into the office as a channel. Paste a bot token,
        pick the chat, and replies route through the bot.
      </p>
      <div className="op-card-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => openConnectWizard("telegram")}
        >
          {tokenSet ? "Connect another chat" : "Connect Telegram"}
        </button>
      </div>
    </div>
  );
}

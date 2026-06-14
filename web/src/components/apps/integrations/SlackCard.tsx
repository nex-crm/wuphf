import type { ConfigSnapshot } from "../../../api/client";
import { useAppStore } from "../../../stores/app";
import type { IntegrationStatus } from "./types";

// SlackCard wraps the guided Slack onboarding wizard so the Integrations app is
// the discoverable entry point — the same role TelegramCard plays. The wizard is
// also reachable from the `/connect` provider picker; both paths open the same
// `openSlackConnect` store action. "Connected" requires BOTH tokens: the bot
// token (xoxb-, Web API) and the app-level token (xapp-, Socket Mode), since the
// transport needs both to come up.

export function slackStatus(cfg: ConfigSnapshot): IntegrationStatus {
  const botSet = Boolean(cfg.slack_bot_token_set);
  const appSet = Boolean(cfg.slack_app_token_set);
  if (botSet && appSet) {
    return { tone: "connected", label: "Bot connected" };
  }
  if (botSet || appSet) {
    return { tone: "unconfigured", label: "Finish setup" };
  }
  return { tone: "unconfigured", label: "Not configured" };
}

export function SlackDetail({ cfg }: { cfg: ConfigSnapshot }) {
  const openSlackConnect = useAppStore((s) => s.openSlackConnect);
  const connected =
    Boolean(cfg.slack_bot_token_set) && Boolean(cfg.slack_app_token_set);
  return (
    <div>
      <p className="op-card-blurb">
        Bring your office into Slack as channels. A guided wizard walks you
        through creating the app, pasting two tokens, and picking a channel —
        then your team is live in Slack, with every task in its own thread.
      </p>
      <div className="op-card-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => openSlackConnect()}
        >
          {connected ? "Connect another channel" : "Connect Slack"}
        </button>
      </div>
    </div>
  );
}

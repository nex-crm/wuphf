import type { ConfigSnapshot } from "../../../api/client";
import { useAppStore } from "../../../stores/app";
import type { IntegrationStatus } from "./types";

// SlackCard wraps the guided Slack onboarding wizard so the Integrations app is
// the discoverable entry point — the same role TelegramCard plays. The wizard is
// also reachable from the `/connect` provider picker; both paths open the same
// `openSlackConnect` store action.
//
// Status reflects how far setup got: "Connected" needs both tokens (xoxb- Web
// API + xapp- Socket Mode) AND at least one bridged channel. Tokens-without-a-
// channel is a half-finished setup ("Finish setup"), and the button + label key
// off the CHANNEL, not the tokens — so it never says "Connect another channel"
// before a single channel is connected.

export function slackStatus(cfg: ConfigSnapshot): IntegrationStatus {
  const botSet = Boolean(cfg.slack_bot_token_set);
  const appSet = Boolean(cfg.slack_app_token_set);
  const channelConnected = Boolean(cfg.slack_channel_connected);
  if (botSet && appSet && channelConnected) {
    return { tone: "connected", label: "Connected" };
  }
  if (botSet || appSet) {
    return { tone: "unconfigured", label: "Finish setup" };
  }
  return { tone: "unconfigured", label: "Not configured" };
}

export function SlackDetail({ cfg }: { cfg: ConfigSnapshot }) {
  const openSlackConnect = useAppStore((s) => s.openSlackConnect);
  const channelConnected = Boolean(cfg.slack_channel_connected);
  return (
    <div>
      <p className="op-card-blurb">
        Bring WUPHF's agents into Slack and make your other AI agents — vendor
        bots, your team's own — work together in a channel. WUPHF's CEO pulls
        them into the work and coordinates them, with every task in its own
        thread. A guided wizard handles setup.
      </p>
      <div className="op-card-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => openSlackConnect()}
        >
          {channelConnected ? "Connect another channel" : "Connect Slack"}
        </button>
      </div>
    </div>
  );
}

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { OpenNewWindow } from "iconoir-react";

import { updateConfig } from "../../../api/client";
import { showNotice } from "../../ui/Toast";
import { GitHubLogo, GmailLogo, SlackLogo } from "./IntegrationLogos";

// ComposioOnboarding is the first-run state of the Integrations page when no
// Composio API key is connected. Composio powers the whole integration catalog
// (OAuth, action execution, audit), so without a key there is nothing to browse
// — this turns that dead end into a one-field setup: paste your key, connect,
// and the catalog fills in. The broker resolves the Composio user identity from
// the workspace email, so the API key is the only thing we need here.

// API keys live in the project settings of the new Composio dashboard; the
// root URL routes signed-in users to their active project.
const COMPOSIO_KEYS_URL = "https://dashboard.composio.dev";

interface ComposioOnboardingProps {
  /** Called after the key is saved so the page can re-fetch config + catalog. */
  onConnected: () => void;
}

export function ComposioOnboarding({ onConnected }: ComposioOnboardingProps) {
  const queryClient = useQueryClient();
  const [apiKey, setApiKey] = useState("");
  const [reveal, setReveal] = useState(false);

  const connectMutation = useMutation({
    mutationFn: (key: string) => updateConfig({ composio_api_key: key }),
    onSuccess: async () => {
      setApiKey("");
      showNotice("Composio connected. Loading your integrations…", "success");
      await queryClient.invalidateQueries({ queryKey: ["config"] });
      await queryClient.invalidateQueries({ queryKey: ["integrations"] });
      onConnected();
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not save the Composio key",
        "error",
      );
    },
  });

  const trimmed = apiKey.trim();
  const canSubmit = trimmed.length > 0 && !connectMutation.isPending;

  return (
    <section className="composio-onb" aria-label="Connect Composio">
      <div className="composio-onb-card">
        <span className="composio-onb-eyebrow">Integrations</span>
        <h2 className="composio-onb-title">
          Connect Composio to add integrations
        </h2>
        <p className="composio-onb-lead">
          Composio is the gateway your team uses to act in Gmail, Slack, GitHub,
          and 250+ other tools — securely, with OAuth and a full audit trail.
          Paste your own Composio API key to browse and connect them.
        </p>

        <div className="composio-onb-logos" aria-hidden="true">
          <span className="composio-onb-logo">
            <GmailLogo />
          </span>
          <span className="composio-onb-logo">
            <SlackLogo />
          </span>
          <span className="composio-onb-logo">
            <GitHubLogo />
          </span>
          <span className="composio-onb-more">+250 more</span>
        </div>

        <form
          className="composio-onb-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (canSubmit) connectMutation.mutate(trimmed);
          }}
        >
          <label className="composio-onb-label" htmlFor="composio-api-key">
            Composio API key
          </label>
          <div className="composio-onb-field">
            <input
              id="composio-api-key"
              className="input composio-onb-input"
              type={reveal ? "text" : "password"}
              placeholder="comp_…"
              autoComplete="off"
              spellCheck={false}
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              disabled={connectMutation.isPending}
            />
            <button
              type="button"
              className="composio-onb-reveal"
              onClick={() => setReveal((v) => !v)}
              aria-pressed={reveal}
            >
              {reveal ? "Hide" : "Show"}
            </button>
          </div>

          <div className="composio-onb-actions">
            <button
              type="submit"
              className="btn btn-primary composio-onb-submit"
              disabled={!canSubmit}
            >
              {connectMutation.isPending ? "Connecting…" : "Connect Composio"}
            </button>
            <a
              className="composio-onb-getkey"
              href={COMPOSIO_KEYS_URL}
              target="_blank"
              rel="noopener noreferrer"
            >
              Get an API key
              <OpenNewWindow width={13} height={13} aria-hidden="true" />
            </a>
          </div>
        </form>

        <p className="composio-onb-foot">
          Your key is stored locally on this workspace and never leaves it. The
          Composio account it identifies is resolved from your workspace email.
        </p>
      </div>
    </section>
  );
}

import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { OpenNewWindow } from "iconoir-react";

import { updateConfig } from "../../../api/client";
import {
  type ComposioSigninState,
  getComposioSigninStatus,
  startComposioSignin,
} from "../../../api/integrations";
import { CommandRow } from "../../ui/CommandRow";
import { showNotice } from "../../ui/Toast";
import { GitHubLogo, GmailLogo, SlackLogo } from "./IntegrationLogos";

// ComposioOnboarding is the first-run state of the Integrations page when no
// Composio API key is connected. Composio powers the whole integration catalog
// (OAuth, action execution, audit), so without a key there is nothing to
// browse. The primary path is "Sign in with Composio": the broker drives the
// official composio CLI (login → dev init) and stores the project API key
// itself — no copy/paste. The manual key-paste form remains as a collapsed
// fallback for users who prefer pasting a key from the dashboard.

// API keys live in the project settings of the new Composio dashboard; the
// root URL routes signed-in users to their active project.
const COMPOSIO_KEYS_URL = "https://dashboard.composio.dev";

type SigninPhase =
  | "idle"
  | "installing"
  | "cli_missing"
  | "awaiting_login"
  | "provisioning"
  | "done"
  | "error";

interface ComposioOnboardingProps {
  /** Called after the key is saved so the page can re-fetch config + catalog. */
  onConnected: () => void;
}

export function ComposioOnboarding({ onConnected }: ComposioOnboardingProps) {
  const queryClient = useQueryClient();
  const [phase, setPhase] = useState<SigninPhase>("idle");
  const [authUrl, setAuthUrl] = useState("");
  const [installCommand, setInstallCommand] = useState("");
  const [signinError, setSigninError] = useState("");
  const [showManual, setShowManual] = useState(false);
  // Auto-open the login URL once per flow; re-renders and status polls must
  // not spawn extra tabs.
  const openedRef = useRef(false);

  const finishConnected = useCallback(async () => {
    showNotice("Integrations connected. Loading…", "success");
    await queryClient.invalidateQueries({ queryKey: ["config"] });
    await queryClient.invalidateQueries({ queryKey: ["integrations"] });
    onConnected();
  }, [queryClient, onConnected]);

  const applySigninState = useCallback(
    (state: ComposioSigninState) => {
      switch (state.status) {
        case "installing":
          // The broker is auto-installing the Composio CLI before it can mint
          // a login URL. We must enter (and keep polling) this phase — without
          // it the page would stall on "idle" and never open the sign-in tab.
          setPhase("installing");
          setInstallCommand(state.install_command ?? "");
          break;
        case "cli_missing":
          setPhase("cli_missing");
          setInstallCommand(state.install_command ?? "");
          break;
        case "awaiting_login":
          setPhase("awaiting_login");
          setAuthUrl(state.auth_url ?? "");
          if (state.auth_url && !openedRef.current) {
            openedRef.current = true;
            window.open(state.auth_url, "_blank", "noopener");
          }
          break;
        case "provisioning":
          setPhase("provisioning");
          break;
        case "done":
          setPhase("done");
          void finishConnected();
          break;
        case "error":
          setPhase("error");
          setSigninError(state.reason ?? "Sign-in failed. Try again.");
          break;
        default:
          break;
      }
    },
    [finishConnected],
  );

  const signinMutation = useMutation({
    mutationFn: startComposioSignin,
    onSuccess: applySigninState,
    onError: (err: unknown) => {
      setPhase("error");
      setSigninError(
        err instanceof Error ? err.message : "Could not start sign-in",
      );
    },
  });

  const startSignin = () => {
    openedRef.current = false;
    setSigninError("");
    signinMutation.mutate();
  };

  const polling =
    phase === "installing" ||
    phase === "awaiting_login" ||
    phase === "provisioning";
  const statusQuery = useQuery({
    queryKey: ["composio-signin-status"],
    queryFn: getComposioSigninStatus,
    enabled: polling,
    refetchInterval: polling ? 1500 : false,
  });
  const statusState = statusQuery.data;
  useEffect(() => {
    if (polling && statusState) applySigninState(statusState);
  }, [polling, statusState, applySigninState]);

  const connectMutation = useMutation({
    mutationFn: (key: string) => updateConfig({ composio_api_key: key }),
    onSuccess: finishConnected,
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not save the API key",
        "error",
      );
    },
  });

  return (
    <section className="composio-onb" aria-label="Connect integrations">
      <div className="composio-onb-card">
        <span className="composio-onb-eyebrow">Integrations</span>
        <h2 className="composio-onb-title">Add integrations to your office</h2>
        <p className="composio-onb-lead">
          Connect once to let your agents act in Gmail, Slack, GitHub, and 250+
          other tools — securely, with OAuth and a full audit trail. We set up
          the rest.
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

        <SigninPanel
          phase={phase}
          authUrl={authUrl}
          installCommand={installCommand}
          starting={signinMutation.isPending}
          onStart={startSignin}
        />

        {phase === "error" && signinError ? (
          <p className="composio-onb-error" role="alert">
            {signinError}
          </p>
        ) : null}

        <button
          type="button"
          className="composio-onb-fallback-toggle"
          aria-expanded={showManual}
          onClick={() => setShowManual((v) => !v)}
        >
          or paste an API key
        </button>

        {showManual ? (
          <ManualKeyForm
            pending={connectMutation.isPending}
            onSubmit={(key) => connectMutation.mutate(key)}
          />
        ) : null}

        <p className="composio-onb-foot">
          Your credentials are stored locally on this workspace and never leave
          it.
        </p>
      </div>
    </section>
  );
}

interface SigninPanelProps {
  phase: SigninPhase;
  authUrl: string;
  installCommand: string;
  starting: boolean;
  onStart: () => void;
}

/** The primary sign-in surface — one panel per flow phase. */
function SigninPanel({
  phase,
  authUrl,
  installCommand,
  starting,
  onStart,
}: SigninPanelProps) {
  if (phase === "installing") {
    return (
      <div className="composio-onb-panel" role="status">
        <p className="composio-onb-panel-title">Setting up integrations…</p>
        <p className="composio-onb-panel-note">
          One-time setup. We’ll open the sign-in page automatically as soon as
          it’s ready.
        </p>
        <p className="composio-onb-wait">Working on it…</p>
      </div>
    );
  }
  if (phase === "cli_missing") {
    return (
      <div className="composio-onb-panel" role="status">
        <p className="composio-onb-panel-title">One quick terminal step</p>
        <p className="composio-onb-panel-note">
          Automatic setup didn’t finish. Run this in a terminal, then try again:
        </p>
        <CommandRow command={installCommand} />
        <div className="composio-onb-actions">
          <button
            type="button"
            className="btn btn-primary"
            onClick={onStart}
            disabled={starting}
          >
            Try again
          </button>
        </div>
      </div>
    );
  }
  if (phase === "awaiting_login") {
    return (
      <div className="composio-onb-panel" role="status">
        <p className="composio-onb-panel-title">
          Finish signing in in your browser
        </p>
        {authUrl ? (
          <p className="composio-onb-panel-note">
            We opened the sign-in page in a new tab. If it didn’t appear,{" "}
            <a
              className="composio-onb-getkey"
              href={authUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              open the sign-in link
              <OpenNewWindow width={13} height={13} aria-hidden="true" />
            </a>
            .
          </p>
        ) : (
          <p className="composio-onb-panel-note">
            Run <code>composio login</code> in a terminal to finish signing in —
            we’ll pick it up automatically.
          </p>
        )}
        <p className="composio-onb-wait">Waiting for you to finish…</p>
      </div>
    );
  }
  if (phase === "provisioning" || phase === "done") {
    return (
      <div className="composio-onb-panel" role="status">
        <p className="composio-onb-panel-title">Connecting your account…</p>
        <p className="composio-onb-panel-note">
          Saving your credentials to this workspace.
        </p>
      </div>
    );
  }
  return (
    <div className="composio-onb-actions">
      <button
        type="button"
        className="btn btn-primary composio-onb-submit"
        onClick={onStart}
        disabled={starting}
      >
        {starting ? "Connecting…" : "Connect integrations"}
      </button>
    </div>
  );
}

interface ManualKeyFormProps {
  pending: boolean;
  onSubmit: (key: string) => void;
}

/** Collapsed fallback: paste a project ak_ key from the dashboard. */
function ManualKeyForm({ pending, onSubmit }: ManualKeyFormProps) {
  const [apiKey, setApiKey] = useState("");
  const [reveal, setReveal] = useState(false);
  const trimmed = apiKey.trim();
  const canSubmit = trimmed.length > 0 && !pending;

  return (
    <form
      className="composio-onb-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit) onSubmit(trimmed);
      }}
    >
      <label className="composio-onb-label" htmlFor="composio-api-key">
        API key
      </label>
      <div className="composio-onb-field">
        <input
          id="composio-api-key"
          className="input composio-onb-input"
          type={reveal ? "text" : "password"}
          placeholder="ak_…"
          autoComplete="off"
          spellCheck={false}
          value={apiKey}
          onChange={(event) => setApiKey(event.target.value)}
          disabled={pending}
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
          className="btn composio-onb-submit"
          disabled={!canSubmit}
        >
          {pending ? "Connecting…" : "Save key"}
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
  );
}

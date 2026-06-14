import {
  type ReactElement,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import {
  connectSlackChannel,
  getSlackAppManifest,
  getSlackOnboardingStatus,
  type SlackAppManifest,
  saveSlackTokens,
} from "../../api/slackOnboarding";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";
import {
  SlackActivatingIllo,
  SlackBridgeIllo,
  SlackChannelIllo,
  SlackLiveIllo,
  SlackManifestIllo,
  SlackTokensIllo,
} from "./SlackConnectIllustrations";
import "../../styles/slack-onboarding.css";

type Step = "intro" | "create" | "tokens" | "channel" | "activating" | "done";

const STEPS: { key: Step; label: string }[] = [
  { key: "intro", label: "Overview" },
  { key: "create", label: "Create app" },
  { key: "tokens", label: "Tokens" },
  { key: "channel", label: "Channel" },
  { key: "done", label: "Live" },
];

// The visible progress index treats "activating" as part of "channel" so the
// rail doesn't flicker an extra dot during the restart.
function railIndex(step: Step): number {
  if (step === "activating") return 3;
  return STEPS.findIndex((s) => s.key === step);
}

const ILLO: Record<Step, (p: { className?: string }) => ReactElement> = {
  intro: SlackBridgeIllo,
  create: SlackManifestIllo,
  tokens: SlackTokensIllo,
  channel: SlackChannelIllo,
  activating: SlackActivatingIllo,
  done: SlackLiveIllo,
};

const HEADLINE: Record<Step, string> = {
  intro: "Make your AI agents work together in Slack",
  create: "Create your Slack app",
  tokens: "Paste your two tokens",
  channel: "Choose the channel",
  activating: "Bringing your agents online",
  done: "Your agents are live in Slack",
};

interface SlackConnectModalProps {
  open: boolean;
  onClose: () => void;
  /** Entry step. Defaults to "intro"; stories/screenshots use it to render a
   *  specific step directly. */
  initialStep?: Step;
}

function errorText(err: unknown): string {
  if (err && typeof err === "object" && "message" in err) {
    return String((err as { message: unknown }).message);
  }
  return "Something went wrong. Try again.";
}

export function SlackConnectModal({
  open,
  onClose,
  initialStep = "intro",
}: SlackConnectModalProps) {
  const [step, setStep] = useState<Step>(initialStep);
  const [manifest, setManifest] = useState<SlackAppManifest | null>(null);
  const [copied, setCopied] = useState(false);
  const [botToken, setBotToken] = useState("");
  const [appToken, setAppToken] = useState("");
  const [identity, setIdentity] = useState<{
    bot: string;
    workspace: string;
  } | null>(null);
  const [channelId, setChannelId] = useState("");
  const [channelName, setChannelName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activatePhase, setActivatePhase] = useState(0);
  const aborted = useRef(false);

  // Reset to a clean slate whenever the wizard is (re)opened.
  useEffect(() => {
    if (open) {
      aborted.current = false;
      setStep(initialStep);
      setError(null);
      setActivatePhase(0);
      setCopied(false);
    }
    return () => {
      aborted.current = true;
    };
  }, [open]);

  // Fetch the manifest the first time the user reaches the create step.
  useEffect(() => {
    if (step === "create" && !manifest) {
      getSlackAppManifest()
        .then((m) => !aborted.current && setManifest(m))
        .catch((err) => !aborted.current && setError(errorText(err)));
    }
  }, [step, manifest]);

  const copyManifest = useCallback(async () => {
    if (!manifest) return;
    try {
      await navigator.clipboard.writeText(manifest.manifest_json);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      showNotice("Copy failed — select the text and copy manually.", "error");
    }
  }, [manifest]);

  const verifyTokens = useCallback(async () => {
    setError(null);
    setBusy(true);
    try {
      const r = await saveSlackTokens(botToken.trim(), appToken.trim());
      if (aborted.current) return;
      setIdentity({ bot: r.bot_name, workspace: r.workspace });
      setStep("channel");
    } catch (err) {
      if (!aborted.current) setError(errorText(err));
    } finally {
      if (!aborted.current) setBusy(false);
    }
  }, [botToken, appToken]);

  // The activation sequence: connect the channel — which hot-starts the Socket
  // Mode transport in-process, no broker restart — then poll /slack/status until
  // the transport reports a live connection. The office stays up the whole time,
  // so this is fast; the poll just waits for the WebSocket to come up.
  const activate = useCallback(async () => {
    setError(null);
    setStep("activating");
    setActivatePhase(0);
    try {
      await connectSlackChannel(
        channelId.trim(),
        channelName.trim() || undefined,
      );
      if (aborted.current) return;
      // Channel bound + the broker has hot-started the bridge; now we wait for
      // the Socket Mode connection to report healthy.
      setActivatePhase(1);

      // Poll up to ~20s for the Socket Mode connection to come up. The office
      // never goes down, so a failed probe is a genuine transient, not a reboot.
      const deadline = Date.now() + 20_000;
      while (!aborted.current && Date.now() < deadline) {
        try {
          const st = await getSlackOnboardingStatus();
          if (st.ready) {
            setActivatePhase(2);
            await new Promise((r) => setTimeout(r, 600));
            if (!aborted.current) setStep("done");
            return;
          }
        } catch {
          // transient — keep polling
        }
        await new Promise((r) => setTimeout(r, 1000));
      }
      if (!aborted.current) {
        setError(
          "Your office is taking longer than usual to connect to Slack. It may still be coming up — close this and check the channel in a moment.",
        );
        setStep("channel");
      }
    } catch (err) {
      if (!aborted.current) {
        setError(errorText(err));
        setStep("channel");
      }
    }
  }, [channelId, channelName]);

  if (!open) return null;

  const Illo = ILLO[step];
  const tokensValid =
    botToken.trim().startsWith("xoxb-") && appToken.trim().startsWith("xapp-");

  return (
    <div
      className="sc-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="Connect Slack"
    >
      <div className="sc-card" data-testid={`sc-step-${step}`}>
        {/* Left rail: progress + illustration */}
        <aside className="sc-rail">
          <div className="sc-rail-brand">
            <span className="sc-rail-dot" />
            Slack integration
          </div>
          <div className="sc-illo-wrap" key={step}>
            <Illo className="sc-illo" />
          </div>
          <ol className="sc-progress" aria-label="Setup progress">
            {STEPS.map((s, i) => {
              const active = i === railIndex(step);
              const done = i < railIndex(step);
              return (
                <li
                  key={s.key}
                  className={`sc-progress-item${active ? " is-active" : ""}${done ? " is-done" : ""}`}
                >
                  <span className="sc-progress-mark">{done ? "✓" : i + 1}</span>
                  {s.label}
                </li>
              );
            })}
          </ol>
        </aside>

        {/* Right stage: the current step */}
        <section className="sc-stage" key={`stage-${step}`}>
          <button
            className="sc-close"
            onClick={onClose}
            aria-label="Close"
            disabled={step === "activating"}
          >
            ✕
          </button>
          <h2 className="sc-headline">{HEADLINE[step]}</h2>

          {error && (
            <div className="sc-banner sc-banner-error" role="alert">
              {error}
            </div>
          )}

          {step === "intro" && (
            <div className="sc-body">
              <p className="sc-lead">
                In about two minutes, WUPHF's agents join your Slack — and the
                other AI agents in the channel start working together. The CEO
                pulls them into the work and coordinates it, every task gets its
                own thread, and the team wiki is one tab away. No terminal, no
                config files.
              </p>
              <ul className="sc-checklist">
                <li>
                  <b>Create a Slack app</b> from a manifest we generate (one
                  paste).
                </li>
                <li>
                  <b>Paste two tokens</b> so the office can read and post.
                </li>
                <li>
                  <b>Pick a channel</b> — then invite your other AI agents and
                  WUPHF coordinates them.
                </li>
              </ul>
              <div className="sc-actions">
                <button
                  className="sc-btn sc-btn-primary"
                  data-testid="sc-start"
                  onClick={() => setStep("create")}
                >
                  Let's go →
                </button>
              </div>
            </div>
          )}

          {step === "create" && (
            <div className="sc-body">
              <p className="sc-lead">
                Open Slack's app builder, choose <b>“From an app manifest,”</b>{" "}
                and paste this. It pre-configures every scope, Socket Mode, and
                the events the office needs.
              </p>
              <div className="sc-manifest">
                <pre className="sc-manifest-code" data-testid="sc-manifest">
                  {manifest ? manifest.manifest_json : "Loading manifest…"}
                </pre>
                <button
                  className="sc-copy"
                  onClick={copyManifest}
                  disabled={!manifest}
                  data-testid="sc-copy"
                >
                  {copied ? "Copied ✓" : "Copy manifest"}
                </button>
              </div>
              <div className="sc-actions">
                <button
                  className="sc-btn sc-btn-ghost"
                  onClick={() => setStep("intro")}
                >
                  Back
                </button>
                <a
                  className="sc-btn sc-btn-outline"
                  href={
                    manifest?.create_url ??
                    "https://api.slack.com/apps?new_app=1"
                  }
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Open Slack app builder ↗
                </a>
                <button
                  className="sc-btn sc-btn-primary"
                  data-testid="sc-created"
                  onClick={() => setStep("tokens")}
                >
                  I've created the app →
                </button>
              </div>
            </div>
          )}

          {step === "tokens" && (
            <div className="sc-body">
              <p className="sc-lead">
                Two tokens from your new app. We verify the bot token with Slack
                before saving.
              </p>
              <label className="sc-field">
                <span className="sc-field-label">Bot User OAuth Token</span>
                <input
                  className="sc-input"
                  data-testid="sc-bot-token"
                  type="password"
                  placeholder="xoxb-…"
                  value={botToken}
                  onChange={(e) => setBotToken(e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
                <span className="sc-field-help">
                  OAuth &amp; Permissions → Bot User OAuth Token, after
                  installing the app.
                </span>
              </label>
              <label className="sc-field">
                <span className="sc-field-label">App-Level Token</span>
                <input
                  className="sc-input"
                  data-testid="sc-app-token"
                  type="password"
                  placeholder="xapp-…"
                  value={appToken}
                  onChange={(e) => setAppToken(e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
                <span className="sc-field-help">
                  Basic Information → App-Level Tokens → generate with the{" "}
                  <code>connections:write</code> scope.
                </span>
              </label>
              <div className="sc-actions">
                <button
                  className="sc-btn sc-btn-ghost"
                  onClick={() => setStep("create")}
                >
                  Back
                </button>
                <button
                  className="sc-btn sc-btn-primary"
                  data-testid="sc-verify"
                  onClick={verifyTokens}
                  disabled={!tokensValid || busy}
                >
                  {busy ? "Verifying with Slack…" : "Verify & continue →"}
                </button>
              </div>
            </div>
          )}

          {step === "channel" && (
            <div className="sc-body">
              {identity && (
                <div
                  className="sc-banner sc-banner-ok"
                  data-testid="sc-identity"
                >
                  Connected as <b>{identity.bot}</b>
                  {identity.workspace ? (
                    <>
                      {" "}
                      in <b>{identity.workspace}</b>
                    </>
                  ) : null}{" "}
                  ✓
                </div>
              )}
              <p className="sc-lead">
                Invite the bot to a channel in Slack (
                <code>/invite @wuphf</code>), then tell us which one.
              </p>
              <label className="sc-field">
                <span className="sc-field-label">Channel ID</span>
                <input
                  className="sc-input"
                  data-testid="sc-channel-id"
                  placeholder="C0XXXXXXXXX"
                  value={channelId}
                  onChange={(e) => setChannelId(e.target.value)}
                  spellCheck={false}
                />
                <span className="sc-field-help">
                  In Slack: open the channel → its name → scroll to the bottom
                  of <b>About</b> for the Channel ID.
                </span>
              </label>
              <label className="sc-field">
                <span className="sc-field-label">
                  Display name <span className="sc-optional">(optional)</span>
                </span>
                <input
                  className="sc-input"
                  placeholder="wuphf-office"
                  value={channelName}
                  onChange={(e) => setChannelName(e.target.value)}
                  spellCheck={false}
                />
              </label>
              <div className="sc-actions">
                <button
                  className="sc-btn sc-btn-ghost"
                  onClick={() => setStep("tokens")}
                >
                  Back
                </button>
                <button
                  className="sc-btn sc-btn-primary"
                  data-testid="sc-activate"
                  onClick={activate}
                  disabled={channelId.trim().length < 6}
                >
                  Connect &amp; go live →
                </button>
              </div>
            </div>
          )}

          {step === "activating" && (
            <div className="sc-body sc-activating">
              <ol className="sc-phases">
                {[
                  "Connecting the channel",
                  "Starting the Slack bridge",
                  "Confirming you're live",
                ].map((label, i) => (
                  <li
                    key={label}
                    className={`sc-phase${activatePhase > i ? " is-done" : ""}${activatePhase === i ? " is-active" : ""}`}
                  >
                    <span className="sc-phase-mark">
                      {activatePhase > i ? "✓" : ""}
                    </span>
                    {label}
                  </li>
                ))}
              </ol>
              <p className="sc-muted">
                This takes a few seconds while your office connects to Slack.
              </p>
            </div>
          )}

          {step === "done" && (
            <div className="sc-body sc-done">
              <p className="sc-lead">
                WUPHF's agents are now in Slack
                {identity?.workspace ? (
                  <>
                    {" "}
                    on <b>{identity.workspace}</b>
                  </>
                ) : null}
                . <b>Invite your other AI agents to the channel</b> — vendor
                bots or your team's own — and WUPHF's CEO pulls them into the
                work and coordinates them. @-mention the team to kick off a
                task; every task gets its own thread, and the <b>wuphf</b> Home
                tab shows the board and wiki.
              </p>
              <div className="sc-nextcard">
                <span className="sc-nextcard-k">Next</span>
                <span>
                  Post <code>@wuphf</code> a goal in the channel and watch the
                  office pick it up.
                </span>
              </div>
              <div className="sc-actions">
                <button
                  className="sc-btn sc-btn-primary"
                  data-testid="sc-done"
                  onClick={onClose}
                >
                  Done
                </button>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

/** Mounted once near the app root; reads the open flag from the store. */
export function SlackConnectHost() {
  const open = useAppStore((s) => s.slackConnectOpen);
  const setOpen = useAppStore((s) => s.setSlackConnectOpen);
  return <SlackConnectModal open={open} onClose={() => setOpen(false)} />;
}

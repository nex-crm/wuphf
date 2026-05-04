import { useCallback, useEffect, useRef, useState } from "react";

import {
  connectTelegramChannel,
  discoverTelegramChats,
  type TelegramGroup,
  verifyTelegramBot,
} from "../../api/client";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";

// The wk-modal-* / wk-editor-* primitives this wizard renders are defined in
// wiki.css, which is otherwise only pulled in when the user navigates to the
// Wiki page. The Telegram wizard is mounted globally via TelegramConnectHost
// and triggered from anywhere via /connect, so we import the stylesheet here
// to guarantee the modal has its card surface, backdrop, and form controls
// regardless of route. Vite/Rollup dedupes the import — no double-load cost.
import "../../styles/wiki.css";

type Step =
  | "provider"
  | "token"
  | "verifying"
  | "mode"
  | "discovering"
  | "pick"
  | "manual"
  | "connecting"
  | "done";

interface TelegramConnectModalProps {
  /** When false, the modal renders nothing — checked by the host. */
  open: boolean;
  /** Where to enter the wizard. "provider" shows the integration picker
   *  (parity with the TUI's `/connect` 4-option picker); "telegram" skips
   *  the picker and lands on the bot-token entry step. */
  initialStep: "provider" | "token";
  /** Called when the user closes or finishes the wizard. */
  onClose: () => void;
}

/**
 * Web port of the TUI's `/connect telegram` flow. Walks the user from a
 * pasted bot token → group discovery → channel creation, all without
 * leaving the browser. Failures keep the user on the same step with the
 * inputs still editable so they can retry.
 *
 * E2E-targeted hooks: every step root carries `data-testid="tg-step-<step>"`
 * and primary controls carry `data-testid="tg-..."` so the Playwright
 * suite can drive the wizard without depending on copy.
 */
export function TelegramConnectModal({
  open,
  initialStep,
  onClose,
}: TelegramConnectModalProps) {
  const [step, setStep] = useState<Step>(initialStep);
  const [token, setToken] = useState("");
  const [botName, setBotName] = useState<string | null>(null);
  const [groups, setGroups] = useState<TelegramGroup[]>([]);
  const [manualChatID, setManualChatID] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [createdSlug, setCreatedSlug] = useState<string | null>(null);

  // requestIdRef gates state updates after an `await`; abortRef cancels the
  // underlying fetch. The combo handles two distinct bug classes:
  //
  //   1. Stale state writes — if the user clicks Cancel mid-verify, the late
  //      response must not push the (now hidden) modal back to "pick" or fire
  //      a "Connected #…" toast. The id check before each setState handles
  //      that.
  //
  //   2. Wasted server-side work — even if we ignore the response, the broker
  //      keeps running: SendTelegramMessage to the chat, manifest sync, the
  //      whole channel create. A rapid Cancel → reopen → connect-different-
  //      chat sequence would otherwise create two channels racing each other.
  //      AbortController makes the server see the disconnect and shed the
  //      request.
  const requestIdRef = useRef(0);
  const abortRef = useRef<AbortController | null>(null);

  const reset = useCallback(() => {
    setStep(initialStep);
    setToken("");
    setBotName(null);
    setGroups([]);
    setManualChatID("");
    setError(null);
    setCreatedSlug(null);
    // Bump so any in-flight verify/connect from this session is ignored,
    // and abort the live fetch so the broker stops processing it.
    requestIdRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
  }, [initialStep]);

  // Reset when the wizard opens, and invalidate requests when it closes. The
  // component stays mounted by the host, so closing must still abort fetches
  // and bump the request id or a late response can update hidden state.
  useEffect(() => {
    if (open) {
      reset();
      return;
    }
    requestIdRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
  }, [open, reset]);

  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  // beginRequest cancels any in-flight wizard fetch and returns a fresh
  // (id, signal) pair tied to a new AbortController. Callers compare the
  // captured id against requestIdRef.current after each `await` to skip
  // state writes from stale responses. AbortError caught below is swallowed
  // — that's exactly what we want, since reset() was the trigger.
  function beginRequest(): { id: number; signal: AbortSignal } {
    requestIdRef.current += 1;
    const id = requestIdRef.current;
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    return { id, signal: ctrl.signal };
  }

  function isAbortError(e: unknown): boolean {
    return (
      typeof e === "object" &&
      e !== null &&
      "name" in e &&
      (e as { name?: string }).name === "AbortError"
    );
  }

  async function verify(rawToken: string) {
    const trimmed = rawToken.trim();
    if (!trimmed) {
      setError("Bot token is required.");
      return;
    }
    const { id: myReq, signal } = beginRequest();
    setError(null);
    setStep("verifying");
    try {
      const result = await verifyTelegramBot(trimmed, signal);
      if (myReq !== requestIdRef.current) return;
      if (!result.ok) {
        setError(result.error ?? "Bot verification failed.");
        setStep("token");
        return;
      }
      setBotName(result.bot_name ?? null);
      setStep("mode");
    } catch (e: unknown) {
      if (myReq !== requestIdRef.current || isAbortError(e)) return;
      const msg = e instanceof Error ? e.message : String(e);
      setError(msg);
      setStep("token");
    }
  }

  async function discover() {
    const { id: myReq, signal } = beginRequest();
    setError(null);
    setStep("discovering");
    try {
      const found = await discoverTelegramChats(token, signal);
      if (myReq !== requestIdRef.current) return;
      setGroups(found.groups ?? []);
      setStep("pick");
    } catch (e: unknown) {
      if (myReq !== requestIdRef.current || isAbortError(e)) return;
      const msg = e instanceof Error ? e.message : String(e);
      setError(msg);
      setStep("mode");
    }
  }

  async function connect(
    opts: {
      chat_id: number;
      title?: string;
      type?: string;
    },
    fallbackStep: "mode" | "pick" | "manual" = "pick",
  ) {
    const { id: myReq, signal } = beginRequest();
    setError(null);
    setStep("connecting");
    try {
      const result = await connectTelegramChannel({ token, ...opts }, signal);
      if (myReq !== requestIdRef.current) return;
      setCreatedSlug(result.channel_slug);
      setStep("done");
      showNotice(`Connected #${result.channel_slug}`, "success");
    } catch (e: unknown) {
      if (myReq !== requestIdRef.current || isAbortError(e)) return;
      const msg = e instanceof Error ? e.message : String(e);
      setError(msg);
      setStep(fallbackStep);
    }
  }

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="tg-connect-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="tg-connect-title"
    >
      <div className="wk-modal" style={{ maxWidth: 480 }}>
        <h2 id="tg-connect-title">
          {step === "provider" ? "Connect an integration" : "Connect Telegram"}
        </h2>

        {step === "provider" && (
          <div data-testid="tg-step-provider">
            <p className="wk-editor-help">
              Pick an integration to bring into the office.
            </p>
            <ul style={{ listStyle: "none", padding: 0, marginTop: 8 }}>
              <li style={{ marginBottom: 4 }}>
                <button
                  type="button"
                  data-testid="tg-provider-telegram"
                  className="wk-editor-cancel"
                  style={{ width: "100%", textAlign: "left" }}
                  onClick={() => setStep("token")}
                >
                  Telegram{" "}
                  <span style={{ opacity: 0.6 }}>— bridge a group or DM</span>
                </button>
              </li>
              <li style={{ marginBottom: 4 }}>
                <button
                  type="button"
                  data-testid="tg-provider-openclaw"
                  className="wk-editor-cancel"
                  style={{ width: "100%", textAlign: "left" }}
                  onClick={() => {
                    showNotice(
                      "OpenClaw connect lives in the TUI today. Run ./wuphf in your terminal, then /connect openclaw. Web wizard coming soon.",
                      "info",
                    );
                    onClose();
                  }}
                >
                  OpenClaw{" "}
                  <span style={{ opacity: 0.6 }}>— TUI only for now</span>
                </button>
              </li>
              <li style={{ marginBottom: 4 }}>
                <button
                  type="button"
                  data-testid="tg-provider-slack"
                  className="wk-editor-cancel"
                  style={{ width: "100%", textAlign: "left", opacity: 0.5 }}
                  disabled={true}
                >
                  Slack <span style={{ opacity: 0.6 }}>— coming soon</span>
                </button>
              </li>
              <li style={{ marginBottom: 4 }}>
                <button
                  type="button"
                  data-testid="tg-provider-discord"
                  className="wk-editor-cancel"
                  style={{ width: "100%", textAlign: "left", opacity: 0.5 }}
                  disabled={true}
                >
                  Discord <span style={{ opacity: 0.6 }}>— coming soon</span>
                </button>
              </li>
            </ul>
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {step === "token" && (
          <div data-testid="tg-step-token">
            <label className="wk-editor-label" htmlFor="tg-token">
              Bot token from @BotFather
            </label>
            <input
              id="tg-token"
              data-testid="tg-token-input"
              className="wk-editor-commit"
              type="password"
              autoComplete="off"
              placeholder="123456:ABC..."
              value={token}
              onChange={(e) => setToken(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void verify(token);
              }}
            />
            {error && (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
              >
                {error}
              </div>
            )}
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-save"
                data-testid="tg-token-submit"
                onClick={() => void verify(token)}
              >
                Verify
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {step === "verifying" && (
          <p data-testid="tg-step-verifying">Verifying bot token…</p>
        )}

        {step === "mode" && (
          <div data-testid="tg-step-mode">
            <p className="wk-editor-help">
              {botName ? `Bot: ${botName}. ` : ""}How do you want to use this
              bot?
            </p>
            {error && (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
              >
                {error}
              </div>
            )}
            <div
              className="wk-editor-actions"
              style={{ flexDirection: "column", alignItems: "stretch" }}
            >
              <button
                type="button"
                className="wk-editor-save"
                data-testid="tg-mode-dm"
                onClick={() =>
                  void connect(
                    {
                      chat_id: 0,
                      title: "Telegram DM",
                      type: "private",
                    },
                    "mode",
                  )
                }
              >
                DM{" "}
                <span style={{ opacity: 0.6, fontWeight: "normal" }}>
                  — messages go straight to the bot inbox
                </span>
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                data-testid="tg-mode-group"
                onClick={() => void discover()}
              >
                Group chat{" "}
                <span style={{ opacity: 0.6 }}>
                  — bridge a group or channel
                </span>
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {step === "discovering" && (
          <p data-testid="tg-step-discovering">
            Discovering groups{botName ? ` for ${botName}` : ""}…
          </p>
        )}

        {step === "pick" && (
          <div data-testid="tg-step-pick">
            <p className="wk-editor-help">
              {botName ? `Bot: ${botName}. ` : ""}
              {groups.length === 0
                ? "No groups found yet. Send a message in the group while the bot is a member, then retry — or enter the chat ID manually."
                : "Choose a chat to bridge into the office."}
            </p>

            {groups.length > 0 && (
              <ul
                data-testid="tg-group-list"
                style={{ listStyle: "none", padding: 0, marginTop: 8 }}
              >
                {groups.map((g) => (
                  <li key={g.chat_id} style={{ marginBottom: 4 }}>
                    <button
                      type="button"
                      data-testid={`tg-group-${g.chat_id}`}
                      className="wk-editor-cancel"
                      style={{ width: "100%", textAlign: "left" }}
                      onClick={() =>
                        void connect({
                          chat_id: g.chat_id,
                          title: g.title,
                          type: g.type,
                        })
                      }
                    >
                      {g.title || `Chat ${g.chat_id}`}{" "}
                      <span style={{ opacity: 0.6 }}>({g.chat_id})</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}

            {error && (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
              >
                {error}
              </div>
            )}

            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-cancel"
                data-testid="tg-retry-discover"
                onClick={() => void discover()}
              >
                Retry discovery
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                data-testid="tg-manual-chat-id"
                onClick={() => setStep("manual")}
              >
                Enter chat ID manually
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={() => setStep("mode")}
              >
                Back
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {step === "manual" && (
          <div data-testid="tg-step-manual">
            <label className="wk-editor-label" htmlFor="tg-manual-id">
              Chat ID (e.g. -5093020979)
            </label>
            <input
              id="tg-manual-id"
              data-testid="tg-manual-id-input"
              className="wk-editor-commit"
              type="text"
              // Telegram group IDs are negative integers and mobile numeric
              // keypads typically omit the minus sign, so inputMode="numeric"
              // would make this field unreachable on touch without paste. Keep
              // text and validate the value with the strict regex below.
              inputMode="text"
              pattern="-?[0-9]+"
              placeholder="-5093020979"
              value={manualChatID}
              onChange={(e) => setManualChatID(e.target.value)}
            />
            {error && (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
              >
                {error}
              </div>
            )}
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-save"
                data-testid="tg-manual-submit"
                onClick={() => {
                  // Number.parseInt accepts trailing junk ("-12abc" → -12),
                  // which would silently bridge the wrong chat. Require the
                  // entire trimmed string to be an optional minus + digits
                  // before converting, and use Number.isSafeInteger so chat
                  // IDs that exceed 2^53 (Telegram allows them) surface as
                  // a validation error rather than a silently-rounded id.
                  const raw = manualChatID.trim();
                  if (!/^-?\d+$/.test(raw)) {
                    setError("Chat ID must be a non-zero integer.");
                    return;
                  }
                  const parsed = Number(raw);
                  if (!Number.isSafeInteger(parsed) || parsed === 0) {
                    setError("Chat ID must be a non-zero integer.");
                    return;
                  }
                  void connect({ chat_id: parsed }, "manual");
                }}
              >
                Connect
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={() => setStep("pick")}
              >
                Back
              </button>
            </div>
          </div>
        )}

        {step === "connecting" && (
          <p data-testid="tg-step-connecting">Connecting…</p>
        )}

        {step === "done" && (
          <div data-testid="tg-step-done">
            <p>
              Connected. New channel:{" "}
              <code data-testid="tg-created-slug">#{createdSlug}</code>
            </p>
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-save"
                data-testid="tg-done-close"
                onClick={onClose}
              >
                Done
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/** Mounted once near the app root. Reads the open flag + mode from the
 *  store and translates the mode into the wizard's initial step. */
export function TelegramConnectHost() {
  const open = useAppStore((s) => s.telegramConnectOpen);
  const mode = useAppStore((s) => s.telegramConnectMode);
  const setOpen = useAppStore((s) => s.setTelegramConnectOpen);
  const initialStep = mode === "telegram" ? "token" : "provider";
  return (
    <TelegramConnectModal
      open={open}
      initialStep={initialStep}
      onClose={() => setOpen(false)}
    />
  );
}

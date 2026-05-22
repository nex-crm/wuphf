/**
 * SystemErrorCard — banner-style card for system-authored runtime errors.
 *
 * Issue #933. Provider auth failures ("Not logged in - Please run /login")
 * previously surfaced as agent-authored chat bubbles inside `#general`,
 * conflating in-character agent output with a system-level error. The
 * broker now emits these as system-authored messages with
 * kind="system_auth_error" carrying a structured payload; the SPA dispatches
 * here so they read as a clearly-separate "the system needs you to do
 * something" banner, not a CEO line.
 *
 * Security: payload fields are plain-text only — no innerHTML, no link
 * rendering, no rich content. The broker-side sanitizer is the authoritative
 * line; this component is the defense-in-depth render path.
 */

import { useState } from "react";

export interface SystemAuthErrorPayload {
  /** Provider id (e.g. "claude-code", "codex", "opencode"). May be empty. */
  provider?: string;
  /** Suggested shell command for the user to copy. May be empty. */
  sign_in_command?: string;
  /** Truncated detail from the upstream error. Plain text. */
  detail?: string;
  /** Slug of the agent whose loop surfaced the auth failure. Plain text. */
  reporter?: string;
}

export interface SystemErrorCardProps {
  payload: SystemAuthErrorPayload;
  /** Optional retry handler — surfaces a "Retry" button after sign-in. */
  onRetry?: () => void;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

/**
 * Defensive payload parser. The Go side guarantees the shape but the wire is
 * `unknown`, so narrow each field before rendering. Returns a safe empty
 * payload when the input is not a plain object.
 */
export function parseSystemAuthErrorPayload(
  raw: unknown,
): SystemAuthErrorPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: SystemAuthErrorPayload = {};
  if (isStringField(r.provider)) out.provider = r.provider;
  if (isStringField(r.sign_in_command)) out.sign_in_command = r.sign_in_command;
  if (isStringField(r.detail)) out.detail = r.detail;
  if (isStringField(r.reporter)) out.reporter = r.reporter;
  return out;
}

export function SystemErrorCard({ payload, onRetry }: SystemErrorCardProps) {
  const [copied, setCopied] = useState(false);
  const providerLabel = payload.provider ? payload.provider : "provider";
  const command = payload.sign_in_command ?? "";
  const detail = payload.detail ?? "";

  function copyCommand(): void {
    if (!command) return;
    if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
      // No clipboard surface — keep the command visible in the card so the
      // user can select-and-copy manually. Do not throw.
      return;
    }
    navigator.clipboard
      .writeText(command)
      .then(() => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1800);
      })
      .catch(() => {
        // Permission denied or insecure context — same fallback as above.
      });
  }

  return (
    <div
      role="alert"
      className="system-error-card"
      data-testid="system-error-card"
      data-provider={payload.provider ?? ""}
    >
      <div className="system-error-card-head">
        <span className="system-error-card-eyebrow">Sign in required</span>
        <h4 className="system-error-card-title">
          {`Sign in to ${providerLabel} to keep working`}
        </h4>
      </div>
      {detail ? <p className="system-error-card-detail">{detail}</p> : null}
      {command ? (
        <div className="system-error-card-command">
          <code data-testid="system-error-card-command">{command}</code>
          <button
            type="button"
            className="system-error-card-copy-btn"
            data-testid="system-error-card-copy"
            onClick={copyCommand}
          >
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
      ) : null}
      {onRetry ? (
        <div className="system-error-card-actions">
          <button
            type="button"
            className="system-error-card-retry-btn"
            data-testid="system-error-card-retry"
            onClick={onRetry}
          >
            Retry
          </button>
        </div>
      ) : null}
    </div>
  );
}

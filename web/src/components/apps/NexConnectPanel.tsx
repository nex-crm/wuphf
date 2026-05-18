/**
 * NexConnectPanel — "Connect Nex" card for Settings → Integrations.
 *
 * Moved here from the legacy onboarding wizard as part of Phase 5 cleanup.
 *
 * Wire endpoint: POST /nex/register { email } → { status, output? }
 * When the broker reports nex-cli is unusable here (missing binary, or only
 * the @nex-ai/nex npm shim without its backing binary), the panel flips to a
 * fallback that surfaces the copy-paste install command — wuphf never runs
 * the install for the user (same policy as the Local LLMs panel) — plus a
 * register-at-nex.ai escape hatch for machines without terminal access.
 *
 * Nex is a context graph platform for AI agents, not a CRM.
 */

import type { CSSProperties } from "react";
import { useState } from "react";

import { post } from "../../api/client";
import { CommandRow } from "../ui/CommandRow";
import { showNotice } from "../ui/Toast";

type NexConnectStatus = "open" | "submitting" | "ok" | "fallback";

// One-line installer for the nex-cli binary. Matches install.sh and the
// nex-as-a-skill README; needs no Node and writes the binary directly, so
// it works regardless of how the user got wuphf.
const NEX_INSTALL_COMMAND =
  "curl -fsSL https://raw.githubusercontent.com/nex-crm/nex-as-a-skill/main/install.sh | sh";

/**
 * The broker reports /nex/register failures as a JSON body
 * {"status":"error","error":"..."}, which api/client throws verbatim as the
 * Error message. Pull out the human-readable `error` field so the panel
 * never renders a raw JSON blob at the user.
 */
function readNexError(raw: string): string {
  try {
    const parsed = JSON.parse(raw) as { error?: unknown };
    if (typeof parsed.error === "string" && parsed.error.trim()) {
      return parsed.error.trim();
    }
  } catch {
    // Not JSON (e.g. a bare "502 Bad Gateway") — fall through to raw text.
  }
  return raw;
}

/**
 * True when the failure means nex-cli can't run on this machine: the binary
 * is missing, or only the @nex-ai/nex npm shim is present without the real
 * binary behind it (the shim prints "nex-cli binary not found"). Both should
 * flip the panel to the install-instructions fallback rather than show an
 * error the user can't act on.
 */
function isNexUnavailable(detail: string): boolean {
  const m = detail.toLowerCase();
  return (
    m.includes("not installed") ||
    m.includes("binary not found") ||
    m.includes("502")
  );
}

const descStyle: CSSProperties = {
  fontSize: 12,
  color: "var(--text-secondary)",
  margin: "0 0 12px",
  lineHeight: 1.5,
};

export function NexConnectPanel() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<NexConnectStatus>("open");
  const [error, setError] = useState("");

  const handleSubmit = async () => {
    if (!email.trim() || status === "submitting") return;
    setStatus("submitting");
    setError("");
    try {
      await post<{ status: string; output?: string }>("/nex/register", {
        email: email.trim(),
      });
      setStatus("ok");
      showNotice("Check your inbox for the Nex API key.", "success");
    } catch (err: unknown) {
      const raw = err instanceof Error ? err.message : "Registration failed";
      const detail = readNexError(raw);
      // nex-cli unusable here → install-instructions fallback. Anything else
      // is a real, actionable error: show the parsed message, not a blob.
      if (isNexUnavailable(detail)) {
        setStatus("fallback");
      } else {
        setError(detail);
        setStatus("open");
      }
    }
  };

  return (
    <div
      data-testid="nex-connect-panel"
      style={{
        marginTop: 20,
        padding: 16,
        border: "1px solid var(--border-light)",
        borderRadius: 6,
        background: "var(--bg-card)",
      }}
    >
      <div
        style={{
          fontSize: 14,
          fontWeight: 600,
          color: "var(--text)",
          marginBottom: 6,
        }}
      >
        Nex
      </div>

      {status === "fallback" ? (
        <>
          <p style={descStyle}>
            nex-cli isn't installed on this machine. Install it from a terminal,
            then come back and connect:
          </p>
          <CommandRow command={NEX_INSTALL_COMMAND} />
          <div style={{ marginTop: 10 }}>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => {
                setError("");
                setStatus("open");
              }}
            >
              I've installed it — try again
            </button>
          </div>
          <p style={{ ...descStyle, margin: "10px 0 0" }}>
            No terminal access? You can also{" "}
            <a
              href="https://nex.ai/register"
              target="_blank"
              rel="noopener noreferrer"
            >
              register at nex.ai/register
            </a>{" "}
            and paste your API key in the API Keys section above.
          </p>
        </>
      ) : (
        <>
          <p style={descStyle}>
            {status === "ok"
              ? `Check your inbox at ${email} for the Nex API key. Paste it in the API Keys section above once it arrives.`
              : "Register an email to get a free Nex API key. Powers shared memory, entity briefs, and integrations."}
          </p>
          {status === "ok" ? null : (
            <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
              <div style={{ flex: 1 }}>
                <input
                  style={{
                    width: "100%",
                    padding: "6px 10px",
                    fontSize: 13,
                    border: "1px solid var(--border-light)",
                    borderRadius: 4,
                    background: "var(--bg)",
                    color: "var(--text)",
                    fontFamily: "inherit",
                    boxSizing: "border-box",
                  }}
                  id="nex-connect-email"
                  type="email"
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  onKeyDown={(e) => {
                    if (
                      e.key === "Enter" &&
                      status !== "submitting" &&
                      email.trim().length > 0
                    ) {
                      e.preventDefault();
                      void handleSubmit();
                    }
                  }}
                  disabled={status === "submitting"}
                  aria-label="Email address for Nex registration"
                />
                {error ? (
                  <p
                    style={{
                      color: "var(--red)",
                      fontSize: 12,
                      marginTop: 4,
                      marginBottom: 0,
                    }}
                    role="alert"
                  >
                    {error}
                  </p>
                ) : null}
              </div>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                onClick={() => void handleSubmit()}
                disabled={status === "submitting" || !email.trim()}
                style={{ flexShrink: 0 }}
              >
                {status === "submitting" ? "Sending…" : "Connect Nex"}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

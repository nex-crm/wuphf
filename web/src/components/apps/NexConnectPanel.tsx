/**
 * NexConnectPanel — "Connect Nex" card for Settings → Integrations.
 *
 * Moved here from the legacy onboarding wizard as part of Phase 5 cleanup.
 *
 * Wire endpoint: POST /nex/register { email } → { status, output? }
 * On 502 the broker returns ErrNotInstalled; we flip to the external-link
 * fallback (open nex.ai/register, paste key in API Keys section).
 *
 * Nex is a context graph platform for AI agents, not a CRM.
 */

import { useState } from "react";

import { post } from "../../api/client";
import { showNotice } from "../ui/Toast";

type NexConnectStatus = "open" | "submitting" | "ok" | "fallback";

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
      const msg = err instanceof Error ? err.message : "Registration failed";
      // 502 means nex-cli not installed — flip to external link fallback.
      if (msg.includes("502") || msg.toLowerCase().includes("not installed")) {
        setStatus("fallback");
      } else {
        setError(msg);
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
      <p
        style={{
          fontSize: 12,
          color: "var(--text-secondary)",
          margin: "0 0 12px",
          lineHeight: 1.5,
        }}
      >
        {status === "fallback"
          ? "nex-cli is not installed on this machine. Register at nex.ai/register, then paste your API key in the API Keys section above."
          : status === "ok"
            ? `Check your inbox at ${email} for the Nex API key. Paste it in the API Keys section above once it arrives.`
            : "Register an email to get a free Nex API key. Powers shared memory, entity briefs, and integrations."}
      </p>

      {status === "fallback" ? (
        <a
          className="btn btn-secondary btn-sm"
          href="https://nex.ai/register"
          target="_blank"
          rel="noopener noreferrer"
        >
          Open nex.ai/register
        </a>
      ) : status === "ok" ? null : (
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
    </div>
  );
}

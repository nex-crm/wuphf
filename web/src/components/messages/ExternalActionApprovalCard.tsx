import { useState } from "react";

import type { ActionEnvelope, AgentRequest } from "../../api/client";
import {
  type ApprovalContext,
  parseApprovalContext,
} from "../../lib/parseApprovalContext";
import {
  GenericIntegrationLogo,
  ToolkitBrandLogo,
} from "../apps/integrations/IntegrationLogos";

// ExternalActionApprovalCard is the dedicated approval surface for a mutating
// integration action the deterministic resolver classified as `approve`
// (connected, no standing grant). It answers the one question the human is
// here to answer — "should this agent really send THIS, through THIS account?"
// — by showing the integration it acts on, the exact action, the agent's
// reason, and the full (secret-masked) payload, with a raw view one tap away.
//
// Phase 4a reads the legacy approval context string (parseApprovalContext); the
// platform, action id, account, and channel are all recoverable from it. Phase
// 4b will swap to a structured action-approval payload with the real HTTP
// envelope behind the raw toggle — same layout, richer source.

interface ExternalActionApprovalCardProps {
  request: AgentRequest;
  submitting: boolean;
  /** Answer with a plain choice id (e.g. "approve", "reject"). */
  onAnswer: (choiceId: string) => void;
  /** Approve this run AND mint a scoped grant so it is never re-asked. */
  onApproveAlways: (grant: ApprovalGrantTarget) => void;
  /** Cancel the request without approving or rejecting. */
  onDismiss: () => void;
}

export interface ApprovalGrantTarget {
  agentSlug: string;
  platform: string;
  actionId: string;
  channel?: string;
}

interface ActionIdentity {
  /** Human verb headline, e.g. "Send Email". */
  headline: string;
  /** Raw integration action id, e.g. "GMAIL_SEND_EMAIL". */
  actionId: string;
  /** Display platform name, e.g. "Gmail". */
  platformName: string;
  /** Lowercased platform slug used to resolve the brand logo. */
  platformSlug: string;
}

// deriveActionIdentity resolves the card's identity, preferring the structured
// payload (slice 4b) when present and falling back to the legacy strings: the Go
// side writes the footer as "Action: GMAIL_SEND_EMAIL via Gmail" and the title
// as "Send Email via Gmail", with the platform slug also derivable from the
// action id prefix.
export function deriveActionIdentity(
  request: AgentRequest,
  parsed: ApprovalContext | null,
): ActionIdentity {
  const structured = request.action;
  if (structured && (structured.action_id || structured.platform)) {
    const actionId = (structured.action_id ?? "").trim();
    const platformSlug = (
      structured.platform ||
      actionId.split("_")[0] ||
      ""
    )
      .trim()
      .toLowerCase();
    return {
      headline:
        (structured.verb ?? "").trim() ||
        stripVia(request.title) ||
        titleCaseTokens(actionId) ||
        "Run an action",
      actionId,
      platformName:
        (structured.name ?? "").trim() ||
        titleCaseTokens(structured.platform ?? "") ||
        "",
      platformSlug,
    };
  }

  const footerAction = parsed?.footer.action ?? "";
  const viaIdx = footerAction.lastIndexOf(" via ");
  const actionId = (
    viaIdx === -1 ? footerAction : footerAction.slice(0, viaIdx)
  ).trim();
  const platformFromFooter =
    viaIdx === -1 ? "" : footerAction.slice(viaIdx + 5).trim();

  const headline = stripVia(request.title) || titleCaseTokens(actionId) || "Run an action";

  const platformName =
    platformFromFooter || titleCaseTokens(request.platform ?? "") || "";
  const platformSlug = (
    request.platform ||
    platformFromFooter ||
    actionId.split("_")[0] ||
    ""
  )
    .trim()
    .toLowerCase();

  return { headline, actionId, platformName, platformSlug };
}

function stripVia(title: string | undefined): string {
  const t = (title ?? "").trim();
  if (!t || t === "Request") return "";
  const idx = t.toLowerCase().lastIndexOf(" via ");
  return (idx === -1 ? t : t.slice(0, idx)).trim();
}

function titleCaseTokens(raw: string): string {
  const t = raw.trim();
  if (!t) return "";
  return t
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
    .join(" ");
}

// canGrant guards the "always allow" escalation: a grant must be scoped to a
// concrete (agent, platform, action_id), so the button is suppressed when any
// of those cannot be determined rather than minting a malformed grant.
function grantTarget(
  request: AgentRequest,
  identity: ActionIdentity,
): ApprovalGrantTarget | null {
  const agentSlug = (request.from ?? "").trim();
  if (!agentSlug || !identity.platformSlug || !identity.actionId) return null;
  return {
    agentSlug,
    platform: identity.platformSlug,
    actionId: identity.actionId,
    channel: request.channel,
  };
}

export function ExternalActionApprovalCard({
  request,
  submitting,
  onAnswer,
  onApproveAlways,
  onDismiss,
}: ExternalActionApprovalCardProps) {
  const [showRaw, setShowRaw] = useState(false);
  const parsed = parseApprovalContext(request.context);
  const identity = deriveActionIdentity(request, parsed);
  const details = parsed?.details ?? [];
  const why = parsed?.why ?? null;
  const account =
    request.action?.account?.name ?? parsed?.footer.account ?? null;
  const channel = parsed?.footer.channel ?? request.channel ?? null;
  const grant = grantTarget(request, identity);
  // The masked HTTP envelope (slice 4b) is the real request behind the raw
  // toggle. When absent (legacy approvals), the raw view reformats the parsed
  // summary fields instead.
  const rawEnvelope = request.action?.raw_envelope ?? null;
  const hasRaw = Boolean(rawEnvelope) || details.length > 0;
  const hasPayload = hasRaw;

  const brandLogo = identity.platformSlug ? (
    <ToolkitBrandLogo platform={identity.platformSlug} />
  ) : null;

  return (
    <div className="eac">
      <header className="eac-head">
        <div className="eac-logo" aria-hidden="true">
          {brandLogo ?? <GenericIntegrationLogo />}
        </div>
        <div className="eac-headings">
          <span className="eac-eyebrow">
            {identity.platformName || "External integration"}
          </span>
          <h3 className="eac-headline">{identity.headline}</h3>
          {identity.actionId ? (
            <span className="eac-action-id mono">{identity.actionId}</span>
          ) : null}
        </div>
              </header>

      {request.connection_unverified ? (
        <p className="eac-unverified" role="status">
          <span className="eac-unverified-dot" aria-hidden="true" />
          Connection unverified — we could not confirm {identity.platformName ||
            "this integration"} is connected. Approve only if you trust it is
          set up.
        </p>
      ) : null}

      {why ? (
        <p className="eac-why">
          <span className="eac-why-label">Why</span>
          <span className="eac-why-text">{why}</span>
        </p>
      ) : null}

      <section className="eac-payload" aria-label="What will be sent">
        <div className="eac-payload-head">
          <span className="eac-payload-title">What will be sent</span>
          {hasRaw ? (
            <button
              type="button"
              className="eac-raw-toggle"
              aria-pressed={showRaw}
              onClick={() => setShowRaw((v) => !v)}
            >
              {showRaw ? "Hide raw" : "Show raw"}
            </button>
          ) : null}
        </div>

        {!hasPayload ? (
          <p className="eac-payload-empty">
            No structured payload — approve based on the action above.
          </p>
        ) : showRaw ? (
          <pre className="eac-raw mono">
            {rawEnvelope ? formatEnvelope(rawEnvelope) : rawPayload(details)}
          </pre>
        ) : details.length > 0 ? (
          <dl className="eac-fields">
            {details.map((detail) => (
              <div key={detail.label} className="eac-field">
                <dt className="eac-field-label">{detail.label}</dt>
                <dd className="eac-field-value">
                  {detail.value}
                  {detail.truncated ? (
                    <span className="eac-trunc">truncated</span>
                  ) : null}
                </dd>
              </div>
            ))}
          </dl>
        ) : (
          // Only the raw envelope is available (no parsed summary) — show it.
          <pre className="eac-raw mono">
            {rawEnvelope ? formatEnvelope(rawEnvelope) : ""}
          </pre>
        )}
      </section>

      <div className="eac-meta">
        {account ? (
          <span className="eac-meta-item">
            <span className="eac-dot" aria-hidden="true" />
            <span className="eac-meta-label">Account</span>
            <span className="mono">{account}</span>
          </span>
        ) : null}
        {channel ? (
          <span className="eac-meta-item">
            <span className="eac-meta-label">Channel</span>#
            {channel.replace(/^#/, "")}
          </span>
        ) : null}
      </div>

      <div className="eac-actions">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          onClick={() => onAnswer("approve")}
          disabled={submitting}
        >
          Approve
        </button>
        {grant ? (
          <button
            type="button"
            className="btn btn-sm btn-ghost eac-always"
            onClick={() => onApproveAlways(grant)}
            disabled={submitting}
            title={`Always allow @${grant.agentSlug} to run ${grant.actionId} on ${identity.platformName || grant.platform} without asking again`}
          >
            Approve &amp; always allow
          </button>
        ) : null}
        <button
          type="button"
          className="btn btn-sm btn-ghost eac-reject"
          onClick={() => onAnswer("reject")}
          disabled={submitting}
        >
          Reject
        </button>
        <button
          type="button"
          className="btn btn-sm btn-ghost eac-dismiss"
          onClick={onDismiss}
          disabled={submitting}
        >
          Dismiss
        </button>
      </div>
    </div>
  );
}

// rawPayload reformats the already-masked summary rows into a raw, developer-
// readable block. It is a presentation of the SAME secret-masked data shown in
// the field list — never a new data source — so the raw view can never reveal
// anything the summary does not.
function rawPayload(details: ApprovalContext["details"]): string {
  const lines = details.map((d) => {
    const value = d.truncated ? `${d.value} (truncated)` : d.value;
    return `${d.label}: ${value}`;
  });
  return lines.join("\n");
}

// formatEnvelope renders the masked HTTP request (slice 4b) as a request line,
// optional headers, and a pretty-printed body. Values are already masked
// server-side, so this is purely a display transform.
function formatEnvelope(env: ActionEnvelope): string {
  const lines: string[] = [];
  const requestLine = [env.method, env.url].filter(Boolean).join(" ");
  if (requestLine) lines.push(requestLine);
  if (env.headers && Object.keys(env.headers).length > 0) {
    if (lines.length > 0) lines.push("");
    for (const [k, v] of Object.entries(env.headers)) {
      lines.push(`${k}: ${formatScalar(v)}`);
    }
  }
  if (env.data && Object.keys(env.data).length > 0) {
    if (lines.length > 0) lines.push("");
    lines.push(JSON.stringify(env.data, null, 2));
  }
  return lines.join("\n");
}

function formatScalar(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

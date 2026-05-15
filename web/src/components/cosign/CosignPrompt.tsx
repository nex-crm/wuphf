import { type ReactNode, useState } from "react";
import type {
  ApprovalClaim,
  ApprovalScope,
  SignedApprovalTokenJsonValue,
} from "@wuphf/protocol";

import {
  isWebAuthnApprovalPendingResponse,
  requestWebAuthnCosignChallenge,
  runWebAuthnAuthenticationCeremony,
  verifyWebAuthnCosign,
  type WebAuthnApprovalPendingResponse,
} from "../../api/webauthn";
import { ClaimSummary } from "./ClaimSummary";

interface CosignPromptProps {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly onAccepted?: (token: SignedApprovalTokenJsonValue) => void;
}

type CosignState =
  | { readonly kind: "idle" }
  | { readonly kind: "running" }
  | {
      readonly kind: "accepted";
      readonly token: SignedApprovalTokenJsonValue;
    }
  | {
      readonly kind: "pending";
      readonly pending: WebAuthnApprovalPendingResponse;
    }
  | { readonly kind: "error"; readonly message: string };

export function CosignPrompt({ claim, scope, onAccepted }: CosignPromptProps) {
  const [state, setState] = useState<CosignState>({ kind: "idle" });
  const running = state.kind === "running";

  const handleCosign = async () => {
    if (running) return;
    setState({ kind: "running" });
    try {
      const challenge = await requestWebAuthnCosignChallenge({ claim, scope });
      const assertionResponse = await runWebAuthnAuthenticationCeremony(
        challenge.requestOptions,
      );
      const response = await verifyWebAuthnCosign({
        challengeId: challenge.challengeId,
        assertionResponse,
      });
      if (isWebAuthnApprovalPendingResponse(response)) {
        setState({ kind: "pending", pending: response });
        return;
      }
      setState({ kind: "accepted", token: response });
      onAccepted?.(response);
    } catch (error) {
      setState({ kind: "error", message: describeCosignFailure(error) });
    }
  };

  return (
    <section
      aria-label="WebAuthn approval co-sign"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 14,
        maxWidth: 720,
      }}
    >
      <ClaimSummary claim={claim} scope={scope} />

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          flexWrap: "wrap",
        }}
      >
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={handleCosign}
          disabled={running}
        >
          {running ? "Waiting for security key..." : "Sign approval"}
        </button>
        <span style={{ fontSize: 12, color: "var(--text-secondary)" }}>
          Role: <code>{scope.role}</code>
        </span>
      </div>

      {state.kind === "accepted" ? (
        <OutcomePanel tone="success" title="Approval token issued">
          <div>
            Token ID: <code>{state.token.tokenId}</code>
          </div>
        </OutcomePanel>
      ) : null}

      {state.kind === "pending" ? (
        <PendingPanel pending={state.pending} />
      ) : null}

      {state.kind === "error" ? (
        <OutcomePanel tone="error" title="Approval not issued">
          {state.message}
        </OutcomePanel>
      ) : null}
    </section>
  );
}

function PendingPanel({
  pending,
}: {
  readonly pending: WebAuthnApprovalPendingResponse;
}) {
  const satisfied = pending.satisfiedRoles.length;
  const required = Math.max(1, pending.requiredThreshold);
  return (
    <OutcomePanel tone="pending" title="More roles required">
      <div style={{ display: "grid", gap: 8 }}>
        <div>
          {satisfied} of {pending.requiredThreshold} roles satisfied
        </div>
        <progress
          aria-label="Threshold progress"
          value={Math.min(satisfied, required)}
          max={required}
          style={{ width: "100%" }}
        />
        <div style={{ fontSize: 12 }}>
          Satisfied roles:{" "}
          {pending.satisfiedRoles.length > 0
            ? pending.satisfiedRoles.join(", ")
            : "none"}
        </div>
      </div>
    </OutcomePanel>
  );
}

function OutcomePanel({
  tone,
  title,
  children,
}: {
  readonly tone: "success" | "pending" | "error";
  readonly title: string;
  readonly children: ReactNode;
}) {
  const colors = {
    success: {
      background: "var(--green-bg)",
      border: "rgba(22, 163, 74, 0.35)",
      color: "var(--green)",
    },
    pending: {
      background: "var(--yellow-bg)",
      border: "rgba(217, 119, 6, 0.35)",
      color: "var(--text)",
    },
    error: {
      background: "rgba(220, 38, 38, 0.08)",
      border: "rgba(220, 38, 38, 0.35)",
      color: "var(--danger-500, #dc2626)",
    },
  }[tone];

  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      style={{
        border: `1px solid ${colors.border}`,
        borderRadius: 8,
        background: colors.background,
        color: colors.color,
        padding: 12,
        fontSize: 13,
        lineHeight: 1.45,
      }}
    >
      <strong style={{ display: "block", marginBottom: 4 }}>{title}</strong>
      {children}
    </div>
  );
}

export function describeCosignFailure(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (/expired|unknown|invalid|reused|challenge/i.test(message)) {
    return "The approval challenge expired or is no longer valid. Start the approval again.";
  }
  if (/wrong.?agent|issuedTo|audience|agent/i.test(message)) {
    return "The broker rejected this approval for the current agent. Refresh the pending request and try again.";
  }
  if (/credential|role|trusted|threshold/i.test(message)) {
    return "The credential did not satisfy the approval policy. Use a registered role or ask another trusted role to co-sign.";
  }
  if (/not.?allowed|cancel|abort/i.test(message)) {
    return "The security key ceremony was cancelled before a token was issued.";
  }
  return "The broker could not issue an approval token. Refresh the request and try again.";
}

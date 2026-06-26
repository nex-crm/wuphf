import { type ReactNode, useState } from "react";
import type { ApprovalRole } from "@wuphf/protocol";

import {
  APPROVAL_ROLE_VALUES,
  describeWebAuthnBrokerStorageError,
  isApprovalRole,
  isWebAuthnSupported,
  requestWebAuthnRegistrationChallenge,
  runWebAuthnRegistrationCeremony,
  verifyWebAuthnRegistration,
} from "../../api/webauthn";
import { showNotice } from "../ui/Toast";

const ROLE_OPTIONS: readonly ApprovalRole[] = APPROVAL_ROLE_VALUES;
const [DEFAULT_ROLE] = APPROVAL_ROLE_VALUES;

const WEBAUTHN_UNSUPPORTED_MESSAGE =
  "This environment doesn't support passkeys (WebAuthn), so a security key can't be registered here. Open WUPHF in a browser or on a platform with passkey support to bind an approval credential.";

type RegistrationState =
  | { readonly kind: "idle" }
  | { readonly kind: "running" }
  | {
      readonly kind: "registered";
      readonly credentialId: string;
      readonly role: ApprovalRole;
    }
  | { readonly kind: "error"; readonly message: string };

export function CredentialRegistrationPanel() {
  const [role, setRole] = useState<ApprovalRole>(DEFAULT_ROLE);
  const [state, setState] = useState<RegistrationState>({ kind: "idle" });
  // Resolve once at render: passkey ceremonies are unavailable on webviews that
  // ship no WebAuthn API (e.g. the Linux WebKitGTK desktop shell). Surface that
  // plainly instead of a "Register" button whose only outcome is an error.
  const supported = isWebAuthnSupported();
  const selectedRole = isApprovalRole(role) ? role : null;
  const running = state.kind === "running";
  const canRegister = supported && selectedRole !== null;

  const handleRegister = async () => {
    if (running) return;
    if (!supported) {
      setState({
        kind: "error",
        message: WEBAUTHN_UNSUPPORTED_MESSAGE,
      });
      return;
    }
    if (selectedRole === null) {
      setState({ kind: "error", message: "Choose a role before registering." });
      return;
    }
    setState({ kind: "running" });
    try {
      const challenge = await requestWebAuthnRegistrationChallenge({
        role: selectedRole,
      });
      const attestationResponse = await runWebAuthnRegistrationCeremony(
        challenge.creationOptions,
      );
      const response = await verifyWebAuthnRegistration({
        challengeId: challenge.challengeId,
        attestationResponse,
      });
      setState({
        kind: "registered",
        credentialId: response.credentialId,
        role: response.role,
      });
      showNotice(`Security key registered for ${response.role}`, "success");
    } catch (error) {
      setState({
        kind: "error",
        message: describeRegistrationFailure(error),
      });
    }
  };

  return (
    <section
      aria-label="Security key registration"
      style={{
        border: "1px solid var(--border)",
        borderRadius: 8,
        background: "var(--bg-card)",
        padding: 16,
        display: "grid",
        gap: 14,
      }}
    >
      <div>
        <h3 style={{ fontSize: 15, fontWeight: 700, marginBottom: 4 }}>
          Security key registration
        </h3>
        <p
          style={{
            color: "var(--text-secondary)",
            fontSize: 12,
            lineHeight: 1.45,
            margin: 0,
          }}
        >
          Bind this browser's WebAuthn credential to an approval role.
        </p>
      </div>

      {supported ? (
        <>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(150px, 0.35fr) minmax(0, 1fr)",
              gap: 12,
              alignItems: "center",
            }}
          >
            <label
              htmlFor="webauthn-role"
              style={{
                color: "var(--text-secondary)",
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              Approval role
            </label>
            <select
              id="webauthn-role"
              value={role}
              onChange={(event) => {
                if (isApprovalRole(event.target.value)) {
                  setRole(event.target.value);
                }
              }}
              style={inputStyle}
            >
              {ROLE_OPTIONS.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
          </div>

          <div>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={handleRegister}
              disabled={running || !canRegister}
            >
              {running
                ? "Waiting for security key..."
                : "Register security key"}
            </button>
          </div>
        </>
      ) : (
        <p
          role="status"
          style={{
            color: "var(--text-secondary)",
            fontSize: 12,
            lineHeight: 1.45,
            margin: 0,
          }}
        >
          {WEBAUTHN_UNSUPPORTED_MESSAGE}
        </p>
      )}

      {state.kind === "registered" ? (
        <RegistrationOutcome tone="success" title="Credential registered">
          <div>
            Role: <code>{state.role}</code>
          </div>
          <div>
            Credential ID: <code>{state.credentialId}</code>
          </div>
        </RegistrationOutcome>
      ) : null}

      {state.kind === "error" ? (
        <RegistrationOutcome tone="error" title="Registration failed">
          {state.message}
        </RegistrationOutcome>
      ) : null}
    </section>
  );
}

const inputStyle = {
  background: "var(--bg-card)",
  border: "1px solid var(--border)",
  color: "var(--text)",
  borderRadius: "var(--radius-sm)",
  height: 36,
  fontSize: 13,
  padding: "0 10px",
  outline: "none",
  width: "100%",
  fontFamily: "var(--font-sans)",
} as const;

function RegistrationOutcome({
  tone,
  title,
  children,
}: {
  readonly tone: "success" | "error";
  readonly title: string;
  readonly children: ReactNode;
}) {
  const colors =
    tone === "success"
      ? {
          background: "var(--green-bg)",
          border: "rgba(22, 163, 74, 0.35)",
          color: "var(--green)",
        }
      : {
          background: "rgba(220, 38, 38, 0.08)",
          border: "rgba(220, 38, 38, 0.35)",
          color: "var(--danger-500, #dc2626)",
        };
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

export function describeRegistrationFailure(error: unknown): string {
  const storageMessage = describeWebAuthnBrokerStorageError(error);
  if (storageMessage !== null) return storageMessage;

  const message = error instanceof Error ? error.message : String(error);
  if (/expired|unknown|invalid|reused|challenge/i.test(message)) {
    return "The registration challenge expired or is no longer valid. Start registration again.";
  }
  if (/not.?allowed|cancel|abort/i.test(message)) {
    return "The security key ceremony was cancelled before a credential was registered.";
  }
  if (/role|policy|credential/i.test(message)) {
    return "The broker rejected this role or credential. Choose a trusted role and try again.";
  }
  return "The broker could not register this security key. Try again from this settings screen.";
}

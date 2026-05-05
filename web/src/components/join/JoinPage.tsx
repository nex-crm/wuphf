import { type FormEvent, useEffect, useId, useRef, useState } from "react";

import {
  type JoinInviteErrorCode,
  submitJoinInvite,
} from "../../api/joinInvite";
import "./join.css";

type ErrorCode = JoinInviteErrorCode | "name_required";

interface JoinPageProps {
  token: string;
  // Test-only seam: replaces the production window.location.assign call.
  onAccepted?: (redirectTo: string) => void;
}

type Status =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; code: ErrorCode; message: string };

export function JoinPage({ token, onAccepted }: JoinPageProps) {
  const nameId = useId();
  const errorId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [displayName, setDisplayName] = useState("");
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const trimmedToken = token.trim();
  if (!trimmedToken) {
    return (
      <JoinShell>
        <p className="join-eyebrow">Team member invite</p>
        <h1 className="join-heading">Invite link is missing its token</h1>
        <p className="join-copy">
          Ask the host to send you a fresh invite link from Access &amp; Health.
        </p>
      </JoinShell>
    );
  }

  const submitting = status.kind === "submitting";
  const errorMessage = status.kind === "error" ? status.message : null;
  const errorCode = status.kind === "error" ? status.code : null;

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const trimmed = displayName.trim();
    if (!trimmed) {
      setStatus({
        kind: "error",
        code: "name_required",
        message: "Add a display name so the team knows who is joining.",
      });
      return;
    }
    setStatus({ kind: "submitting" });
    const result = await submitJoinInvite({
      token: trimmedToken,
      displayName: trimmed,
    });
    if (result.ok) {
      if (onAccepted) {
        onAccepted(result.redirect);
        return;
      }
      window.location.assign(result.redirect);
      return;
    }
    setStatus({ kind: "error", code: result.code, message: result.message });
  }

  return (
    <JoinShell>
      <p className="join-eyebrow">Team member invite</p>
      <h1 className="join-heading">Join this WUPHF office</h1>
      <p className="join-copy">
        Pick the name your teammate should see in messages, requests, and office
        activity. WUPHF will not share the host's broker token with this browser
        session.
      </p>

      {errorMessage ? (
        <div
          className="join-error"
          id={errorId}
          role="alert"
          data-error-code={errorCode ?? undefined}
        >
          {errorMessage}
        </div>
      ) : null}

      <form className="join-form" onSubmit={handleSubmit} noValidate={true}>
        <label htmlFor={nameId} className="join-label">
          Display name
        </label>
        <input
          ref={inputRef}
          id={nameId}
          name="display_name"
          autoComplete="name"
          placeholder="e.g. Maya"
          className="join-input"
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          disabled={submitting}
          aria-invalid={errorCode === "name_required"}
          aria-describedby={errorMessage ? errorId : undefined}
        />
        <button
          type="submit"
          className="join-submit"
          disabled={submitting}
          aria-busy={submitting}
        >
          {submitting ? "Entering…" : "Enter office"}
        </button>
      </form>

      <p className="join-note">
        This creates a scoped team-member browser session. Sessions show up
        under Access &amp; Health on the host machine and can be revoked at any
        time.
      </p>
    </JoinShell>
  );
}

function JoinShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="join-page">
      <main className="join-card">{children}</main>
    </div>
  );
}

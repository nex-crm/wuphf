import {
  type FormEvent,
  type ReactNode,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";

import {
  type JoinInviteErrorCode,
  submitJoinInvite,
} from "../../api/joinInvite";
import "./join.css";

type ErrorCode = JoinInviteErrorCode | "name_required" | "passcode_missing";

// callSubmitJoinInvite wraps the never-rejecting submitJoinInvite so the
// (defensive) try/catch lives outside JoinPage.handleSubmit and the latter
// stays under biome's per-function complexity ceiling. Returns null when
// the catch ran — the caller has already stamped the status.
async function callSubmitJoinInvite(
  token: string,
  displayName: string,
  passcode: string,
  setStatus: (status: {
    kind: "error";
    code: ErrorCode;
    message: string;
  }) => void,
): Promise<Awaited<ReturnType<typeof submitJoinInvite>> | null> {
  try {
    return await submitJoinInvite({
      token,
      displayName,
      passcode: passcode || undefined,
    });
  } catch (err) {
    // submitJoinInvite is contractually never-rejecting, so reaching this
    // branch means a programmer error or a future refactor regression.
    // Treat it as a generic network failure so the joiner can retry.
    const message =
      err instanceof Error && err.message
        ? `Could not reach WUPHF: ${err.message}`
        : "Something went wrong submitting the invite. Try again.";
    setStatus({ kind: "error", code: "network", message });
    return null;
  }
}

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
  const passcodeId = useId();
  const errorId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const passcodeInputRef = useRef<HTMLInputElement>(null);
  const [displayName, setDisplayName] = useState("");
  const [passcode, setPasscode] = useState("");
  // passcodeRequired flips on the first time the server returns
  // passcode_required and stays on for the rest of the session — the host
  // doesn't switch a tunnel from "passcode-needed" to "no passcode" without
  // a tunnel restart, and re-hiding the field would lose what the joiner
  // already typed.
  const [passcodeRequired, setPasscodeRequired] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    if (passcodeRequired) {
      passcodeInputRef.current?.focus();
    }
  }, [passcodeRequired]);

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

  function validateLocally():
    | { ok: true; trimmed: string; trimmedPasscode: string }
    | { ok: false } {
    const trimmed = displayName.trim();
    if (!trimmed) {
      setStatus({
        kind: "error",
        code: "name_required",
        message: "Add a display name so the team knows who is joining.",
      });
      return { ok: false };
    }
    const trimmedPasscode = passcode.trim();
    if (passcodeRequired && !trimmedPasscode) {
      setStatus({
        kind: "error",
        code: "passcode_missing",
        message: "Enter the passcode your host gave you.",
      });
      return { ok: false };
    }
    return { ok: true, trimmed, trimmedPasscode };
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const validated = validateLocally();
    if (!validated.ok) return;
    setStatus({ kind: "submitting" });
    const result = await callSubmitJoinInvite(
      trimmedToken,
      validated.trimmed,
      validated.trimmedPasscode,
      setStatus,
    );
    if (!result) return;
    if (result.ok) {
      if (onAccepted) {
        onAccepted(result.redirect);
        return;
      }
      window.location.assign(result.redirect);
      return;
    }
    // First refusal: surface the passcode field. Clear the stored
    // value (it was empty anyway since the field was hidden, but
    // explicit zero-state is harmless here). On *subsequent*
    // refusals we leave whatever the joiner typed alone — wiping it
    // forces them to retype six digits to fix a single-digit typo,
    // which is hostile UX and the security gate already rate-limits
    // attempts via the share-handler token bucket.
    if (result.code === "passcode_required" && !passcodeRequired) {
      setPasscodeRequired(true);
      setPasscode("");
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
          // Only attach aria-describedby when the error actually targets
          // THIS field. With both name + passcode visible, a generic
          // "errorMessage ? errorId : undefined" makes a screen reader
          // announce passcode-targeted errors while the user is on the
          // name field (and vice-versa).
          aria-describedby={
            errorMessage && errorCode === "name_required" ? errorId : undefined
          }
        />
        {passcodeRequired ? (
          <>
            <label htmlFor={passcodeId} className="join-label">
              Passcode
            </label>
            <input
              ref={passcodeInputRef}
              id={passcodeId}
              name="passcode"
              // inputMode=numeric brings up the digit pad on phones; we
              // keep type=text (the default) so password managers don't
              // try to memo it as a normal credential.
              // autoComplete="one-time-code" is the WHATWG token for SMS
              // OTP autofill on iOS Safari and Android Chrome — if the
              // host reads the passcode out over the phone the joiner
              // taps once instead of typing it. The convenience win
              // outweighs the autofill risk because the OTP-token hint
              // is scoped to a single submission, not stored.
              inputMode="numeric"
              pattern="[0-9]*"
              autoComplete="one-time-code"
              placeholder="6-digit code from the host"
              className="join-input"
              value={passcode}
              onChange={(event) =>
                // Clamp to the server's 6-digit shape: strip non-digits
                // first, then slice. A pasted longer numeric string would
                // otherwise pad maxLength up to 6 but a chained
                // event.target.value of "1234567890" with non-digits
                // mixed in could exceed it on browsers that don't
                // honor maxLength for programmatic value writes.
                setPasscode(event.target.value.replace(/\D/g, "").slice(0, 6))
              }
              disabled={submitting}
              aria-invalid={
                errorCode === "passcode_required" ||
                errorCode === "passcode_missing"
              }
              // Same per-field rule as the name input — only describe
              // the passcode field when the live error targets it.
              aria-describedby={
                errorMessage &&
                (errorCode === "passcode_required" ||
                  errorCode === "passcode_missing")
                  ? errorId
                  : undefined
              }
              maxLength={6}
            />
          </>
        ) : null}
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

function JoinShell({ children }: { children: ReactNode }) {
  return (
    <div className="join-page">
      <main className="join-card">{children}</main>
    </div>
  );
}

// The joiner has no broker token or session cookie yet, so requests cannot
// flow through the authenticated `/api/*` share proxy — they must hit the
// share handler's `/join/<token>` endpoint directly.

export type JoinInviteErrorCode =
  | "invite_expired_or_used"
  | "invite_invalid"
  | "broker_unreachable"
  | "broker_failed"
  | "invalid_request"
  // Phase 2 hardening: tunnel-mode invites require a second-factor passcode
  // the host reads out-of-band. The server returns this code on both
  // missing and wrong passcodes (deliberately indistinguishable on the
  // wire — see cmd/wuphf/share_join_guard.go).
  | "passcode_required"
  // Phase 2 hardening: the share server clamps brute-force attempts at the
  // unauthenticated /join POST. The JoinPage renders this distinctly so the
  // joiner knows to wait, not retry immediately.
  | "rate_limited"
  | "network"
  | "unknown";

// Keep this set in sync with cmd/wuphf/share.go writeShareJoinError. Codes
// not present here collapse to "unknown" on the client.
const SERVER_ERROR_CODES: ReadonlySet<JoinInviteErrorCode> = new Set([
  "invite_expired_or_used",
  "invite_invalid",
  "broker_unreachable",
  "broker_failed",
  "invalid_request",
  "passcode_required",
  "rate_limited",
]);

export interface JoinInviteSuccess {
  ok: true;
  redirect: string;
  display_name: string;
}

export interface JoinInviteFailure {
  ok: false;
  code: JoinInviteErrorCode;
  message: string;
}

export type JoinInviteResult = JoinInviteSuccess | JoinInviteFailure;

interface SubmitOptions {
  token: string;
  displayName: string;
  // Optional second-factor passcode for tunnel-mode invites. Empty/undefined
  // for network-share invites and on the first submit when the joiner has
  // not yet been told a passcode is required — the server will respond with
  // `passcode_required` and the JoinPage will surface a passcode field.
  passcode?: string;
  signal?: AbortSignal;
}

export async function submitJoinInvite({
  token,
  displayName,
  passcode,
  signal,
}: SubmitOptions): Promise<JoinInviteResult> {
  // Trim any whitespace pasted in by the joiner so a stray space at the end
  // of the digits does not collide with the constant-time compare on the
  // server side.
  const trimmedPasscode =
    typeof passcode === "string" ? passcode.trim() : undefined;
  const body: Record<string, string> = { display_name: displayName };
  if (trimmedPasscode) body.passcode = trimmedPasscode;
  let response: Response;
  try {
    response = await fetch(`/join/${encodeURIComponent(token)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      credentials: "same-origin",
      signal,
    });
  } catch (err) {
    if (signal?.aborted) {
      return { ok: false, code: "network", message: "Cancelled." };
    }
    return {
      ok: false,
      code: "network",
      message:
        err instanceof Error && err.message
          ? `Could not reach WUPHF: ${err.message}`
          : "Could not reach WUPHF. Check your network and try again.",
    };
  }

  return interpretJoinResponse(response);
}

async function interpretJoinResponse(
  response: Response,
): Promise<JoinInviteResult> {
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }

  const record =
    body && typeof body === "object" ? (body as Record<string, unknown>) : null;

  if (
    response.ok &&
    record?.ok === true &&
    typeof record.redirect === "string" &&
    typeof record.display_name === "string"
  ) {
    return {
      ok: true,
      redirect: record.redirect,
      display_name: record.display_name,
    };
  }

  // The broker may have already set a session cookie before the response
  // body got mangled. Tell the joiner to reload rather than show "unknown".
  if (response.ok && body === null) {
    return {
      ok: false,
      code: "unknown",
      message:
        "WUPHF accepted the invite but the response was unreadable. Reload — you may already be in.",
    };
  }

  if (
    record &&
    typeof record.error === "string" &&
    typeof record.message === "string"
  ) {
    const code: JoinInviteErrorCode = SERVER_ERROR_CODES.has(
      record.error as JoinInviteErrorCode,
    )
      ? (record.error as JoinInviteErrorCode)
      : "unknown";
    return { ok: false, code, message: record.message };
  }

  return {
    ok: false,
    code: "unknown",
    message: `WUPHF returned an unexpected response (${response.status}). Ask the host for a new invite.`,
  };
}

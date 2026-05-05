// The joiner has no broker token or session cookie yet, so requests cannot
// flow through the authenticated `/api/*` share proxy — they must hit the
// share handler's `/join/<token>` endpoint directly.

export type JoinInviteErrorCode =
  | "invite_expired_or_used"
  | "invite_invalid"
  | "broker_unreachable"
  | "broker_failed"
  | "invalid_request"
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
  signal?: AbortSignal;
}

export async function submitJoinInvite({
  token,
  displayName,
  signal,
}: SubmitOptions): Promise<JoinInviteResult> {
  let response: Response;
  try {
    response = await fetch(`/join/${encodeURIComponent(token)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ display_name: displayName }),
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

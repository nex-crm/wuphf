// Joiner-side invite acceptance. The host machine's share handler accepts
// the JSON body, exchanges it for a human session cookie, and returns
// either {ok, redirect, display_name} or {error, message}.
//
// This module deliberately does not use the regular `/api/*` proxy: the
// joiner has no broker token and no session cookie yet, so the request
// must hit the share handler's `/join/<token>` endpoint directly.

export type JoinInviteErrorCode =
  | "invite_expired_or_used"
  | "invite_invalid"
  | "broker_unreachable"
  | "broker_failed"
  | "invalid_request"
  | "network"
  | "unknown";

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
      return {
        ok: false,
        code: "network",
        message: "Cancelled.",
      };
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

  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = null;
  }

  if (response.ok && isSuccessBody(body)) {
    return {
      ok: true,
      redirect: body.redirect,
      display_name: body.display_name,
    };
  }

  if (isErrorBody(body)) {
    return {
      ok: false,
      code: normalizeErrorCode(body.error),
      message: body.message,
    };
  }

  return {
    ok: false,
    code: "unknown",
    message: `WUPHF returned an unexpected response (${response.status}). Ask the host for a new invite.`,
  };
}

function isSuccessBody(
  body: unknown,
): body is { ok: true; redirect: string; display_name: string } {
  if (!body || typeof body !== "object") return false;
  const record = body as Record<string, unknown>;
  return (
    record.ok === true &&
    typeof record.redirect === "string" &&
    typeof record.display_name === "string"
  );
}

function isErrorBody(
  body: unknown,
): body is { error: string; message: string } {
  if (!body || typeof body !== "object") return false;
  const record = body as Record<string, unknown>;
  return typeof record.error === "string" && typeof record.message === "string";
}

function normalizeErrorCode(raw: string): JoinInviteErrorCode {
  switch (raw) {
    case "invite_expired_or_used":
    case "invite_invalid":
    case "broker_unreachable":
    case "broker_failed":
    case "invalid_request":
      return raw;
    default:
      return "unknown";
  }
}

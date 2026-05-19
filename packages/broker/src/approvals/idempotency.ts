// Command idempotency for approval writes.
//
// Direct appender callers may use the same structural shape as the cost ledger:
//
//   cmd_<command>_<26-char-ULID>
//
// HTTP routes use the protocol route-envelope `idempotencyKey` body field
// instead. The route layer prefixes it with the command before passing it to
// the appender so a key minted for approval.requested cannot replay an
// approval.decided response.

export type ApprovalCommand = "approval.requested" | "approval.decided";

export const DEFAULT_APPROVAL_IDEMPOTENCY_TTL_MS = 24 * 60 * 60 * 1000;

export const APPROVAL_COMMAND_VALUES: readonly ApprovalCommand[] = [
  "approval.requested",
  "approval.decided",
];

const APPROVAL_COMMAND_SET: ReadonlySet<string> = new Set<string>(APPROVAL_COMMAND_VALUES);
const KEY_RE = /^cmd_([a-z][a-z0-9.]*[a-z0-9])_([0-9A-HJKMNP-TV-Z]{26})$/;

export interface ParsedApprovalIdempotencyKey {
  readonly raw: string;
  readonly command: ApprovalCommand;
  readonly ulid: string;
}

export type ApprovalIdempotencyParseError =
  | { readonly code: "missing" }
  | { readonly code: "malformed"; readonly reason: string }
  | { readonly code: "unknown_command"; readonly command: string }
  | {
      readonly code: "command_mismatch";
      readonly expected: ApprovalCommand;
      readonly actual: string;
    };

export type ApprovalIdempotencyParseResult =
  | { readonly ok: true; readonly key: ParsedApprovalIdempotencyKey }
  | { readonly ok: false; readonly error: ApprovalIdempotencyParseError };

export function parseApprovalIdempotencyKey(
  raw: string | undefined,
  expectedCommand: ApprovalCommand,
): ApprovalIdempotencyParseResult {
  if (raw === undefined || raw.length === 0) {
    return { ok: false, error: { code: "missing" } };
  }
  const match = KEY_RE.exec(raw);
  if (match === null) {
    return {
      ok: false,
      error: { code: "malformed", reason: "must match cmd_<command>_<26-char-ULID>" },
    };
  }
  const command = match[1] ?? "";
  const ulid = match[2] ?? "";
  if (!APPROVAL_COMMAND_SET.has(command)) {
    return { ok: false, error: { code: "unknown_command", command } };
  }
  if (command !== expectedCommand) {
    return {
      ok: false,
      error: { code: "command_mismatch", expected: expectedCommand, actual: command },
    };
  }
  return { ok: true, key: { raw, command: command as ApprovalCommand, ulid } };
}

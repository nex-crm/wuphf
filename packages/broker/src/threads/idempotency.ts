export type ThreadCommand = "thread.create" | "thread.spec.edit" | "thread.status.change";

export const THREAD_COMMAND_VALUES: readonly ThreadCommand[] = [
  "thread.create",
  "thread.spec.edit",
  "thread.status.change",
];

const THREAD_COMMAND_SET: ReadonlySet<string> = new Set<string>(THREAD_COMMAND_VALUES);
const KEY_RE = /^cmd_([a-z][a-z0-9.]*[a-z0-9])_([0-9A-HJKMNP-TV-Z]{26})$/;

export interface ParsedIdempotencyKey {
  readonly raw: string;
  readonly command: ThreadCommand;
  readonly ulid: string;
}

export type IdempotencyParseError =
  | { readonly code: "missing" }
  | { readonly code: "malformed"; readonly reason: string }
  | { readonly code: "unknown_command"; readonly command: string }
  | {
      readonly code: "command_mismatch";
      readonly expected: ThreadCommand;
      readonly actual: string;
    };

export type IdempotencyParseResult =
  | { readonly ok: true; readonly key: ParsedIdempotencyKey }
  | { readonly ok: false; readonly error: IdempotencyParseError };

export function parseThreadIdempotencyKey(
  raw: string | undefined,
  expectedCommand: ThreadCommand,
): IdempotencyParseResult {
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
  if (!THREAD_COMMAND_SET.has(command)) {
    return { ok: false, error: { code: "unknown_command", command } };
  }
  if (command !== expectedCommand) {
    return {
      ok: false,
      error: { code: "command_mismatch", expected: expectedCommand, actual: command },
    };
  }
  return { ok: true, key: { raw, command: command as ThreadCommand, ulid } };
}

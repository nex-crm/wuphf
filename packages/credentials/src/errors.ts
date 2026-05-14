export type CredentialErrorCode =
  | "adapter_not_supported"
  | "basic_text_rejected"
  | "broker_identity_required"
  | "invalid_credential_payload"
  | "invalid_handle"
  | "keychain_command_failed"
  | "keychain_command_timed_out"
  | "no_keyring_available"
  | "not_found"
  | "ownership_mismatch";

export interface CredentialErrorOptions extends ErrorOptions {
  readonly recoveryHint?: string | undefined;
}

export class CredentialStoreError extends Error {
  code: CredentialErrorCode;
  readonly recoveryHint?: string | undefined;

  constructor(code: CredentialErrorCode, message: string, options?: CredentialErrorOptions) {
    super(message, options);
    this.code = code;
    this.name = new.target.name;
    this.recoveryHint = options?.recoveryHint;
  }
}

export class AdapterNotSupported extends CredentialStoreError {
  constructor(platform: string) {
    super("adapter_not_supported", `credential adapter is not supported on platform "${platform}"`);
  }
}

export class BasicTextRejected extends CredentialStoreError {
  constructor() {
    super("basic_text_rejected", "refusing to use an unencrypted libsecret basic_text collection");
  }
}

export class BrokerIdentityRequired extends CredentialStoreError {
  constructor() {
    super("broker_identity_required", "credential store access requires a broker identity");
  }
}

export class InvalidHandle extends CredentialStoreError {
  constructor() {
    super("invalid_handle", "credential handle is invalid");
  }
}

export class InvalidCredentialPayload extends CredentialStoreError {
  constructor() {
    super(
      "invalid_credential_payload",
      "credential secret must be valid UTF-8 text without NUL bytes",
    );
  }
}

export class CredentialOwnershipMismatch extends CredentialStoreError {
  constructor() {
    super("ownership_mismatch", "credential handle ownership does not match requested agent scope");
  }
}

export interface KeychainCommandFailureOptions extends CredentialErrorOptions {
  readonly killed?: boolean | undefined;
  readonly signal?: NodeJS.Signals | string | undefined;
  readonly stderrSnippet?: string | undefined;
  readonly systemCode?: string | undefined;
}

export class KeychainCommandFailed extends CredentialStoreError {
  readonly killed?: boolean | undefined;
  readonly signal?: NodeJS.Signals | string | undefined;
  readonly stderrSnippet: string;
  readonly systemCode?: string | undefined;

  constructor(
    readonly command: string,
    readonly exitCode: number,
    stderr: string,
    options?: KeychainCommandFailureOptions,
  ) {
    const stderrSnippet = options?.stderrSnippet ?? sanitizeCommandText(stderr);
    super(
      "keychain_command_failed",
      `${command} failed with exit code ${exitCode}: ${stderrSnippet}`,
      options,
    );
    this.killed = options?.killed;
    this.signal = options?.signal;
    this.stderrSnippet = stderrSnippet;
    this.systemCode = options?.systemCode;
  }
}

export class KeychainCommandTimedOut extends KeychainCommandFailed {
  constructor(
    command: string,
    readonly timeoutMs: number,
    readonly platform: NodeJS.Platform,
    readonly action: string,
    options?: KeychainCommandFailureOptions,
  ) {
    super(command, 124, "", options);
    this.code = "keychain_command_timed_out";
    this.message = `${command} timed out after ${timeoutMs}ms during ${action} on ${platform}`;
  }
}

export class NoKeyringAvailable extends CredentialStoreError {
  constructor(
    detail = "OS keyring command is unavailable or not initialized",
    options?: CredentialErrorOptions,
  ) {
    super("no_keyring_available", detail, options);
  }
}

export class NotFound extends CredentialStoreError {
  constructor() {
    super("not_found", "credential not found");
  }
}

const ANSI_ESCAPE_PATTERN = `${String.fromCharCode(0x1b)}\\[[0-?]*[ -/]*[@-~]`;
const CONTROL_BYTES_PATTERN = "[\\u0000-\\u001f\\u007f-\\u009f]";
const ANSI_ESCAPE_RE = new RegExp(ANSI_ESCAPE_PATTERN, "g");
const CONTROL_BYTES_RE = new RegExp(CONTROL_BYTES_PATTERN, "g");

function sanitizeCommandText(value: string): string {
  const compact = value
    .replace(ANSI_ESCAPE_RE, "")
    .replace(CONTROL_BYTES_RE, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (compact.length === 0) return "no stderr";
  return compact.slice(0, 200);
}

export type CredentialErrorCode =
  | "adapter_not_supported"
  | "basic_text_rejected"
  | "broker_identity_required"
  | "invalid_handle"
  | "keychain_command_failed"
  | "no_keyring_available"
  | "not_found";

export class CredentialStoreError extends Error {
  constructor(
    readonly code: CredentialErrorCode,
    message: string,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = new.target.name;
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

export class KeychainCommandFailed extends CredentialStoreError {
  constructor(
    readonly command: string,
    readonly exitCode: number,
    stderr: string,
    options?: ErrorOptions,
  ) {
    super(
      "keychain_command_failed",
      `${command} failed with exit code ${exitCode}: ${sanitizeCommandText(stderr)}`,
      options,
    );
  }
}

export class NoKeyringAvailable extends CredentialStoreError {
  constructor(detail = "OS keyring command is unavailable or not initialized") {
    super("no_keyring_available", detail);
  }
}

export class NotFound extends CredentialStoreError {
  constructor() {
    super("not_found", "credential not found");
  }
}

function sanitizeCommandText(value: string): string {
  const compact = value.replace(/\s+/g, " ").trim();
  if (compact.length === 0) return "no stderr";
  return compact.slice(0, 200);
}

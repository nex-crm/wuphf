export {
  AdapterNotSupported,
  BasicTextRejected,
  type CredentialErrorCode,
  CredentialStoreError,
  InvalidCredentialPayload,
  InvalidHandle,
  KeychainCommandFailed,
  KeychainCommandTimedOut,
  NoKeyringAvailable,
  NotFound,
} from "./errors.ts";
export {
  type CredentialHandleParts,
  credentialAccount,
  credentialHandleFromParts,
  credentialLabel,
  DEFAULT_CREDENTIAL_SERVICE,
  newCredentialHandle,
} from "./handle.ts";
export {
  type CredentialStore,
  type CredentialStoreOptions,
  type CredentialWriteRequest,
  execFileSpawner,
  open,
  openCredentialStore,
  type Spawner,
  type SpawnOptions,
  type SpawnResult,
} from "./store.ts";

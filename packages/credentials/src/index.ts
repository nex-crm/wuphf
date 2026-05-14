export type { BrokerIdentity } from "@wuphf/protocol";
export {
  AdapterNotSupported,
  BasicTextRejected,
  BrokerIdentityRequired,
  type CredentialErrorCode,
  CredentialOwnershipMismatch,
  CredentialStoreError,
  InvalidCredentialPayload,
  InvalidHandle,
  KeychainCommandFailed,
  KeychainCommandTimedOut,
  NoKeyringAvailable,
  NotFound,
} from "./errors.ts";
export type {
  CredentialReadWithOwnershipRequest,
  CredentialReadWithOwnershipResult,
  CredentialStore,
} from "./store.ts";
export { open } from "./store.ts";

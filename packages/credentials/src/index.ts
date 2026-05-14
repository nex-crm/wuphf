export type { BrokerIdentity } from "@wuphf/protocol";
export {
  AdapterNotSupported,
  BasicTextRejected,
  BrokerIdentityRequired,
  type CredentialErrorCode,
  CredentialStoreError,
  InvalidCredentialPayload,
  InvalidHandle,
  KeychainCommandFailed,
  KeychainCommandTimedOut,
  NoKeyringAvailable,
  NotFound,
} from "./errors.ts";
export type { CredentialStore } from "./store.ts";
export { open } from "./store.ts";

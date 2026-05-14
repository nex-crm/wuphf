export type { BrokerIdentity } from "@wuphf/protocol";
export {
  AdapterNotSupported,
  BasicTextRejected,
  BrokerIdentityRequired,
  type CredentialErrorCode,
  CredentialStoreError,
  InvalidHandle,
  KeychainCommandFailed,
  NoKeyringAvailable,
  NotFound,
} from "./errors.ts";
export type { CredentialStore } from "./store.ts";
export { open } from "./store.ts";

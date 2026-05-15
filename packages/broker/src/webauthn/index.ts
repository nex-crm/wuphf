export { createSimpleWebAuthnCeremony } from "./ceremony.ts";
export type { SqliteWebAuthnStoreConfig } from "./store.ts";
export { createWebAuthnStore, SqliteWebAuthnStore } from "./store.ts";
export type {
  Clock,
  ConsumedWebAuthnTokenRecord,
  RegisteredWebAuthnCredential,
  WebAuthnCeremony,
  WebAuthnRouteDeps,
  WebAuthnStore,
} from "./types.ts";
export {
  WEBAUTHN_ALLOWED_ORIGINS,
  WEBAUTHN_CHALLENGE_TTL_MS,
  WEBAUTHN_RP_ID,
  WEBAUTHN_RP_NAME,
  WEBAUTHN_TRUSTED_APPROVAL_ROLES,
} from "./types.ts";

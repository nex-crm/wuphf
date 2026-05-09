import type { ApprovalClaims, SignedApprovalToken } from "./receipt-types.ts";

const APPROVAL_CLAIMS_KEYS_TUPLE = [
  "signerIdentity",
  "role",
  "receiptId",
  "writeId",
  "frozenArgsHash",
  "riskClass",
  "issuedAt",
  "expiresAt",
  "webauthnAssertion",
] as const satisfies readonly (keyof ApprovalClaims)[];
export const APPROVAL_CLAIMS_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_CLAIMS_KEYS_TUPLE,
);

const SIGNED_APPROVAL_TOKEN_KEYS_TUPLE = [
  "claims",
  "algorithm",
  "signerKeyId",
  "signature",
] as const satisfies readonly (keyof SignedApprovalToken)[];
export const SIGNED_APPROVAL_TOKEN_KEYS: ReadonlySet<string> = new Set<string>(
  SIGNED_APPROVAL_TOKEN_KEYS_TUPLE,
);

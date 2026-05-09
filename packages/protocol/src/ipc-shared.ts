import {
  MAX_APPROVAL_SIGNATURE_BYTES,
  MAX_WEBAUTHN_ASSERTION_BYTES,
  validateApprovalTokenLifetimeValues,
} from "./budgets.ts";
import type { ApprovalClaims, SignedApprovalToken } from "./receipt-types.ts";

export type ApprovalTokenCheckResult = { ok: true } | { ok: false; reason: string };

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

export function validateApprovalSignatureBudget(value: unknown): ApprovalTokenCheckResult {
  if (typeof value !== "string") return { ok: true };
  if (utf8ByteLengthUpTo(value, MAX_APPROVAL_SIGNATURE_BYTES) > MAX_APPROVAL_SIGNATURE_BYTES) {
    return { ok: false, reason: "exceeds MAX_APPROVAL_SIGNATURE_BYTES" };
  }
  return { ok: true };
}

export function validateApprovalWebauthnAssertionBudget(value: unknown): ApprovalTokenCheckResult {
  if (typeof value !== "string") return { ok: true };
  if (utf8ByteLengthUpTo(value, MAX_WEBAUTHN_ASSERTION_BYTES) > MAX_WEBAUTHN_ASSERTION_BYTES) {
    return { ok: false, reason: "exceeds MAX_WEBAUTHN_ASSERTION_BYTES" };
  }
  return { ok: true };
}

export function validateApprovalClaimsLifetimeBudget(value: unknown): ApprovalTokenCheckResult {
  const issuedAt = objectProperty(value, "issuedAt");
  const expiresAt = objectProperty(value, "expiresAt");
  if (!(issuedAt instanceof Date) || !(expiresAt instanceof Date)) return { ok: true };
  if (Number.isNaN(issuedAt.getTime()) || Number.isNaN(expiresAt.getTime())) return { ok: true };

  const lifetime = validateApprovalTokenLifetimeValues(issuedAt, expiresAt);
  if (!lifetime.ok) {
    return {
      ok: false,
      reason: `exceeds MAX_APPROVAL_TOKEN_LIFETIME_MS: ${lifetime.reason}`,
    };
  }
  return { ok: true };
}

function objectProperty(value: unknown, key: string): unknown | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function utf8ByteLengthUpTo(value: string, budget: number): number {
  if (value.length > budget) return budget + 1;

  let bytes = 0;
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x7f) {
      bytes += 1;
    } else if (code <= 0x7ff) {
      bytes += 2;
    } else if (code >= 0xd800 && code <= 0xdbff && i + 1 < value.length) {
      const next = value.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        bytes += 4;
        i += 1;
      } else {
        bytes += 3;
      }
    } else {
      bytes += 3;
    }

    if (bytes > budget) return budget + 1;
  }

  return bytes;
}

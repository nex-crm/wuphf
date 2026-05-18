import {
  type ApprovalEvent,
  approvalClaimToJsonValue,
  approvalScopeToJsonValue,
  type BrokerTokenVerdict,
  canonicalJSON,
  type ReceiptSnapshot,
  type SignedApprovalToken,
  sha256Hex,
} from "@wuphf/protocol";

import type { ConsumedWebAuthnTokenRecord, WebAuthnStore } from "./types.ts";

export type ReceiptApprovalTokenVerificationCode =
  | "webauthn_required_for_approval_token"
  | "unverified_approval_token"
  | "approval_token_scope_mismatch"
  | "approval_token_not_approved"
  | "approval_token_role_mismatch"
  | "approval_token_issued_to_mismatch";

export class ReceiptApprovalTokenVerificationError extends Error {
  override readonly name = "ReceiptApprovalTokenVerificationError";

  constructor(
    readonly code: ReceiptApprovalTokenVerificationCode,
    readonly path: string,
  ) {
    super(`${path}: ${code}`);
  }
}

export async function verifyReceiptApprovalTokens(
  receipt: ReceiptSnapshot,
  deps: { readonly webauthnStore: WebAuthnStore | null },
): Promise<ReceiptSnapshot> {
  const firstTokenPath = firstSignedApprovalTokenPath(receipt);
  if (firstTokenPath === null) return receipt;
  if (deps.webauthnStore === null) {
    throw new ReceiptApprovalTokenVerificationError(
      "webauthn_required_for_approval_token",
      firstTokenPath,
    );
  }

  const records = new Map<SignedApprovalToken["tokenId"], ConsumedWebAuthnTokenRecord | null>();
  let approvalsChanged = false;
  const approvals: ApprovalEvent[] = [];
  for (let index = 0; index < receipt.approvals.length; index += 1) {
    const approval = receipt.approvals[index];
    if (approval === undefined) continue;
    const tokenPath = `/approvals/${index}/signedToken`;
    const tokenVerdict = await verifySignedApprovalToken(
      approval.signedToken,
      tokenPath,
      deps.webauthnStore,
      records,
    );
    approvals.push({ ...approval, tokenVerdict });
    if (!sameTokenVerdict(approval.tokenVerdict, tokenVerdict)) {
      approvalsChanged = true;
    }
  }

  for (let index = 0; index < receipt.writes.length; index += 1) {
    const write = receipt.writes[index];
    if (write?.approvalToken === null || write?.approvalToken === undefined) continue;
    await verifySignedApprovalToken(
      write.approvalToken,
      `/writes/${index}/approvalToken`,
      deps.webauthnStore,
      records,
    );
  }

  if (!approvalsChanged) return receipt;
  if (receipt.schemaVersion === 1) {
    return { ...receipt, approvals };
  }
  return { ...receipt, approvals };
}

async function verifySignedApprovalToken(
  token: SignedApprovalToken,
  path: string,
  store: WebAuthnStore,
  records: Map<SignedApprovalToken["tokenId"], ConsumedWebAuthnTokenRecord | null>,
): Promise<BrokerTokenVerdict> {
  let record = records.get(token.tokenId);
  if (!records.has(token.tokenId)) {
    record = await store.getConsumedToken(token.tokenId);
    records.set(token.tokenId, record);
  }
  if (record === null || record === undefined) {
    throw new ReceiptApprovalTokenVerificationError("unverified_approval_token", path);
  }
  const expectedClaimScopeHash = hashClaimScope(token);
  if (record.claimScopeHash !== expectedClaimScopeHash) {
    throw new ReceiptApprovalTokenVerificationError("approval_token_scope_mismatch", path);
  }
  if (record.outcome !== "approved") {
    throw new ReceiptApprovalTokenVerificationError("approval_token_not_approved", path);
  }
  if (record.role !== token.scope.role) {
    throw new ReceiptApprovalTokenVerificationError("approval_token_role_mismatch", path);
  }
  if (record.issuedToAgentId !== token.issuedTo) {
    throw new ReceiptApprovalTokenVerificationError("approval_token_issued_to_mismatch", path);
  }
  return {
    status: "valid",
    verifiedAt: new Date(record.consumedAtMs),
  };
}

function firstSignedApprovalTokenPath(receipt: ReceiptSnapshot): string | null {
  if (receipt.approvals.length > 0) return "/approvals/0/signedToken";
  for (let index = 0; index < receipt.writes.length; index += 1) {
    const write = receipt.writes[index];
    if (write?.approvalToken !== null && write?.approvalToken !== undefined) {
      return `/writes/${index}/approvalToken`;
    }
  }
  return null;
}

function hashClaimScope(token: SignedApprovalToken): ReturnType<typeof sha256Hex> {
  return sha256Hex(
    canonicalJSON({
      claim: approvalClaimToJsonValue(token.claim),
      scope: approvalScopeToJsonValue(token.scope),
    }),
  );
}

function sameTokenVerdict(left: BrokerTokenVerdict, right: BrokerTokenVerdict): boolean {
  return left.status === right.status && left.verifiedAt.getTime() === right.verifiedAt.getTime();
}

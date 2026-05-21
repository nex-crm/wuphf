import type { DatabaseSync } from "node:sqlite";
import {
  type ApprovalClaim,
  type ApprovalRole,
  type ApprovalScope,
  asAgentId,
  asApprovalClaimId,
  asApprovalTokenId,
  asReceiptId,
  asSha256Hex,
  asTimestampMs,
  canonicalJSON,
  type ReceiptCoSignClaim,
  type ReceiptCoSignScope,
  sha256Hex,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import { openDatabase, runMigrations } from "../../src/event-log/index.ts";
import {
  __classifyWebAuthnSqliteReadErrorForTesting,
  __classifyWebAuthnSqliteWriteErrorForTesting,
  createWebAuthnStore,
} from "../../src/webauthn/store.ts";
import {
  type WebAuthnStore,
  WebAuthnStoreBusyError,
  WebAuthnStoreFullError,
  WebAuthnStoreUnavailableError,
} from "../../src/webauthn/types.ts";

const agentId = asAgentId("agent_alpha");

let db: DatabaseSync | null = null;

afterEach(() => {
  if (db !== null) {
    db.close();
    db = null;
  }
});

describe("SqliteWebAuthnStore", () => {
  it("prunes expired consumed tokens and expired orphan challenges", async () => {
    const store = createTestStore();
    await saveCredential(store);
    const expired = await saveConsumedCosign(store, {
      challengeId: "expiredConsumedChallenge",
      tokenId: "01BRZ3NDEKTSV4RRFFQ69G5FC1",
      expiresAtMs: 100,
      consumedAtMs: 10,
      newSignCount: 2,
    });
    const unexpired = await saveConsumedCosign(store, {
      challengeId: "unexpiredConsumedChallenge",
      tokenId: "01BRZ3NDEKTSV4RRFFQ69G5FC2",
      expiresAtMs: 1_000,
      consumedAtMs: 20,
      newSignCount: 3,
    });
    await store.saveRegistrationChallenge({
      challengeId: "expiredOrphanChallenge",
      challenge: "expiredOrphanChallenge",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 90,
    });
    await store.saveRegistrationChallenge({
      challengeId: "unexpiredOrphanChallenge",
      challenge: "unexpiredOrphanChallenge",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 1_000,
    });

    const pruned = await store.pruneExpired({ nowMs: 100 });

    expect(pruned).toEqual({ consumedTokens: 1, orphanChallenges: 2 });
    await expect(store.getConsumedToken(expired.tokenId)).resolves.toBeNull();
    await expect(store.getChallenge(expired.challengeId)).resolves.toBeNull();
    await expect(store.getConsumedToken(unexpired.tokenId)).resolves.not.toBeNull();
    await expect(store.getChallenge(unexpired.challengeId)).resolves.not.toBeNull();
    await expect(store.getChallenge("expiredOrphanChallenge")).resolves.toBeNull();
    await expect(store.getChallenge("unexpiredOrphanChallenge")).resolves.not.toBeNull();
  });

  it("bounds each expired state prune batch", async () => {
    const store = createTestStore();
    await store.saveRegistrationChallenge({
      challengeId: "expiredOrphanOne",
      challenge: "expiredOrphanOne",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 10,
    });
    await store.saveRegistrationChallenge({
      challengeId: "expiredOrphanTwo",
      challenge: "expiredOrphanTwo",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 20,
    });

    const first = await store.pruneExpired({ nowMs: 100, maxRows: 1 });
    const second = await store.pruneExpired({ nowMs: 100, maxRows: 1 });

    expect(first).toEqual({ consumedTokens: 0, orphanChallenges: 1 });
    expect(second).toEqual({ consumedTokens: 0, orphanChallenges: 1 });
    await expect(store.getChallenge("expiredOrphanOne")).resolves.toBeNull();
    await expect(store.getChallenge("expiredOrphanTwo")).resolves.toBeNull();
  });

  it("returns the consumed token claim-scope hash from the linked challenge", async () => {
    const store = createTestStore();
    await saveCredential(store);
    const consumed = await saveConsumedCosign(store, {
      challengeId: "claimScopeHashChallenge",
      tokenId: "01BRZ3NDEKTSV4RRFFQ69G5FC3",
      expiresAtMs: 1_000,
      consumedAtMs: 20,
      newSignCount: 2,
    });

    const record = await store.getConsumedToken(consumed.tokenId);

    expect(record?.claimScopeHash).toBe(consumed.claimScopeHash);
  });

  it("derives consumed token identity from the stored cosign challenge", async () => {
    const store = createTestStore();
    await saveCredential(store);
    const first = await saveCosignChallenge(store, {
      challengeId: "identityChallengeOne",
      tokenId: "01BRZ3NDEKTSV4RRFFQ69G5FD2",
      receiptId: "01BRZ3NDEKTSV4RRFFQ69G5FA1",
      frozenArgsHash: "b".repeat(64),
    });
    const second = await saveCosignChallenge(store, {
      challengeId: "identityChallengeTwo",
      tokenId: "01BRZ3NDEKTSV4RRFFQ69G5FD3",
      receiptId: "01BRZ3NDEKTSV4RRFFQ69G5FA2",
      frozenArgsHash: "c".repeat(64),
    });

    const consumed = await store.consumeCosignChallenge({
      challengeId: first.challengeId,
      tokenId: second.tokenId,
      credentialId: "cred_approver",
      newSignCount: 2,
      requiredThreshold: 1,
      approvedResponseJson: { status: "approved" },
      role: "host",
      approvalGroupHash: second.approvalGroupHash,
      issuedToAgentId: asAgentId("agent_beta"),
      expiresAtMs: 99_999,
      consumedAtMs: 20,
    });

    expect(consumed).toMatchObject({
      tokenId: first.tokenId,
      challengeId: first.challengeId,
      role: "approver",
      approvalGroupHash: first.approvalGroupHash,
      issuedToAgentId: agentId,
      expiresAtMs: 10_000,
    });
    await expect(store.getConsumedToken(first.tokenId)).resolves.toMatchObject({
      tokenId: first.tokenId,
      approvalGroupHash: first.approvalGroupHash,
    });
    await expect(store.getConsumedToken(second.tokenId)).resolves.toBeNull();
    await expect(
      store.listSatisfiedRoles({
        approvalGroupHash: first.approvalGroupHash,
        issuedToAgentId: agentId,
        nowMs: 20,
      }),
    ).resolves.toEqual(["approver"]);
    await expect(
      store.listSatisfiedRoles({
        approvalGroupHash: second.approvalGroupHash,
        issuedToAgentId: asAgentId("agent_beta"),
        nowMs: 20,
      }),
    ).resolves.toEqual([]);
  });

  it.each([
    "SQLITE_BUSY",
    "SQLITE_LOCKED",
    "SQLITE_BUSY_SNAPSHOT",
    "SQLITE_LOCKED_SHAREDCACHE",
  ])("classifies %s as retryable storage contention", (code) => {
    const classified = __classifyWebAuthnSqliteWriteErrorForTesting(sqliteError(code));

    expect(classified).toBeInstanceOf(WebAuthnStoreBusyError);
  });

  it("classifies SQLITE_FULL as insufficient storage", () => {
    const classified = __classifyWebAuthnSqliteWriteErrorForTesting(sqliteError("SQLITE_FULL"));

    expect(classified).toBeInstanceOf(WebAuthnStoreFullError);
  });

  it.each([
    "SQLITE_READONLY",
    "SQLITE_CANTOPEN",
    "SQLITE_CORRUPT",
    "SQLITE_IOERR_READ",
    "SQLITE_NOTADB",
    "SQLITE_PERM",
  ])("classifies %s as storage unavailable", (code) => {
    const classified = __classifyWebAuthnSqliteWriteErrorForTesting(sqliteError(code));

    expect(classified).toBeInstanceOf(WebAuthnStoreUnavailableError);
  });

  it("classifies corrupt stored JSON on read as storage unavailable", async () => {
    const store = createTestStore();
    const { claim, scope } = receiptCoSignFixture("approver");
    const hashes = hashClaimScopeForTest(claim, scope);
    await store.saveCosignChallenge({
      challengeId: "corruptReadChallenge",
      challenge: "corruptReadChallenge",
      tokenId: asApprovalTokenId("01BRZ3NDEKTSV4RRFFQ69G5FD1"),
      claim,
      scope,
      claimJson: hashes.claimJson,
      scopeJson: hashes.scopeJson,
      claimScopeHash: hashes.claimScopeHash,
      approvalGroupHash: hashes.approvalGroupHash,
      issuedToAgentId: agentId,
      notBeforeMs: asTimestampMs(1),
      expiresAtMs: asTimestampMs(10_000),
      createdAtMs: 1,
    });
    requiredDb()
      .prepare("UPDATE webauthn_challenges SET claim_json = ? WHERE challenge_id = ?")
      .run("{", "corruptReadChallenge");

    await expect(store.getChallenge("corruptReadChallenge")).rejects.toBeInstanceOf(
      WebAuthnStoreUnavailableError,
    );
  });

  it("classifies closed store reads as storage unavailable", async () => {
    const store = createTestStore();
    const closeableStore = store as WebAuthnStore & { close(): void };
    closeableStore.close();

    await expect(store.getCredential("cred_approver")).rejects.toBeInstanceOf(
      WebAuthnStoreUnavailableError,
    );
  });

  it.each([
    "SQLITE_BUSY",
    "SQLITE_LOCKED",
  ])("classifies read-side %s as retryable storage contention", (code) => {
    const classified = __classifyWebAuthnSqliteReadErrorForTesting(sqliteError(code));

    expect(classified).toBeInstanceOf(WebAuthnStoreBusyError);
  });
});

function createTestStore(): WebAuthnStore {
  db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  return createWebAuthnStore(db);
}

function requiredDb(): DatabaseSync {
  if (db === null) {
    throw new Error("test database is not open");
  }
  return db;
}

async function saveCredential(store: WebAuthnStore): Promise<void> {
  await store.saveRegistrationChallenge({
    challengeId: "registrationChallenge",
    challenge: "registrationChallenge",
    role: "approver",
    issuedToAgentId: agentId,
    createdAtMs: 1,
    expiresAtMs: 1_000,
  });
  await store.saveCredential({
    challengeId: "registrationChallenge",
    credential: {
      credentialId: "cred_approver",
      publicKey: new Uint8Array([1, 2, 3]),
      signCount: 1,
      role: "approver",
      agentId,
      createdAtMs: 2,
    },
    consumedAtMs: 2,
  });
}

async function saveConsumedCosign(
  store: WebAuthnStore,
  args: {
    readonly challengeId: string;
    readonly tokenId: string;
    readonly expiresAtMs: number;
    readonly consumedAtMs: number;
    readonly newSignCount: number;
  },
): Promise<{
  readonly challengeId: string;
  readonly tokenId: ReturnType<typeof asApprovalTokenId>;
  readonly claimScopeHash: ReturnType<typeof sha256Hex>;
}> {
  const { claim, scope } = receiptCoSignFixture("approver");
  const hashes = hashClaimScopeForTest(claim, scope);
  const tokenId = asApprovalTokenId(args.tokenId);
  await store.saveCosignChallenge({
    challengeId: args.challengeId,
    challenge: args.challengeId,
    tokenId,
    claim,
    scope,
    claimJson: hashes.claimJson,
    scopeJson: hashes.scopeJson,
    claimScopeHash: hashes.claimScopeHash,
    approvalGroupHash: hashes.approvalGroupHash,
    issuedToAgentId: agentId,
    notBeforeMs: asTimestampMs(args.consumedAtMs),
    expiresAtMs: asTimestampMs(args.expiresAtMs),
    createdAtMs: args.consumedAtMs,
  });
  await store.consumeCosignChallenge({
    challengeId: args.challengeId,
    tokenId,
    credentialId: "cred_approver",
    newSignCount: args.newSignCount,
    requiredThreshold: 1,
    approvedResponseJson: { status: "approved" },
    role: "approver",
    approvalGroupHash: hashes.approvalGroupHash,
    issuedToAgentId: agentId,
    expiresAtMs: args.expiresAtMs,
    consumedAtMs: args.consumedAtMs,
  });
  return { challengeId: args.challengeId, tokenId, claimScopeHash: hashes.claimScopeHash };
}

async function saveCosignChallenge(
  store: WebAuthnStore,
  args: {
    readonly challengeId: string;
    readonly tokenId: string;
    readonly receiptId: string;
    readonly frozenArgsHash: string;
  },
): Promise<{
  readonly challengeId: string;
  readonly tokenId: ReturnType<typeof asApprovalTokenId>;
  readonly approvalGroupHash: ReturnType<typeof sha256Hex>;
}> {
  const fixture = receiptCoSignFixture("approver");
  const claim = {
    ...fixture.claim,
    claimId: asApprovalClaimId(`claim-${args.challengeId}`),
    receiptId: asReceiptId(args.receiptId),
    frozenArgsHash: asSha256Hex(args.frozenArgsHash),
  } satisfies ApprovalClaim;
  const scope = {
    ...fixture.scope,
    claimId: claim.claimId,
    receiptId: claim.receiptId,
    frozenArgsHash: claim.frozenArgsHash,
  } satisfies ApprovalScope;
  const hashes = hashClaimScopeForTest(claim, scope);
  const tokenId = asApprovalTokenId(args.tokenId);
  await store.saveCosignChallenge({
    challengeId: args.challengeId,
    challenge: args.challengeId,
    tokenId,
    claim,
    scope,
    claimJson: hashes.claimJson,
    scopeJson: hashes.scopeJson,
    claimScopeHash: hashes.claimScopeHash,
    approvalGroupHash: hashes.approvalGroupHash,
    issuedToAgentId: agentId,
    notBeforeMs: asTimestampMs(10),
    expiresAtMs: asTimestampMs(10_000),
    createdAtMs: 10,
  });
  return { challengeId: args.challengeId, tokenId, approvalGroupHash: hashes.approvalGroupHash };
}

function receiptCoSignFixture(role: ApprovalRole): {
  readonly claim: ReceiptCoSignClaim;
  readonly scope: ReceiptCoSignScope;
} {
  const claim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-branch-12"),
    kind: "receipt_co_sign",
    receiptId: asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    frozenArgsHash: asSha256Hex("a".repeat(64)),
    riskClass: "high",
  } satisfies ReceiptCoSignClaim;
  const scope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "receipt_co_sign",
    role,
    maxUses: 1,
    receiptId: claim.receiptId,
    frozenArgsHash: claim.frozenArgsHash,
  } satisfies ReceiptCoSignScope;
  return { claim, scope };
}

function hashClaimScopeForTest(
  claim: ApprovalClaim,
  scope: ApprovalScope,
): {
  readonly claimScopeHash: ReturnType<typeof sha256Hex>;
  readonly approvalGroupHash: ReturnType<typeof sha256Hex>;
  readonly claimJson: string;
  readonly scopeJson: string;
} {
  const claimJson = canonicalJSON(claim);
  const scopeJson = canonicalJSON(scope);
  return {
    claimScopeHash: sha256Hex(`{"claim":${claimJson},"scope":${scopeJson}}`),
    approvalGroupHash: sha256Hex(`{"claim":${claimJson}}`),
    claimJson,
    scopeJson,
  };
}

function sqliteError(code: string): Error {
  return Object.assign(new Error("test sqlite error"), {
    code: "ERR_SQLITE_ERROR",
    errcode: sqliteErrcode(code),
    errstr: code,
  });
}

function sqliteErrcode(code: string): number {
  switch (code) {
    case "SQLITE_BUSY":
      return 5;
    case "SQLITE_LOCKED":
      return 6;
    case "SQLITE_BUSY_SNAPSHOT":
      return 517;
    case "SQLITE_LOCKED_SHAREDCACHE":
      return 262;
    case "SQLITE_FULL":
      return 13;
    case "SQLITE_READONLY":
      return 8;
    case "SQLITE_CANTOPEN":
      return 14;
    case "SQLITE_CORRUPT":
      return 11;
    case "SQLITE_IOERR_READ":
      return 266;
    case "SQLITE_NOTADB":
      return 26;
    case "SQLITE_PERM":
      return 3;
    default:
      throw new Error(`unknown sqlite test code: ${code}`);
  }
}

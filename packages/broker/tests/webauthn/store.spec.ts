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
  sha256Hex,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import BetterSqlite3 from "better-sqlite3";
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

let db: Database.Database | null = null;

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

    await store.pruneExpired({ nowMs: 100 });

    await expect(store.getConsumedToken(expired.tokenId)).resolves.toBeNull();
    await expect(store.getChallenge(expired.challengeId)).resolves.toBeNull();
    await expect(store.getConsumedToken(unexpired.tokenId)).resolves.not.toBeNull();
    await expect(store.getChallenge(unexpired.challengeId)).resolves.not.toBeNull();
    await expect(store.getChallenge("expiredOrphanChallenge")).resolves.toBeNull();
    await expect(store.getChallenge("unexpiredOrphanChallenge")).resolves.not.toBeNull();
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

function requiredDb(): Database.Database {
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
  return { challengeId: args.challengeId, tokenId };
}

function receiptCoSignFixture(role: ApprovalRole): {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
} {
  const claim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-branch-12"),
    kind: "receipt_co_sign",
    receiptId: asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    frozenArgsHash: asSha256Hex("a".repeat(64)),
    riskClass: "high",
  } satisfies ApprovalClaim;
  const scope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "receipt_co_sign",
    role,
    maxUses: 1,
    receiptId: claim.receiptId,
    frozenArgsHash: claim.frozenArgsHash,
  } satisfies ApprovalScope;
  return { claim, scope };
}

function hashClaimScopeForTest(
  claim: ApprovalClaim,
  scope: ApprovalScope,
): {
  readonly claimScopeHash: ReturnType<typeof sha256Hex>;
  readonly approvalGroupHash: ReturnType<typeof sha256Hex>;
} {
  const claimJson = claim;
  return {
    claimScopeHash: sha256Hex(canonicalJSON({ claim: claimJson, scope })),
    approvalGroupHash: sha256Hex(canonicalJSON({ claim: claimJson })),
  };
}

function sqliteError(code: string): Error {
  return new BetterSqlite3.SqliteError("test sqlite error", code);
}

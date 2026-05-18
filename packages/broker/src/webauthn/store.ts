import {
  type AgentId,
  type ApprovalRole,
  type ApprovalTokenId,
  approvalClaimFromJson,
  approvalScopeFromJson,
  asAgentId,
  asApprovalTokenId,
  asSha256Hex,
  asTimestampMs,
  canonicalJSON,
  isApprovalRole,
  type JsonValue,
  type Sha256Hex,
  type TimestampMs,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import BetterSqlite3 from "better-sqlite3";

import { type OpenDatabaseArgs, openDatabase, runMigrations } from "../event-log/index.ts";
import {
  type ConsumeCosignChallengeArgs,
  type ConsumedWebAuthnTokenRecord,
  type CosignChallengeRecord,
  type PruneExpiredWebAuthnStateArgs,
  type PruneExpiredWebAuthnStateResult,
  type RegisteredWebAuthnCredential,
  type RegistrationChallengeRecord,
  type SaveCosignChallengeArgs,
  type SaveCredentialArgs,
  type SaveRegistrationChallengeArgs,
  type WebAuthnChallengeRecord,
  WebAuthnSignCountReplayError,
  type WebAuthnStore,
  WebAuthnStoreBusyError,
  WebAuthnStoreFullError,
  WebAuthnStoreUnavailableError,
  type WebAuthnTokenOutcome,
} from "./types.ts";

export interface SqliteWebAuthnStoreConfig extends OpenDatabaseArgs {}

interface SqliteWebAuthnStoreOptions {
  readonly closeDatabase?: boolean;
}

interface CredentialRow {
  readonly credentialId: string;
  readonly publicKeyB64url: string;
  readonly signCount: number;
  readonly role: string;
  readonly agentId: string;
  readonly createdAtMs: number;
}

interface ChallengeRow {
  readonly challengeId: string;
  readonly challengeType: string;
  readonly challenge: string;
  readonly tokenId: string | null;
  readonly role: string | null;
  readonly agentId: string;
  readonly claimJson: string | null;
  readonly scopeJson: string | null;
  readonly claimScopeHash: string | null;
  readonly approvalGroupHash: string | null;
  readonly notBeforeMs: number | null;
  readonly expiresAtMs: number;
  readonly consumedAtMs: number | null;
}

interface ConsumedTokenRow {
  readonly tokenId: string;
  readonly challengeId: string;
  readonly outcome: string;
  readonly responseJson: string;
  readonly role: string;
  readonly claimScopeHash: string;
  readonly approvalGroupHash: string;
  readonly agentId: string;
  readonly expiresAtMs: number;
  readonly consumedAtMs: number;
}

interface RoleRow {
  readonly role: string;
}

type InsertRegistrationChallengeParams = [string, string, ApprovalRole, AgentId, number, number];
type InsertCosignChallengeParams = [
  string,
  string,
  ApprovalTokenId,
  ApprovalRole,
  AgentId,
  string,
  string,
  Sha256Hex,
  Sha256Hex,
  TimestampMs,
  TimestampMs,
  number,
];
type InsertCredentialParams = [string, string, number, ApprovalRole, AgentId, number];
type MarkChallengeConsumedParams = [number, string];
type UpdateCredentialCounterParams = [number, string, number];
type InsertConsumedTokenParams = [
  ApprovalTokenId,
  string,
  WebAuthnTokenOutcome,
  string,
  ApprovalRole,
  Sha256Hex,
  AgentId,
  number,
  number,
];
type UpdateConsumedTokenParams = [WebAuthnTokenOutcome, string, ApprovalTokenId];
type PruneExpiredParams = [number, number];

const TOKEN_OUTCOME_SET: ReadonlySet<string> = new Set(["approval_pending", "approved"]);
const DEFAULT_PRUNE_EXPIRED_BATCH_ROWS = 100;
const MAX_PRUNE_EXPIRED_BATCH_ROWS = 1_000;

export class SqliteWebAuthnStore implements WebAuthnStore {
  private readonly closeDatabase: boolean;
  private readonly insertRegistrationChallengeStmt: Database.Statement<InsertRegistrationChallengeParams>;
  private readonly insertCosignChallengeStmt: Database.Statement<InsertCosignChallengeParams>;
  private readonly getChallengeStmt: Database.Statement<[string], ChallengeRow>;
  private readonly listCredentialsForAgentStmt: Database.Statement<[AgentId], CredentialRow>;
  private readonly listCredentialsForAgentRoleStmt: Database.Statement<
    [AgentId, ApprovalRole],
    CredentialRow
  >;
  private readonly getCredentialStmt: Database.Statement<[string], CredentialRow>;
  private readonly insertCredentialStmt: Database.Statement<InsertCredentialParams>;
  private readonly markChallengeConsumedStmt: Database.Statement<MarkChallengeConsumedParams>;
  private readonly getConsumedTokenStmt: Database.Statement<[ApprovalTokenId], ConsumedTokenRow>;
  private readonly listSatisfiedRolesStmt: Database.Statement<
    [Sha256Hex, AgentId, number],
    RoleRow
  >;
  private readonly updateCredentialCounterStmt: Database.Statement<UpdateCredentialCounterParams>;
  private readonly insertConsumedTokenStmt: Database.Statement<InsertConsumedTokenParams>;
  private readonly updateConsumedTokenStmt: Database.Statement<UpdateConsumedTokenParams>;
  private readonly pruneExpiredConsumedTokensStmt: Database.Statement<PruneExpiredParams>;
  private readonly pruneExpiredOrphanChallengesStmt: Database.Statement<PruneExpiredParams>;
  private readonly saveCredentialTransaction: Database.Transaction<
    (args: SaveCredentialArgs) => void
  >;
  private readonly consumeCosignTransaction: Database.Transaction<
    (args: ConsumeCosignChallengeArgs) => ConsumedWebAuthnTokenRecord
  >;
  private readonly pruneExpiredTransaction: Database.Transaction<
    (args: Required<PruneExpiredWebAuthnStateArgs>) => PruneExpiredWebAuthnStateResult
  >;
  private closed = false;

  static open(config: SqliteWebAuthnStoreConfig): SqliteWebAuthnStore {
    const db = openDatabase(config);
    try {
      runMigrations(db);
      return new SqliteWebAuthnStore(db, { closeDatabase: true });
    } catch (err) {
      db.close();
      throw err;
    }
  }

  constructor(
    private readonly db: Database.Database,
    options: SqliteWebAuthnStoreOptions = {},
  ) {
    this.closeDatabase = options.closeDatabase ?? false;
    this.insertRegistrationChallengeStmt = db.prepare<InsertRegistrationChallengeParams>(
      `INSERT INTO webauthn_challenges
        (challenge_id, challenge_type, challenge_b64url, role, agent_id, expires_at_ms, created_at_ms)
       VALUES (?, 'registration', ?, ?, ?, ?, ?)`,
    );
    this.insertCosignChallengeStmt = db.prepare<InsertCosignChallengeParams>(
      `INSERT INTO webauthn_challenges
        (challenge_id, challenge_type, challenge_b64url, token_id, role, agent_id,
         claim_json, scope_json, claim_scope_hash, approval_group_hash,
         not_before_ms, expires_at_ms, created_at_ms)
       VALUES (?, 'cosign', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    );
    this.getChallengeStmt = db.prepare<[string], ChallengeRow>(
      `SELECT challenge_id AS challengeId,
              challenge_type AS challengeType,
              challenge_b64url AS challenge,
              token_id AS tokenId,
              role,
              agent_id AS agentId,
              claim_json AS claimJson,
              scope_json AS scopeJson,
              claim_scope_hash AS claimScopeHash,
              approval_group_hash AS approvalGroupHash,
              not_before_ms AS notBeforeMs,
              expires_at_ms AS expiresAtMs,
              consumed_at_ms AS consumedAtMs
       FROM webauthn_challenges
       WHERE challenge_id = ?`,
    );
    this.listCredentialsForAgentStmt = db.prepare<[AgentId], CredentialRow>(
      `SELECT credential_id AS credentialId,
              public_key_b64url AS publicKeyB64url,
              sign_count AS signCount,
              role,
              agent_id AS agentId,
              created_at_ms AS createdAtMs
       FROM webauthn_registered_credentials
       WHERE agent_id = ?
       ORDER BY role ASC, credential_id ASC`,
    );
    this.listCredentialsForAgentRoleStmt = db.prepare<[AgentId, ApprovalRole], CredentialRow>(
      `SELECT credential_id AS credentialId,
              public_key_b64url AS publicKeyB64url,
              sign_count AS signCount,
              role,
              agent_id AS agentId,
              created_at_ms AS createdAtMs
       FROM webauthn_registered_credentials
       WHERE agent_id = ? AND role = ?
       ORDER BY credential_id ASC`,
    );
    this.getCredentialStmt = db.prepare<[string], CredentialRow>(
      `SELECT credential_id AS credentialId,
              public_key_b64url AS publicKeyB64url,
              sign_count AS signCount,
              role,
              agent_id AS agentId,
              created_at_ms AS createdAtMs
       FROM webauthn_registered_credentials
       WHERE credential_id = ?`,
    );
    this.insertCredentialStmt = db.prepare<InsertCredentialParams>(
      `INSERT INTO webauthn_registered_credentials
        (credential_id, public_key_b64url, sign_count, role, agent_id, created_at_ms)
       VALUES (?, ?, ?, ?, ?, ?)`,
    );
    this.markChallengeConsumedStmt = db.prepare<MarkChallengeConsumedParams>(
      "UPDATE webauthn_challenges SET consumed_at_ms = ? WHERE challenge_id = ? AND consumed_at_ms IS NULL",
    );
    this.getConsumedTokenStmt = db.prepare<[ApprovalTokenId], ConsumedTokenRow>(
      `SELECT t.token_id AS tokenId,
              t.challenge_id AS challengeId,
              t.outcome,
              t.response_json AS responseJson,
              t.role,
              c.claim_scope_hash AS claimScopeHash,
              t.approval_group_hash AS approvalGroupHash,
              t.agent_id AS agentId,
              t.expires_at_ms AS expiresAtMs,
              t.consumed_at_ms AS consumedAtMs
       FROM webauthn_consumed_tokens AS t
       INNER JOIN webauthn_challenges AS c
         ON c.challenge_id = t.challenge_id
       WHERE t.token_id = ?`,
    );
    this.listSatisfiedRolesStmt = db.prepare<[Sha256Hex, AgentId, number], RoleRow>(
      `SELECT DISTINCT role
       FROM webauthn_consumed_tokens
       WHERE approval_group_hash = ?
         AND agent_id = ?
         AND expires_at_ms > ?
         AND outcome IN ('approval_pending', 'approved')
       ORDER BY role ASC`,
    );
    this.updateCredentialCounterStmt = db.prepare<UpdateCredentialCounterParams>(
      `UPDATE webauthn_registered_credentials
       SET sign_count = ?
       WHERE credential_id = ?
         AND (sign_count = 0 OR sign_count < ?)`,
    );
    this.insertConsumedTokenStmt = db.prepare<InsertConsumedTokenParams>(
      `INSERT INTO webauthn_consumed_tokens
        (token_id, challenge_id, outcome, response_json, role, approval_group_hash,
         agent_id, expires_at_ms, consumed_at_ms)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    );
    this.updateConsumedTokenStmt = db.prepare<UpdateConsumedTokenParams>(
      `UPDATE webauthn_consumed_tokens
       SET outcome = ?, response_json = ?
       WHERE token_id = ?`,
    );
    this.pruneExpiredConsumedTokensStmt = db.prepare<PruneExpiredParams>(
      `DELETE FROM webauthn_consumed_tokens
       WHERE token_id IN (
         SELECT token_id
         FROM webauthn_consumed_tokens
         WHERE expires_at_ms <= ?
         ORDER BY expires_at_ms ASC, token_id ASC
         LIMIT ?
       )`,
    );
    this.pruneExpiredOrphanChallengesStmt = db.prepare<PruneExpiredParams>(
      `DELETE FROM webauthn_challenges
       WHERE challenge_id IN (
         SELECT c.challenge_id
         FROM webauthn_challenges AS c
         WHERE c.expires_at_ms <= ?
           AND NOT EXISTS (
             SELECT 1
             FROM webauthn_consumed_tokens AS t
             WHERE t.challenge_id = c.challenge_id
           )
         ORDER BY c.expires_at_ms ASC, c.challenge_id ASC
         LIMIT ?
       )`,
    );
    this.saveCredentialTransaction = db.transaction((args: SaveCredentialArgs) => {
      this.insertCredentialStmt.run(
        args.credential.credentialId,
        Buffer.from(args.credential.publicKey).toString("base64url"),
        args.credential.signCount,
        args.credential.role,
        args.credential.agentId,
        args.credential.createdAtMs,
      );
      const result = this.markChallengeConsumedStmt.run(args.consumedAtMs, args.challengeId);
      if (result.changes !== 1) {
        throw new Error("webauthn registration challenge was already consumed");
      }
    });
    this.consumeCosignTransaction = db.transaction((args: ConsumeCosignChallengeArgs) => {
      const challenge = this.getChallengeStmt.get(args.challengeId);
      if (
        challenge === undefined ||
        challenge.tokenId === null ||
        challenge.role === null ||
        challenge.claimScopeHash === null ||
        challenge.approvalGroupHash === null
      ) {
        throw new Error("webauthn consumed token challenge is missing claim_scope_hash");
      }
      const tokenId = asApprovalTokenId(challenge.tokenId);
      const role = roleFromString(challenge.role, "webauthn_challenges.role");
      const claimScopeHash = asSha256Hex(challenge.claimScopeHash);
      const approvalGroupHash = asSha256Hex(challenge.approvalGroupHash);
      const issuedToAgentId = asAgentId(challenge.agentId);
      const expiresAtMs = challenge.expiresAtMs;
      const existing = this.getConsumedTokenStmt.get(tokenId);
      if (existing !== undefined) return consumedTokenFromRow(existing);
      const result = this.markChallengeConsumedStmt.run(args.consumedAtMs, challenge.challengeId);
      if (result.changes !== 1) {
        throw new Error("webauthn cosign challenge was already consumed");
      }
      const counterResult = this.updateCredentialCounterStmt.run(
        args.newSignCount,
        args.credentialId,
        args.newSignCount,
      );
      if (counterResult.changes !== 1) {
        throw new WebAuthnSignCountReplayError();
      }
      this.insertConsumedTokenStmt.run(
        tokenId,
        challenge.challengeId,
        "approval_pending",
        canonicalJSON({
          status: "approval_pending",
          satisfiedRoles: [role],
          requiredThreshold: args.requiredThreshold,
        }),
        role,
        approvalGroupHash,
        issuedToAgentId,
        expiresAtMs,
        args.consumedAtMs,
      );
      const satisfiedRoles = sortedUniqueRoles(
        this.listSatisfiedRolesStmt
          .all(approvalGroupHash, issuedToAgentId, args.consumedAtMs)
          .map((row) => roleFromString(row.role, "webauthn_consumed_tokens.role")),
      );
      const thresholdMet = satisfiedRoles.length >= args.requiredThreshold;
      const outcome = thresholdMet ? "approved" : "approval_pending";
      const responseJson: JsonValue = thresholdMet
        ? args.approvedResponseJson
        : {
            status: "approval_pending",
            satisfiedRoles,
            requiredThreshold: args.requiredThreshold,
          };
      this.updateConsumedTokenStmt.run(outcome, canonicalJSON(responseJson), tokenId);
      return {
        tokenId,
        challengeId: challenge.challengeId,
        outcome,
        responseJson,
        role,
        claimScopeHash,
        approvalGroupHash,
        issuedToAgentId,
        expiresAtMs,
        consumedAtMs: args.consumedAtMs,
      };
    });
    this.pruneExpiredTransaction = db.transaction(
      (args: Required<PruneExpiredWebAuthnStateArgs>) => {
        const consumedTokens = this.pruneExpiredConsumedTokensStmt.run(
          args.nowMs,
          args.maxRows,
        ).changes;
        const orphanChallenges = this.pruneExpiredOrphanChallengesStmt.run(
          args.nowMs,
          args.maxRows,
        ).changes;
        return { consumedTokens, orphanChallenges };
      },
    );
  }

  async saveRegistrationChallenge(args: SaveRegistrationChallengeArgs): Promise<void> {
    this.assertOpen();
    try {
      this.insertRegistrationChallengeStmt.run(
        args.challengeId,
        args.challenge,
        args.role,
        args.issuedToAgentId,
        args.expiresAtMs,
        args.createdAtMs,
      );
    } catch (err) {
      throw classifySqliteWriteError(err, "SqliteWebAuthnStore.saveRegistrationChallenge");
    }
  }

  async saveCosignChallenge(args: SaveCosignChallengeArgs): Promise<void> {
    this.assertOpen();
    try {
      this.insertCosignChallengeStmt.run(
        args.challengeId,
        args.challenge,
        args.tokenId,
        args.scope.role,
        args.issuedToAgentId,
        args.claimJson,
        args.scopeJson,
        args.claimScopeHash,
        args.approvalGroupHash,
        args.notBeforeMs,
        args.expiresAtMs,
        args.createdAtMs,
      );
    } catch (err) {
      throw classifySqliteWriteError(err, "SqliteWebAuthnStore.saveCosignChallenge");
    }
  }

  async pruneExpired(
    args: PruneExpiredWebAuthnStateArgs,
  ): Promise<PruneExpiredWebAuthnStateResult> {
    this.assertOpen();
    try {
      return this.pruneExpiredTransaction.immediate({
        nowMs: args.nowMs,
        maxRows: normalizePruneExpiredBatchRows(args.maxRows),
      });
    } catch (err) {
      throw classifySqliteWriteError(err, "SqliteWebAuthnStore.pruneExpired");
    }
  }

  async getChallenge(challengeId: string): Promise<WebAuthnChallengeRecord | null> {
    try {
      this.assertOpen();
      const row = this.getChallengeStmt.get(challengeId);
      return row === undefined ? null : challengeFromRow(row);
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.getChallenge");
    }
  }

  async listCredentialsForAgent(
    agentId: AgentId,
  ): Promise<readonly RegisteredWebAuthnCredential[]> {
    try {
      this.assertOpen();
      return this.listCredentialsForAgentStmt.all(agentId).map(credentialFromRow);
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.listCredentialsForAgent");
    }
  }

  async listCredentialsForAgentRole(args: {
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
  }): Promise<readonly RegisteredWebAuthnCredential[]> {
    try {
      this.assertOpen();
      return this.listCredentialsForAgentRoleStmt
        .all(args.agentId, args.role)
        .map(credentialFromRow);
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.listCredentialsForAgentRole");
    }
  }

  async getCredential(credentialId: string): Promise<RegisteredWebAuthnCredential | null> {
    try {
      this.assertOpen();
      const row = this.getCredentialStmt.get(credentialId);
      return row === undefined ? null : credentialFromRow(row);
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.getCredential");
    }
  }

  async saveCredential(args: SaveCredentialArgs): Promise<void> {
    this.assertOpen();
    try {
      this.saveCredentialTransaction.immediate(args);
    } catch (err) {
      throw classifySqliteWriteError(err, "SqliteWebAuthnStore.saveCredential");
    }
  }

  async getConsumedToken(tokenId: ApprovalTokenId): Promise<ConsumedWebAuthnTokenRecord | null> {
    try {
      this.assertOpen();
      const row = this.getConsumedTokenStmt.get(tokenId);
      return row === undefined ? null : consumedTokenFromRow(row);
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.getConsumedToken");
    }
  }

  async listSatisfiedRoles(args: {
    readonly approvalGroupHash: Sha256Hex;
    readonly issuedToAgentId: AgentId;
    readonly nowMs: number;
  }): Promise<readonly ApprovalRole[]> {
    try {
      this.assertOpen();
      return this.listSatisfiedRolesStmt
        .all(args.approvalGroupHash, args.issuedToAgentId, args.nowMs)
        .map((row) => roleFromString(row.role, "webauthn_consumed_tokens.role"));
    } catch (err) {
      throw classifySqliteReadError(err, "SqliteWebAuthnStore.listSatisfiedRoles");
    }
  }

  async consumeCosignChallenge(
    args: ConsumeCosignChallengeArgs,
  ): Promise<ConsumedWebAuthnTokenRecord> {
    this.assertOpen();
    try {
      return this.consumeCosignTransaction.immediate(args);
    } catch (err) {
      if (err instanceof WebAuthnSignCountReplayError) {
        throw err;
      }
      throw classifySqliteWriteError(err, "SqliteWebAuthnStore.consumeCosignChallenge");
    }
  }

  close(): void {
    if (this.closed) return;
    if (this.closeDatabase) {
      this.db.close();
    }
    this.closed = true;
  }

  private assertOpen(): void {
    if (this.closed) {
      throw new WebAuthnStoreUnavailableError("SqliteWebAuthnStore: storage error (closed)");
    }
  }
}

export function createWebAuthnStore(db: Database.Database): WebAuthnStore {
  return new SqliteWebAuthnStore(db);
}

function challengeFromRow(row: ChallengeRow): WebAuthnChallengeRecord {
  if (row.challengeType === "registration") {
    if (row.role === null) throw new Error("webauthn registration challenge missing role");
    return {
      challengeId: row.challengeId,
      type: "registration",
      challenge: row.challenge,
      role: roleFromString(row.role, "webauthn_challenges.role"),
      issuedToAgentId: asAgentId(row.agentId),
      expiresAtMs: row.expiresAtMs,
      consumedAtMs: row.consumedAtMs,
    } satisfies RegistrationChallengeRecord;
  }
  if (row.challengeType === "cosign") {
    return cosignChallengeFromRow(row);
  }
  throw new Error(`Unknown webauthn challenge type: ${row.challengeType}`);
}

function cosignChallengeFromRow(row: ChallengeRow): CosignChallengeRecord {
  if (
    row.tokenId === null ||
    row.claimJson === null ||
    row.scopeJson === null ||
    row.claimScopeHash === null ||
    row.approvalGroupHash === null ||
    row.notBeforeMs === null
  ) {
    throw new Error("webauthn cosign challenge row is incomplete");
  }
  const claimJson = parseStoredJson(row.claimJson, "webauthn_challenges.claim_json");
  const scopeJson = parseStoredJson(row.scopeJson, "webauthn_challenges.scope_json");
  const claim = approvalClaimFromJson(claimJson, "webauthn_challenges.claim_json");
  const scope = approvalScopeFromJson(scopeJson, "webauthn_challenges.scope_json");
  return {
    challengeId: row.challengeId,
    type: "cosign",
    challenge: row.challenge,
    tokenId: asApprovalTokenId(row.tokenId),
    claim,
    scope,
    claimScopeHash: asSha256Hex(row.claimScopeHash),
    approvalGroupHash: asSha256Hex(row.approvalGroupHash),
    issuedToAgentId: asAgentId(row.agentId),
    notBeforeMs: asTimestampMs(row.notBeforeMs),
    expiresAtMs: asTimestampMs(row.expiresAtMs),
    consumedAtMs: row.consumedAtMs,
  };
}

function credentialFromRow(row: CredentialRow): RegisteredWebAuthnCredential {
  return {
    credentialId: row.credentialId,
    publicKey: new Uint8Array(Buffer.from(row.publicKeyB64url, "base64url")),
    signCount: row.signCount,
    role: roleFromString(row.role, "webauthn_registered_credentials.role"),
    agentId: asAgentId(row.agentId),
    createdAtMs: row.createdAtMs,
  };
}

function consumedTokenFromRow(row: ConsumedTokenRow): ConsumedWebAuthnTokenRecord {
  return {
    tokenId: asApprovalTokenId(row.tokenId),
    challengeId: row.challengeId,
    outcome: outcomeFromString(row.outcome),
    responseJson: parseStoredJson(row.responseJson, "webauthn_consumed_tokens.response_json"),
    role: roleFromString(row.role, "webauthn_consumed_tokens.role"),
    claimScopeHash: asSha256Hex(row.claimScopeHash),
    approvalGroupHash: asSha256Hex(row.approvalGroupHash),
    issuedToAgentId: asAgentId(row.agentId),
    expiresAtMs: row.expiresAtMs,
    consumedAtMs: row.consumedAtMs,
  };
}

function parseStoredJson(value: string, path: string): JsonValue {
  const parsed = JSON.parse(value) as unknown;
  try {
    canonicalJSON(parsed);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
  return parsed as JsonValue;
}

function normalizePruneExpiredBatchRows(value: number | undefined): number {
  const rows = value ?? DEFAULT_PRUNE_EXPIRED_BATCH_ROWS;
  if (!Number.isSafeInteger(rows) || rows < 1 || rows > MAX_PRUNE_EXPIRED_BATCH_ROWS) {
    throw new Error(
      `webauthn prune batch rows must be an integer in 1..${MAX_PRUNE_EXPIRED_BATCH_ROWS}`,
    );
  }
  return rows;
}

function roleFromString(value: string, path: string): ApprovalRole {
  if (isApprovalRole(value)) {
    return value;
  }
  throw new Error(`${path}: invalid approval role`);
}

function outcomeFromString(value: string): WebAuthnTokenOutcome {
  if (TOKEN_OUTCOME_SET.has(value)) {
    return value as WebAuthnTokenOutcome;
  }
  throw new Error(`webauthn_consumed_tokens.outcome: invalid outcome ${value}`);
}

function sortedUniqueRoles(values: readonly ApprovalRole[]): readonly ApprovalRole[] {
  return [...new Set(values)].sort(compareRoles);
}

function compareRoles(a: ApprovalRole, b: ApprovalRole): number {
  return roleOrder(a) - roleOrder(b);
}

function roleOrder(role: ApprovalRole): number {
  return role === "viewer" ? 0 : role === "approver" ? 1 : 2;
}

function classifySqliteWriteError(err: unknown, operation: string): Error {
  if (isSqliteFullError(err)) {
    return new WebAuthnStoreFullError(`${operation}: database full (SQLITE_FULL)`);
  }
  if (isSqliteBusyError(err)) {
    return new WebAuthnStoreBusyError(`${operation}: database busy (SQLITE_BUSY/LOCKED)`);
  }
  if (isSqliteUnavailableError(err)) {
    return new WebAuthnStoreUnavailableError(
      `${operation}: storage error (${
        err instanceof BetterSqlite3.SqliteError ? err.code : "unknown"
      })`,
    );
  }
  return err instanceof Error ? err : new Error(String(err));
}

function classifySqliteReadError(err: unknown, operation: string): Error {
  if (
    err instanceof WebAuthnStoreBusyError ||
    err instanceof WebAuthnStoreFullError ||
    err instanceof WebAuthnStoreUnavailableError
  ) {
    return err;
  }
  if (isSqliteFullError(err)) {
    return new WebAuthnStoreFullError(`${operation}: database full (SQLITE_FULL)`);
  }
  if (isSqliteBusyError(err)) {
    return new WebAuthnStoreBusyError(`${operation}: database busy (SQLITE_BUSY/LOCKED)`);
  }
  return new WebAuthnStoreUnavailableError(
    `${operation}: storage error (${err instanceof Error ? err.message : String(err)})`,
  );
}

function isSqliteFullError(err: unknown): boolean {
  return err instanceof BetterSqlite3.SqliteError && err.code === "SQLITE_FULL";
}

function isSqliteBusyError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
  return (
    err.code === "SQLITE_BUSY" ||
    err.code === "SQLITE_LOCKED" ||
    err.code.startsWith("SQLITE_BUSY_") ||
    err.code.startsWith("SQLITE_LOCKED_")
  );
}

function isSqliteUnavailableError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
  return (
    err.code === "SQLITE_READONLY" ||
    err.code === "SQLITE_CANTOPEN" ||
    err.code === "SQLITE_CORRUPT" ||
    err.code === "SQLITE_NOTADB" ||
    err.code === "SQLITE_PERM" ||
    err.code.startsWith("SQLITE_READONLY_") ||
    err.code.startsWith("SQLITE_IOERR") ||
    err.code.startsWith("SQLITE_CANTOPEN_") ||
    err.code.startsWith("SQLITE_CORRUPT_")
  );
}

export function __classifyWebAuthnSqliteWriteErrorForTesting(err: unknown): Error {
  return classifySqliteWriteError(err, "SqliteWebAuthnStore.test");
}

export function __classifyWebAuthnSqliteReadErrorForTesting(err: unknown): Error {
  return classifySqliteReadError(err, "SqliteWebAuthnStore.test");
}

import {
  type AgentId,
  type ApprovalRole,
  type ApprovalTokenId,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  asAgentId,
  asApprovalTokenId,
  asSha256Hex,
  asTimestampMs,
  canonicalJSON,
  type JsonValue,
  type Sha256Hex,
  type TimestampMs,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import { type OpenDatabaseArgs, openDatabase, runMigrations } from "../event-log/index.ts";
import type {
  ConsumeCosignChallengeArgs,
  ConsumedWebAuthnTokenRecord,
  CosignChallengeRecord,
  RegisteredWebAuthnCredential,
  RegistrationChallengeRecord,
  SaveCosignChallengeArgs,
  SaveCredentialArgs,
  SaveRegistrationChallengeArgs,
  WebAuthnChallengeRecord,
  WebAuthnStore,
  WebAuthnTokenOutcome,
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
type UpdateCredentialCounterParams = [number, string];
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

const APPROVAL_ROLE_SET: ReadonlySet<string> = new Set(["viewer", "approver", "host"]);
const TOKEN_OUTCOME_SET: ReadonlySet<string> = new Set(["approval_pending", "approved"]);

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
  private readonly saveCredentialTransaction: Database.Transaction<
    (args: SaveCredentialArgs) => void
  >;
  private readonly consumeCosignTransaction: Database.Transaction<
    (args: ConsumeCosignChallengeArgs) => ConsumedWebAuthnTokenRecord | null
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
      `SELECT token_id AS tokenId,
              challenge_id AS challengeId,
              outcome,
              response_json AS responseJson,
              role,
              approval_group_hash AS approvalGroupHash,
              agent_id AS agentId,
              expires_at_ms AS expiresAtMs,
              consumed_at_ms AS consumedAtMs
       FROM webauthn_consumed_tokens
       WHERE token_id = ?`,
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
      "UPDATE webauthn_registered_credentials SET sign_count = ? WHERE credential_id = ?",
    );
    this.insertConsumedTokenStmt = db.prepare<InsertConsumedTokenParams>(
      `INSERT INTO webauthn_consumed_tokens
        (token_id, challenge_id, outcome, response_json, role, approval_group_hash,
         agent_id, expires_at_ms, consumed_at_ms)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
      const existing = this.getConsumedTokenStmt.get(args.tokenId);
      if (existing !== undefined) return consumedTokenFromRow(existing);
      const result = this.markChallengeConsumedStmt.run(args.consumedAtMs, args.challengeId);
      if (result.changes !== 1) {
        throw new Error("webauthn cosign challenge was already consumed");
      }
      this.updateCredentialCounterStmt.run(args.newSignCount, args.credentialId);
      this.insertConsumedTokenStmt.run(
        args.tokenId,
        args.challengeId,
        args.outcome,
        canonicalJSON(args.responseJson),
        args.role,
        args.approvalGroupHash,
        args.issuedToAgentId,
        args.expiresAtMs,
        args.consumedAtMs,
      );
      return null;
    });
  }

  async saveRegistrationChallenge(args: SaveRegistrationChallengeArgs): Promise<void> {
    this.assertOpen();
    this.insertRegistrationChallengeStmt.run(
      args.challengeId,
      args.challenge,
      args.role,
      args.issuedToAgentId,
      args.expiresAtMs,
      args.createdAtMs,
    );
  }

  async saveCosignChallenge(args: SaveCosignChallengeArgs): Promise<void> {
    this.assertOpen();
    this.insertCosignChallengeStmt.run(
      args.challengeId,
      args.challenge,
      args.tokenId,
      args.scope.role,
      args.issuedToAgentId,
      canonicalJSON(approvalClaimToJsonValue(args.claim)),
      canonicalJSON(approvalScopeToJsonValue(args.scope)),
      args.claimScopeHash,
      args.approvalGroupHash,
      args.notBeforeMs,
      args.expiresAtMs,
      args.createdAtMs,
    );
  }

  async getChallenge(challengeId: string): Promise<WebAuthnChallengeRecord | null> {
    this.assertOpen();
    const row = this.getChallengeStmt.get(challengeId);
    return row === undefined ? null : challengeFromRow(row);
  }

  async listCredentialsForAgent(
    agentId: AgentId,
  ): Promise<readonly RegisteredWebAuthnCredential[]> {
    this.assertOpen();
    return this.listCredentialsForAgentStmt.all(agentId).map(credentialFromRow);
  }

  async listCredentialsForAgentRole(args: {
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
  }): Promise<readonly RegisteredWebAuthnCredential[]> {
    this.assertOpen();
    return this.listCredentialsForAgentRoleStmt.all(args.agentId, args.role).map(credentialFromRow);
  }

  async getCredential(credentialId: string): Promise<RegisteredWebAuthnCredential | null> {
    this.assertOpen();
    const row = this.getCredentialStmt.get(credentialId);
    return row === undefined ? null : credentialFromRow(row);
  }

  async saveCredential(args: SaveCredentialArgs): Promise<void> {
    this.assertOpen();
    this.saveCredentialTransaction.immediate(args);
  }

  async getConsumedToken(tokenId: ApprovalTokenId): Promise<ConsumedWebAuthnTokenRecord | null> {
    this.assertOpen();
    const row = this.getConsumedTokenStmt.get(tokenId);
    return row === undefined ? null : consumedTokenFromRow(row);
  }

  async listSatisfiedRoles(args: {
    readonly approvalGroupHash: Sha256Hex;
    readonly issuedToAgentId: AgentId;
    readonly nowMs: number;
  }): Promise<readonly ApprovalRole[]> {
    this.assertOpen();
    return this.listSatisfiedRolesStmt
      .all(args.approvalGroupHash, args.issuedToAgentId, args.nowMs)
      .map((row) => roleFromString(row.role, "webauthn_consumed_tokens.role"));
  }

  async consumeCosignChallenge(
    args: ConsumeCosignChallengeArgs,
  ): Promise<ConsumedWebAuthnTokenRecord | null> {
    this.assertOpen();
    return this.consumeCosignTransaction.immediate(args);
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
      throw new Error("SqliteWebAuthnStore is closed");
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

function roleFromString(value: string, path: string): ApprovalRole {
  if (APPROVAL_ROLE_SET.has(value)) {
    return value as ApprovalRole;
  }
  throw new Error(`${path}: invalid approval role`);
}

function outcomeFromString(value: string): WebAuthnTokenOutcome {
  if (TOKEN_OUTCOME_SET.has(value)) {
    return value as WebAuthnTokenOutcome;
  }
  throw new Error(`webauthn_consumed_tokens.outcome: invalid outcome ${value}`);
}

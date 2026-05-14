import {
  type AgentId,
  type AgentProviderRouting,
  type AgentProviderRoutingEntry,
  type CredentialScope,
  isAgentId,
  isCredentialScope,
  isProviderKind,
  isRunnerKind,
  MAX_AGENT_PROVIDER_ROUTES,
  type ProviderKind,
  RUNNER_KIND_VALUES,
  type RunnerKind,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import { type OpenDatabaseArgs, openDatabase, runMigrations } from "../event-log/index.ts";
import type { AgentProviderRoutingStore } from "./types.ts";

export interface SqliteAgentProviderRoutingStoreConfig extends OpenDatabaseArgs {}

interface SqliteAgentProviderRoutingStoreOptions {
  readonly closeDatabase?: boolean;
}

interface RoutingEntryDbRow {
  readonly kind: string;
  readonly credentialScope: string;
  readonly providerKind: string;
}

type InsertRouteParams = [AgentId, RunnerKind, CredentialScope, ProviderKind];

const KIND_ORDER: ReadonlyMap<RunnerKind, number> = new Map(
  RUNNER_KIND_VALUES.map((kind, index) => [kind, index] as const),
);

export class SqliteAgentProviderRoutingStore implements AgentProviderRoutingStore {
  private readonly closeDatabase: boolean;
  private readonly listRoutesStmt: Database.Statement<[AgentId], RoutingEntryDbRow>;
  private readonly getEntryStmt: Database.Statement<[AgentId, RunnerKind], RoutingEntryDbRow>;
  private readonly deleteAgentStmt: Database.Statement<[AgentId]>;
  private readonly insertRouteStmt: Database.Statement<InsertRouteParams>;
  private readonly putTransaction: Database.Transaction<(config: AgentProviderRouting) => void>;
  private closed = false;

  static open(config: SqliteAgentProviderRoutingStoreConfig): SqliteAgentProviderRoutingStore {
    const db = openDatabase(config);
    try {
      runMigrations(db);
      return new SqliteAgentProviderRoutingStore(db, { closeDatabase: true });
    } catch (err) {
      db.close();
      throw err;
    }
  }

  constructor(
    private readonly db: Database.Database,
    options: SqliteAgentProviderRoutingStoreOptions = {},
  ) {
    this.closeDatabase = options.closeDatabase ?? false;
    this.listRoutesStmt = db.prepare<[AgentId], RoutingEntryDbRow>(
      `SELECT runner_kind AS kind,
              credential_scope AS credentialScope,
              provider_kind AS providerKind
       FROM agent_provider_routing
       WHERE agent_id = ?`,
    );
    this.getEntryStmt = db.prepare<[AgentId, RunnerKind], RoutingEntryDbRow>(
      `SELECT runner_kind AS kind,
              credential_scope AS credentialScope,
              provider_kind AS providerKind
       FROM agent_provider_routing
       WHERE agent_id = ? AND runner_kind = ?`,
    );
    this.deleteAgentStmt = db.prepare<[AgentId]>(
      "DELETE FROM agent_provider_routing WHERE agent_id = ?",
    );
    this.insertRouteStmt = db.prepare<InsertRouteParams>(
      `INSERT INTO agent_provider_routing
         (agent_id, runner_kind, credential_scope, provider_kind)
       VALUES (?, ?, ?, ?)`,
    );
    this.putTransaction = db.transaction((config: AgentProviderRouting) => {
      this.deleteAgentStmt.run(config.agentId);
      for (const route of config.routes) {
        this.insertRouteStmt.run(
          config.agentId,
          route.kind,
          route.credentialScope,
          route.providerKind,
        );
      }
    });
  }

  async get(agentId: AgentId): Promise<AgentProviderRouting> {
    this.assertOpen();
    const validAgentId = validateAgentId(agentId, "agentProviderRoutingStore.get.agentId");
    const routes = this.listRoutesStmt.all(validAgentId).map(rowToEntry).sort(compareEntriesByKind);
    return { agentId: validAgentId, routes };
  }

  async getEntry(
    agentId: AgentId,
    kind: RunnerKind,
  ): Promise<{
    readonly credentialScope: CredentialScope;
    readonly providerKind: ProviderKind;
  } | null> {
    this.assertOpen();
    const validAgentId = validateAgentId(agentId, "agentProviderRoutingStore.getEntry.agentId");
    const validKind = validateRunnerKind(kind, "agentProviderRoutingStore.getEntry.kind");
    const row = this.getEntryStmt.get(validAgentId, validKind);
    if (row === undefined) {
      return null;
    }
    const entry = rowToEntry(row);
    return {
      credentialScope: entry.credentialScope,
      providerKind: entry.providerKind,
    };
  }

  async put(config: AgentProviderRouting): Promise<void> {
    this.assertOpen();
    this.putTransaction.immediate(validateConfig(config));
  }

  close(): void {
    if (this.closed) {
      return;
    }
    if (this.closeDatabase) {
      this.db.close();
    }
    this.closed = true;
  }

  private assertOpen(): void {
    if (this.closed) {
      throw new Error("SqliteAgentProviderRoutingStore is closed");
    }
  }
}

export function createAgentProviderRoutingStore(db: Database.Database): AgentProviderRoutingStore {
  return new SqliteAgentProviderRoutingStore(db);
}

function validateConfig(config: AgentProviderRouting): AgentProviderRouting {
  const agentId = validateAgentId(config.agentId, "agentProviderRouting.agentId");
  if (!Array.isArray(config.routes)) {
    throw new Error("agentProviderRouting.routes: must be an array");
  }
  if (config.routes.length > MAX_AGENT_PROVIDER_ROUTES) {
    throw new Error(
      `agentProviderRouting.routes: exceeds ${MAX_AGENT_PROVIDER_ROUTES} entries (got ${config.routes.length})`,
    );
  }

  const seenKinds = new Set<RunnerKind>();
  const routes: AgentProviderRoutingEntry[] = [];
  for (let index = 0; index < config.routes.length; index += 1) {
    const route = config.routes[index];
    if (route === undefined) {
      throw new Error(`agentProviderRouting.routes/${index}: is required`);
    }
    const kind = validateRunnerKind(route.kind, `agentProviderRouting.routes/${index}.kind`);
    if (seenKinds.has(kind)) {
      throw new Error(`agentProviderRouting.routes/${index}.kind: duplicate route for "${kind}"`);
    }
    seenKinds.add(kind);
    routes.push({
      kind,
      credentialScope: validateCredentialScope(
        route.credentialScope,
        `agentProviderRouting.routes/${index}.credentialScope`,
      ),
      providerKind: validateProviderKind(
        route.providerKind,
        `agentProviderRouting.routes/${index}.providerKind`,
      ),
    });
  }

  return { agentId, routes };
}

function rowToEntry(row: RoutingEntryDbRow): AgentProviderRoutingEntry {
  return {
    kind: validateRunnerKind(row.kind, "agent_provider_routing.runner_kind"),
    credentialScope: validateCredentialScope(
      row.credentialScope,
      "agent_provider_routing.credential_scope",
    ),
    providerKind: validateProviderKind(row.providerKind, "agent_provider_routing.provider_kind"),
  };
}

function compareEntriesByKind(a: AgentProviderRoutingEntry, b: AgentProviderRoutingEntry): number {
  return (KIND_ORDER.get(a.kind) ?? 0) - (KIND_ORDER.get(b.kind) ?? 0);
}

function validateAgentId(value: unknown, path: string): AgentId {
  if (isAgentId(value)) {
    return value;
  }
  throw new Error(`${path}: not an AgentId`);
}

function validateRunnerKind(value: unknown, path: string): RunnerKind {
  if (isRunnerKind(value)) {
    return value;
  }
  throw new Error(`${path}: not a supported RunnerKind`);
}

function validateCredentialScope(value: unknown, path: string): CredentialScope {
  if (isCredentialScope(value)) {
    return value;
  }
  throw new Error(`${path}: not a supported CredentialScope`);
}

function validateProviderKind(value: unknown, path: string): ProviderKind {
  if (isProviderKind(value)) {
    return value;
  }
  throw new Error(`${path}: not a supported ProviderKind`);
}

import { chmodSync, existsSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";

import { type BrokerHandle, type BrokerLogger, createBroker } from "@wuphf/broker";
import type { SqliteReceiptStore } from "@wuphf/broker/sqlite";
import type { SqliteWebAuthnStore } from "@wuphf/broker/webauthn";
import { type AgentId, type ApiToken, type ApprovalRole, asAgentId } from "@wuphf/protocol";

export const RENDERER_DIST_ENV = "WUPHF_RENDERER_DIST";
export const DEV_RENDERER_ORIGIN_ENV = "WUPHF_DEV_RENDERER_ORIGIN";
export const RECEIPT_STORE_PATH_ENV = "WUPHF_RECEIPT_STORE_PATH";
export const WEBAUTHN_STORE_PATH_ENV = "WUPHF_WEBAUTHN_STORE_PATH";

// Desktop v1 is a single-user trusted-LAN install. Until the desktop grows a
// real agent-identity model, the bootstrap bearer represents the local human
// operator and can enroll cosign-capable WebAuthn credentials.
export const DESKTOP_OPERATOR_AGENT_ID = asAgentId("operator");

const DESKTOP_OPERATOR_ENROLLABLE_ROLES = [
  "viewer",
  "approver",
  "host",
] as const satisfies readonly ApprovalRole[];

const DESKTOP_OPERATOR_ENROLLABLE_ROLES_BY_AGENT: ReadonlyMap<AgentId, readonly ApprovalRole[]> =
  new Map([[DESKTOP_OPERATOR_AGENT_ID, DESKTOP_OPERATOR_ENROLLABLE_ROLES]]);

export interface DesktopBrokerRuntime {
  readonly broker: BrokerHandle;
  close(): Promise<void>;
}

interface WebAuthnRuntimeConfig {
  readonly store: SqliteWebAuthnStore;
  readonly rpName: string;
  readonly rpId: string;
}

export async function startDesktopBrokerFromEnv(args: {
  readonly env: NodeJS.ProcessEnv;
  readonly logger: BrokerLogger;
}): Promise<DesktopBrokerRuntime> {
  const { env, logger } = args;
  const rendererDir = env[RENDERER_DIST_ENV];
  if (typeof rendererDir === "string" && rendererDir.length > 0 && !existsSync(rendererDir)) {
    throw new Error(`renderer dist directory does not exist: ${rendererDir}`);
  }
  const renderer =
    typeof rendererDir === "string" && rendererDir.length > 0 ? { dir: rendererDir } : null;

  const devOrigin = env[DEV_RENDERER_ORIGIN_ENV];
  const trustedOrigins =
    typeof devOrigin === "string" && devOrigin.length > 0 ? [devOrigin] : undefined;

  let receiptStore: SqliteReceiptStore | null = null;
  let webauthn: WebAuthnRuntimeConfig | null = null;

  try {
    receiptStore = await openReceiptStoreFromEnv(env);
    webauthn = await openWebAuthnStoreFromEnv(env);

    const tokenAgentIds = new Map<ApiToken, AgentId>();
    const broker = await createBroker({
      port: 0,
      renderer,
      logger,
      ...(trustedOrigins !== undefined ? { trustedOrigins } : {}),
      ...(receiptStore !== null ? { receiptStore } : {}),
      ...(webauthn !== null
        ? {
            webauthn: {
              store: webauthn.store,
              tokenAgentIds,
              enrollableRoles: DESKTOP_OPERATOR_ENROLLABLE_ROLES_BY_AGENT,
              rpName: webauthn.rpName,
              rpId: webauthn.rpId,
              allowedOrigins: trustedOrigins ?? [],
            },
          }
        : {}),
    });

    if (webauthn !== null) {
      tokenAgentIds.set(broker.token, DESKTOP_OPERATOR_AGENT_ID);
    }

    return {
      broker,
      close: async (): Promise<void> => {
        await closeDesktopBrokerRuntime(broker, receiptStore, webauthn?.store ?? null);
      },
    };
  } catch (error) {
    closeStore(receiptStore);
    closeStore(webauthn?.store ?? null);
    throw error;
  }
}

async function openReceiptStoreFromEnv(env: NodeJS.ProcessEnv): Promise<SqliteReceiptStore | null> {
  const receiptStorePath = env[RECEIPT_STORE_PATH_ENV];
  if (typeof receiptStorePath !== "string" || receiptStorePath.length === 0) return null;

  prepareSqliteStorePath(receiptStorePath);
  const { SqliteReceiptStore } = await import("@wuphf/broker/sqlite");
  const store = SqliteReceiptStore.open({ path: receiptStorePath });
  tightenSqliteStorePermissions(receiptStorePath);
  return store;
}

async function openWebAuthnStoreFromEnv(
  env: NodeJS.ProcessEnv,
): Promise<WebAuthnRuntimeConfig | null> {
  const webauthnStorePath = env[WEBAUTHN_STORE_PATH_ENV];
  if (typeof webauthnStorePath !== "string" || webauthnStorePath.length === 0) return null;

  prepareSqliteStorePath(webauthnStorePath);
  const { SqliteWebAuthnStore, WEBAUTHN_RP_ID, WEBAUTHN_RP_NAME } = await import(
    "@wuphf/broker/webauthn"
  );
  const store = SqliteWebAuthnStore.open({ path: webauthnStorePath });
  tightenSqliteStorePermissions(webauthnStorePath);
  return { store, rpName: WEBAUTHN_RP_NAME, rpId: WEBAUTHN_RP_ID };
}

async function closeDesktopBrokerRuntime(
  broker: BrokerHandle,
  receiptStore: SqliteReceiptStore | null,
  webauthnStore: SqliteWebAuthnStore | null,
): Promise<void> {
  try {
    await broker.stop();
  } finally {
    closeStore(receiptStore);
    closeStore(webauthnStore);
  }
}

function closeStore(store: { close(): void } | null): void {
  if (store === null) return;
  try {
    store.close();
  } catch {
    // Shutdown is best effort. A close failure must not hide the original
    // startup error or keep the utility process alive during app quit.
  }
}

function prepareSqliteStorePath(dbPath: string): void {
  mkdirSync(dirname(dbPath), { recursive: true });
  tightenSqliteStorePermissions(dbPath);
}

function tightenSqliteStorePermissions(dbPath: string): void {
  if (process.platform === "win32") return;

  const tryChmod = (path: string, mode: number): void => {
    try {
      chmodSync(path, mode);
    } catch {
      // Some sandboxed or network filesystems reject chmod. UserData
      // isolation is the primary boundary; owner-only bits are defense in depth.
    }
  };

  tryChmod(dirname(dbPath), 0o700);
  for (const sidecar of [dbPath, `${dbPath}-wal`, `${dbPath}-shm`, `${dbPath}-journal`]) {
    if (existsSync(sidecar)) {
      tryChmod(sidecar, 0o600);
    }
  }
}

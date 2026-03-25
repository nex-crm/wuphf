/**
 * File-based session store for MCP session ID mapping.
 * Persists session mappings to ~/.wuphf/mcp-sessions.json.
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

export interface SessionStoreConfig {
  maxSize: number;
  dataDir: string;
}

const DEFAULT_MAX = 100;
const DEFAULT_DATA_DIR = join(homedir(), ".wuphf");

export class SessionStore {
  private filePath: string;
  private maxSize: number;

  constructor(config?: Partial<SessionStoreConfig>) {
    const dataDir = config?.dataDir ?? DEFAULT_DATA_DIR;
    this.maxSize = config?.maxSize ?? DEFAULT_MAX;
    this.filePath = join(dataDir, "mcp-sessions.json");
    mkdirSync(dataDir, { recursive: true });
  }

  private readStore(): Record<string, string> {
    try {
      const raw = readFileSync(this.filePath, "utf-8");
      const data = JSON.parse(raw);
      if (data && typeof data === "object" && !Array.isArray(data)) {
        return data;
      }
      return {};
    } catch {
      return {};
    }
  }

  private writeStore(store: Record<string, string>): void {
    try {
      writeFileSync(this.filePath, JSON.stringify(store), "utf-8");
    } catch {
      // Best-effort
    }
  }

  get(sessionKey: string): string | undefined {
    const store = this.readStore();
    return store[sessionKey];
  }

  set(sessionKey: string, nexSessionId: string): void {
    const store = this.readStore();
    store[sessionKey] = nexSessionId;
    const keys = Object.keys(store);
    while (keys.length > this.maxSize) {
      const oldest = keys.shift()!;
      delete store[oldest];
    }
    this.writeStore(store);
  }

  delete(sessionKey: string): boolean {
    const store = this.readStore();
    if (sessionKey in store) {
      delete store[sessionKey];
      this.writeStore(store);
      return true;
    }
    return false;
  }

  get size(): number {
    return Object.keys(this.readStore()).length;
  }

  clear(): void {
    this.writeStore({});
  }
}

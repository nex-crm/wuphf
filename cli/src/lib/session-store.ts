import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const DEFAULT_MAX = 100;
const DEFAULT_DATA_DIR = join(homedir(), ".wuphf");

export class SessionStore {
  private filePath: string;
  private maxSize: number;

  constructor(config?: { maxSize?: number; dataDir?: string }) {
    const dataDir = config?.dataDir ?? DEFAULT_DATA_DIR;
    this.maxSize = config?.maxSize ?? DEFAULT_MAX;
    this.filePath = join(dataDir, "cli-sessions.json");
    mkdirSync(dataDir, { recursive: true });
  }

  private readStore(): Record<string, string> {
    try {
      const data = JSON.parse(readFileSync(this.filePath, "utf-8"));
      if (data && typeof data === "object" && !Array.isArray(data))
        return data as Record<string, string>;
      return {};
    } catch {
      return {};
    }
  }

  private writeStore(store: Record<string, string>): void {
    try {
      writeFileSync(this.filePath, JSON.stringify(store), "utf-8");
    } catch {
      /* best-effort */
    }
  }

  get(key: string): string | undefined {
    return this.readStore()[key];
  }

  set(key: string, value: string): void {
    const store = this.readStore();
    store[key] = value;
    const keys = Object.keys(store);
    while (keys.length > this.maxSize) {
      delete store[keys.shift()!];
    }
    this.writeStore(store);
  }

  delete(key: string): boolean {
    const store = this.readStore();
    if (key in store) {
      delete store[key];
      this.writeStore(store);
      return true;
    }
    return false;
  }

  list(): Record<string, string> {
    return this.readStore();
  }

  clear(): void {
    this.writeStore({});
  }
}

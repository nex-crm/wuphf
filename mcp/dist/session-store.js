/**
 * File-based session store for MCP session ID mapping.
 * Persists session mappings to ~/.wuphf/mcp-sessions.json.
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
const DEFAULT_MAX = 100;
const DEFAULT_DATA_DIR = join(homedir(), ".wuphf");
export class SessionStore {
    filePath;
    maxSize;
    constructor(config) {
        const dataDir = config?.dataDir ?? DEFAULT_DATA_DIR;
        this.maxSize = config?.maxSize ?? DEFAULT_MAX;
        this.filePath = join(dataDir, "mcp-sessions.json");
        mkdirSync(dataDir, { recursive: true });
    }
    readStore() {
        try {
            const raw = readFileSync(this.filePath, "utf-8");
            const data = JSON.parse(raw);
            if (data && typeof data === "object" && !Array.isArray(data)) {
                return data;
            }
            return {};
        }
        catch {
            return {};
        }
    }
    writeStore(store) {
        try {
            writeFileSync(this.filePath, JSON.stringify(store), "utf-8");
        }
        catch {
            // Best-effort
        }
    }
    get(sessionKey) {
        const store = this.readStore();
        return store[sessionKey];
    }
    set(sessionKey, nexSessionId) {
        const store = this.readStore();
        store[sessionKey] = nexSessionId;
        const keys = Object.keys(store);
        while (keys.length > this.maxSize) {
            const oldest = keys.shift();
            delete store[oldest];
        }
        this.writeStore(store);
    }
    delete(sessionKey) {
        const store = this.readStore();
        if (sessionKey in store) {
            delete store[sessionKey];
            this.writeStore(store);
            return true;
        }
        return false;
    }
    get size() {
        return Object.keys(this.readStore()).length;
    }
    clear() {
        this.writeStore({});
    }
}
//# sourceMappingURL=session-store.js.map
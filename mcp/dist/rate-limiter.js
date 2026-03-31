/**
 * File-based sliding window rate limiter.
 * Designed for WUPHF /text endpoint (10 req/min).
 *
 * Persists timestamps to a JSON file so rate limits are respected
 * across invocations.
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
const DEFAULTS = {
    maxRequests: 10,
    windowMs: 60_000,
    dataDir: join(homedir(), ".wuphf"),
};
export class RateLimiter {
    config;
    filePath;
    constructor(config) {
        this.config = { ...DEFAULTS, ...config };
        this.filePath = join(this.config.dataDir, "rate-limiter.json");
        mkdirSync(this.config.dataDir, { recursive: true });
    }
    readTimestamps() {
        try {
            const raw = readFileSync(this.filePath, "utf-8");
            const data = JSON.parse(raw);
            if (Array.isArray(data))
                return data;
            return [];
        }
        catch {
            return [];
        }
    }
    writeTimestamps(timestamps) {
        try {
            writeFileSync(this.filePath, JSON.stringify(timestamps), "utf-8");
        }
        catch {
            // Best-effort
        }
    }
    canProceed() {
        const now = Date.now();
        const timestamps = this.readTimestamps().filter((t) => now - t < this.config.windowMs);
        if (timestamps.length >= this.config.maxRequests) {
            this.writeTimestamps(timestamps);
            return false;
        }
        return true;
    }
    recordRequest() {
        const now = Date.now();
        const timestamps = this.readTimestamps().filter((t) => now - t < this.config.windowMs);
        timestamps.push(now);
        this.writeTimestamps(timestamps);
    }
}
//# sourceMappingURL=rate-limiter.js.map
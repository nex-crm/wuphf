/**
 * File-based sliding window rate limiter.
 * Designed for Nex /text endpoint (10 req/min).
 *
 * Persists timestamps to a JSON file so rate limits are respected
 * across invocations.
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

export interface RateLimiterConfig {
  maxRequests: number;
  windowMs: number;
  dataDir: string;
}

const DEFAULTS: RateLimiterConfig = {
  maxRequests: 10,
  windowMs: 60_000,
  dataDir: join(homedir(), ".nex"),
};

export class RateLimiter {
  private config: RateLimiterConfig;
  private filePath: string;

  constructor(config?: Partial<RateLimiterConfig>) {
    this.config = { ...DEFAULTS, ...config };
    this.filePath = join(this.config.dataDir, "rate-limiter.json");
    mkdirSync(this.config.dataDir, { recursive: true });
  }

  private readTimestamps(): number[] {
    try {
      const raw = readFileSync(this.filePath, "utf-8");
      const data = JSON.parse(raw);
      if (Array.isArray(data)) return data;
      return [];
    } catch {
      return [];
    }
  }

  private writeTimestamps(timestamps: number[]): void {
    try {
      writeFileSync(this.filePath, JSON.stringify(timestamps), "utf-8");
    } catch {
      // Best-effort
    }
  }

  canProceed(): boolean {
    const now = Date.now();
    const timestamps = this.readTimestamps().filter((t) => now - t < this.config.windowMs);
    if (timestamps.length >= this.config.maxRequests) {
      this.writeTimestamps(timestamps);
      return false;
    }
    timestamps.push(now);
    this.writeTimestamps(timestamps);
    return true;
  }
}

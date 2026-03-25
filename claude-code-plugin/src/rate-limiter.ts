/**
 * File-based sliding window rate limiter.
 * Designed for WUPHF /text endpoint (10 req/min).
 *
 * Since Claude Code hooks are short-lived processes (start, run, exit),
 * in-memory state is lost between invocations. This implementation persists
 * timestamps to a JSON file so rate limits are respected across invocations.
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
  dataDir: join(homedir(), ".wuphf"),
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
      // Best-effort — if we can't write, proceed without rate limiting
    }
  }

  /**
   * Check if a request can proceed, and if so, record the timestamp.
   * Returns true if the request is allowed, false if rate limited.
   */
  canProceed(): boolean {
    const now = Date.now();
    const timestamps = this.readTimestamps().filter(
      (t) => now - t < this.config.windowMs
    );

    if (timestamps.length >= this.config.maxRequests) {
      // Write back pruned timestamps
      this.writeTimestamps(timestamps);
      return false;
    }

    timestamps.push(now);
    this.writeTimestamps(timestamps);
    return true;
  }

  get pending(): number {
    return 0;
  }
}

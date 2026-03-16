import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { RateLimiter } from "../../src/lib/rate-limiter.ts";

describe("RateLimiter", () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "nex-rl-test-"));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test("canProceed returns true when under limit", () => {
    const limiter = new RateLimiter({ maxRequests: 5, windowMs: 60_000, dataDir: tmpDir });
    expect(limiter.canProceed()).toBe(true);
    expect(limiter.canProceed()).toBe(true);
  });

  test("canProceed returns false when at limit", () => {
    const limiter = new RateLimiter({ maxRequests: 2, windowMs: 60_000, dataDir: tmpDir });
    expect(limiter.canProceed()).toBe(true);
    expect(limiter.canProceed()).toBe(true);
    expect(limiter.canProceed()).toBe(false);
  });

  test("old timestamps are pruned outside window", () => {
    const limiter = new RateLimiter({ maxRequests: 1, windowMs: 1, dataDir: tmpDir });
    expect(limiter.canProceed()).toBe(true);
    // Wait for window to expire
    const start = Date.now();
    while (Date.now() - start < 5) { /* spin */ }
    expect(limiter.canProceed()).toBe(true);
  });
});

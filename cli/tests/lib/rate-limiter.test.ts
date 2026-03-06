import { describe, it, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { RateLimiter } from "../../src/lib/rate-limiter.js";

describe("RateLimiter", () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "nex-rl-test-"));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it("canProceed returns true when under limit", () => {
    const limiter = new RateLimiter({ maxRequests: 5, windowMs: 60_000, dataDir: tmpDir });
    assert.equal(limiter.canProceed(), true);
    assert.equal(limiter.canProceed(), true);
  });

  it("canProceed returns false when at limit", () => {
    const limiter = new RateLimiter({ maxRequests: 2, windowMs: 60_000, dataDir: tmpDir });
    assert.equal(limiter.canProceed(), true);
    assert.equal(limiter.canProceed(), true);
    assert.equal(limiter.canProceed(), false);
  });

  it("old timestamps are pruned outside window", () => {
    const limiter = new RateLimiter({ maxRequests: 1, windowMs: 1, dataDir: tmpDir });
    assert.equal(limiter.canProceed(), true);
    // Wait for window to expire
    const start = Date.now();
    while (Date.now() - start < 5) { /* spin */ }
    assert.equal(limiter.canProceed(), true);
  });
});

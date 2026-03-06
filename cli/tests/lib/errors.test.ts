import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { AuthError, RateLimitError, ServerError } from "../../src/lib/errors.js";

describe("AuthError", () => {
  it("has correct name and exitCode", () => {
    const err = new AuthError("custom msg");
    assert.equal(err.name, "AuthError");
    assert.equal(err.message, "custom msg");
    assert.equal(err.exitCode, 2);
    assert.ok(err instanceof Error);
  });

  it("uses default message when none provided", () => {
    const err = new AuthError();
    assert.ok(err.message.includes("No API key configured"));
  });
});

describe("RateLimitError", () => {
  it("has correct name, exitCode, and retryAfterMs", () => {
    const err = new RateLimitError(5000);
    assert.equal(err.name, "RateLimitError");
    assert.equal(err.exitCode, 1);
    assert.equal(err.retryAfterMs, 5000);
    assert.ok(err.message.includes("5s"));
  });

  it("defaults retryAfterMs to 60000", () => {
    const err = new RateLimitError();
    assert.equal(err.retryAfterMs, 60_000);
  });
});

describe("ServerError", () => {
  it("has correct name, exitCode, and status", () => {
    const err = new ServerError(500, "Internal");
    assert.equal(err.name, "ServerError");
    assert.equal(err.exitCode, 1);
    assert.equal(err.status, 500);
    assert.ok(err.message.includes("500"));
    assert.ok(err.message.includes("Internal"));
  });

  it("works without body", () => {
    const err = new ServerError(404);
    assert.equal(err.status, 404);
    assert.ok(err.message.includes("404"));
  });
});

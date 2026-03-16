import { describe, test, expect } from "bun:test";
import { AuthError, RateLimitError, ServerError } from "../../src/lib/errors.ts";

describe("AuthError", () => {
  test("has correct name and exitCode", () => {
    const err = new AuthError("custom msg");
    expect(err.name).toBe("AuthError");
    expect(err.message).toBe("custom msg");
    expect(err.exitCode).toBe(2);
    expect(err instanceof Error).toBeTruthy();
  });

  test("uses default message when none provided", () => {
    const err = new AuthError();
    expect(err.message.includes("No API key configured")).toBeTruthy();
  });
});

describe("RateLimitError", () => {
  test("has correct name, exitCode, and retryAfterMs", () => {
    const err = new RateLimitError(5000);
    expect(err.name).toBe("RateLimitError");
    expect(err.exitCode).toBe(1);
    expect(err.retryAfterMs).toBe(5000);
    expect(err.message.includes("5s")).toBeTruthy();
  });

  test("defaults retryAfterMs to 60000", () => {
    const err = new RateLimitError();
    expect(err.retryAfterMs).toBe(60_000);
  });
});

describe("ServerError", () => {
  test("has correct name, exitCode, and status", () => {
    const err = new ServerError(500, "Internal");
    expect(err.name).toBe("ServerError");
    expect(err.exitCode).toBe(1);
    expect(err.status).toBe(500);
    expect(err.message.includes("500")).toBeTruthy();
    expect(err.message.includes("Internal")).toBeTruthy();
  });

  test("works without body", () => {
    const err = new ServerError(404);
    expect(err.status).toBe(404);
    expect(err.message.includes("404")).toBeTruthy();
  });
});

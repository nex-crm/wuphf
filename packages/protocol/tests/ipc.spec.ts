import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { ALLOWED_LOOPBACK_HOSTS, isAllowedLoopbackHost } from "../src/ipc.ts";

describe("isAllowedLoopbackHost", () => {
  it("accepts canonical loopback hosts", () => {
    for (const h of ALLOWED_LOOPBACK_HOSTS) {
      expect(isAllowedLoopbackHost(h)).toBe(true);
    }
  });

  it("accepts loopback host with port", () => {
    expect(isAllowedLoopbackHost("127.0.0.1:8080")).toBe(true);
    expect(isAllowedLoopbackHost("localhost:3000")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:8080")).toBe(true);
  });

  it("rejects rebound hosts", () => {
    expect(isAllowedLoopbackHost("evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("0.0.0.0")).toBe(false);
    expect(isAllowedLoopbackHost("localhost.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("169.254.169.254")).toBe(false);
    expect(isAllowedLoopbackHost("10.0.0.1")).toBe(false);
    expect(isAllowedLoopbackHost("192.168.1.1")).toBe(false);
  });

  it("rejects empty and obviously malformed hosts", () => {
    expect(isAllowedLoopbackHost("")).toBe(false);
    expect(isAllowedLoopbackHost(":")).toBe(false);
  });

  it("property: any non-allowlisted host is rejected", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 32 }).filter((s) => {
          const lower = s.toLowerCase();
          return (
            !lower.includes("127.0.0.1") &&
            !lower.includes("localhost") &&
            !lower.includes("::1") &&
            !lower.includes(":")
          );
        }),
        (host) => isAllowedLoopbackHost(host) === false,
      ),
      { numRuns: 500 },
    );
  });
});

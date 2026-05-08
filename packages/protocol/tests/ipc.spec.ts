import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  ALLOWED_LOOPBACK_HOSTS,
  isAllowedLoopbackHost,
  isLoopbackRemoteAddress,
} from "../src/ipc.ts";

describe("isAllowedLoopbackHost", () => {
  it("accepts canonical loopback hosts", () => {
    for (const h of ALLOWED_LOOPBACK_HOSTS) {
      expect(isAllowedLoopbackHost(h)).toBe(true);
    }
  });

  it("accepts loopback host with valid port", () => {
    expect(isAllowedLoopbackHost("127.0.0.1:8080")).toBe(true);
    expect(isAllowedLoopbackHost("localhost:3000")).toBe(true);
    expect(isAllowedLoopbackHost("Localhost:3000")).toBe(true); // case-insensitive
    expect(isAllowedLoopbackHost("[::1]")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:8080")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:0")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:65535")).toBe(true);
  });

  it("rejects rebound hosts", () => {
    expect(isAllowedLoopbackHost("evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("0.0.0.0")).toBe(false);
    expect(isAllowedLoopbackHost("localhost.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("169.254.169.254")).toBe(false);
    expect(isAllowedLoopbackHost("10.0.0.1")).toBe(false);
    expect(isAllowedLoopbackHost("192.168.1.1")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.2")).toBe(false); // not 127.0.0.1
  });

  it("rejects malformed loopback-looking hosts", () => {
    expect(isAllowedLoopbackHost("[::1]junk")).toBe(false);
    expect(isAllowedLoopbackHost("::1:8080")).toBe(false); // unbracketed IPv6+port
    expect(isAllowedLoopbackHost("0:0:0:0:0:0:0:1")).toBe(false); // expanded IPv6
    expect(isAllowedLoopbackHost("localhost:abc")).toBe(false);
    expect(isAllowedLoopbackHost("localhost:")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:abc")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:65536")).toBe(false); // port out of range
    expect(isAllowedLoopbackHost("127.0.0.1:99999")).toBe(false);
    expect(isAllowedLoopbackHost("[localhost]")).toBe(false);
    expect(isAllowedLoopbackHost("[127.0.0.1]:8080")).toBe(false);
    expect(isAllowedLoopbackHost("[]")).toBe(false);
    expect(isAllowedLoopbackHost("localhost, evil.com")).toBe(false);
  });

  it("rejects empty and obviously malformed hosts", () => {
    expect(isAllowedLoopbackHost("")).toBe(false);
    expect(isAllowedLoopbackHost(":")).toBe(false);
    expect(isAllowedLoopbackHost(" 127.0.0.1")).toBe(false);
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

describe("isLoopbackRemoteAddress", () => {
  it("accepts ::1 and 127.0.0.0/8", () => {
    expect(isLoopbackRemoteAddress("::1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.255.255.255")).toBe(true);
    expect(isLoopbackRemoteAddress("127.1.2.3")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.42.42.42")).toBe(true);
  });

  it("rejects non-loopback addresses", () => {
    expect(isLoopbackRemoteAddress("0.0.0.0")).toBe(false);
    expect(isLoopbackRemoteAddress("128.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("10.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("169.254.169.254")).toBe(false);
    expect(isLoopbackRemoteAddress("::ffff:192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("fe80::1")).toBe(false);
    expect(isLoopbackRemoteAddress("")).toBe(false);
    expect(isLoopbackRemoteAddress("not-an-ip")).toBe(false);
    expect(isLoopbackRemoteAddress("127.0.0")).toBe(false); // truncated
    expect(isLoopbackRemoteAddress("127.0.0.256")).toBe(false); // out-of-range octet
  });
});

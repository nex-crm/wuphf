import { describe, expect, it } from "vitest";

import { checkLoopbackRequest } from "../src/dns-rebinding-guard.ts";

describe("checkLoopbackRequest", () => {
  it("accepts loopback host + loopback peer", () => {
    expect(
      checkLoopbackRequest({ hostHeader: "127.0.0.1:7891", remoteAddress: "127.0.0.1" }),
    ).toEqual({ allowed: true });
    expect(checkLoopbackRequest({ hostHeader: "localhost", remoteAddress: "::1" })).toEqual({
      allowed: true,
    });
  });

  it("rejects missing host", () => {
    expect(checkLoopbackRequest({ hostHeader: undefined, remoteAddress: "127.0.0.1" })).toEqual({
      allowed: false,
      reason: "missing_host",
    });
    expect(checkLoopbackRequest({ hostHeader: "", remoteAddress: "127.0.0.1" })).toEqual({
      allowed: false,
      reason: "missing_host",
    });
  });

  it("rejects non-loopback host even from loopback peer", () => {
    expect(
      checkLoopbackRequest({ hostHeader: "evil.example.com", remoteAddress: "127.0.0.1" }),
    ).toEqual({ allowed: false, reason: "bad_host" });
    // DNS-rebinding payload: 127.0.0.1.evil
    expect(
      checkLoopbackRequest({ hostHeader: "127.0.0.1.evil", remoteAddress: "127.0.0.1" }),
    ).toEqual({ allowed: false, reason: "bad_host" });
  });

  it("rejects missing peer", () => {
    expect(checkLoopbackRequest({ hostHeader: "127.0.0.1", remoteAddress: undefined })).toEqual({
      allowed: false,
      reason: "missing_peer",
    });
    expect(checkLoopbackRequest({ hostHeader: "127.0.0.1", remoteAddress: "" })).toEqual({
      allowed: false,
      reason: "missing_peer",
    });
  });

  it("rejects non-loopback peer even with loopback host", () => {
    expect(checkLoopbackRequest({ hostHeader: "127.0.0.1", remoteAddress: "10.0.0.5" })).toEqual({
      allowed: false,
      reason: "bad_peer",
    });
    expect(checkLoopbackRequest({ hostHeader: "localhost", remoteAddress: "192.168.1.7" })).toEqual(
      {
        allowed: false,
        reason: "bad_peer",
      },
    );
  });
});

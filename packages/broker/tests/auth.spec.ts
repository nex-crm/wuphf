import { asApiToken } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { extractBearerFromHeader, tokenMatches } from "../src/auth.ts";

const TOKEN = asApiToken("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA");

describe("extractBearerFromHeader", () => {
  it("returns null for non-bearer / missing values", () => {
    expect(extractBearerFromHeader(undefined)).toBeNull();
    expect(extractBearerFromHeader("")).toBeNull();
    expect(extractBearerFromHeader("Basic abc")).toBeNull();
  });

  it("strips the Bearer prefix and trims whitespace", () => {
    expect(extractBearerFromHeader("Bearer abc")).toBe("abc");
    expect(extractBearerFromHeader("Bearer  abc  ")).toBe("abc");
  });

  it("returns null when the bearer payload is empty after trimming", () => {
    expect(extractBearerFromHeader("Bearer    ")).toBeNull();
    expect(extractBearerFromHeader("Bearer ")).toBeNull();
  });
});

describe("tokenMatches", () => {
  it("returns false on null presented value", () => {
    expect(tokenMatches(null, TOKEN)).toBe(false);
  });

  it("returns false on different lengths (without crashing timingSafeEqual)", () => {
    expect(tokenMatches("short", TOKEN)).toBe(false);
  });

  it("returns true on exact match", () => {
    expect(tokenMatches(TOKEN, TOKEN)).toBe(true);
  });

  it("returns false on same-length-but-different value", () => {
    const wrong = "B".repeat(TOKEN.length);
    expect(wrong.length).toBe(TOKEN.length);
    expect(tokenMatches(wrong, TOKEN)).toBe(false);
  });
});

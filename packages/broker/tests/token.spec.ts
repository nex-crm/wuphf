import { isApiToken } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { generateApiToken } from "../src/token.ts";

describe("generateApiToken", () => {
  it("produces values that satisfy `isApiToken`", () => {
    const t = generateApiToken();
    expect(isApiToken(t)).toBe(true);
  });

  it("returns 256 bits of base64url entropy (43 chars)", () => {
    const t = generateApiToken();
    expect(t).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });

  it("does not collide across N=1024 calls", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 1024; i++) {
      seen.add(generateApiToken());
    }
    expect(seen.size).toBe(1024);
  });
});

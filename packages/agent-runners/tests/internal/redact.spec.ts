import { describe, expect, it } from "vitest";

import { createSecretRedactor } from "../../src/internal/redact.ts";

describe("createSecretRedactor", () => {
  it("redacts full secrets and common partial leaks", () => {
    const redact = createSecretRedactor("abcdefghijklmnop");

    expect(redact("full abcdefghijklmnop")).toBe("full <redacted>");
    expect(redact("first8 abcdefgh")).toBe("first8 <redacted>");
    expect(redact("first12 abcdefghijkl")).toBe("first12 <redacted>");
    expect(redact("last4 mnop")).toBe("last4 <redacted>");

    const longSecretRedact = createSecretRedactor("abcdefghijklmnopqrstuvwxyz");
    expect(longSecretRedact("first12 abcdefghijkl")).toBe("first12 <redacted>");
  });
});

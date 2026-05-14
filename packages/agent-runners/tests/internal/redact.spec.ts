import { describe, expect, it } from "vitest";

import {
  createSecretStreamingRedactor,
  createStreamingRedactor,
} from "../../src/internal/redact.ts";

describe("createStreamingRedactor", () => {
  it("redacts full secrets and common partial leaks", () => {
    expect(
      redactAll(createSecretStreamingRedactor("abcdefghijklmnop"), "full abcdefghijklmnop"),
    ).toBe("full <redacted>");
    expect(redactAll(createSecretStreamingRedactor("abcdefghijklmnop"), "first8 abcdefgh")).toBe(
      "first8 <redacted>",
    );
    expect(
      redactAll(createSecretStreamingRedactor("abcdefghijklmnop"), "first12 abcdefghijkl"),
    ).toBe("first12 <redacted>");
    expect(redactAll(createSecretStreamingRedactor("abcdefghijklmnop"), "last4 mnop")).toBe(
      "last4 <redacted>",
    );
    expect(
      redactAll(
        createSecretStreamingRedactor("abcdefghijklmnopqrstuvwxyz"),
        "first12 abcdefghijkl",
      ),
    ).toBe("first12 <redacted>");
  });

  it("redacts secrets split across chunks without redacting near misses", () => {
    const fixture = redactionFixture();
    const redactor = createStreamingRedactor([fixture]);
    const emitted = fixture
      .match(/.{1,2}/g)
      ?.map((chunk) => redactor.redact(chunk))
      .join("");

    expect(`${emitted ?? ""}${redactor.flush()}`).toBe("<redacted>");

    const nearMiss = `${fixture.slice(0, 12)}X${fixture.slice(13)}`;
    const nearMissRedactor = createStreamingRedactor([fixture]);
    const nearMissOutput = nearMiss
      .match(/.{1,2}/g)
      ?.map((chunk) => nearMissRedactor.redact(chunk))
      .join("");

    expect(`${nearMissOutput ?? ""}${nearMissRedactor.flush()}`).toBe(nearMiss);
  });

  it("flushes retained carryover safely at terminal events", () => {
    const redactor = createStreamingRedactor(["very-long-secret"]);

    expect(redactor.redact("very-")).toBe("");
    expect(redactor.redact("long")).toBe("");
    expect(redactor.flush()).toBe("very-long");
  });
});

function redactAll(
  redactor: ReturnType<typeof createSecretStreamingRedactor>,
  text: string,
): string {
  return `${redactor.redact(text)}${redactor.flush()}`;
}

function redactionFixture(): string {
  return ["redact safe", "fixture value", "token!"].join(" ");
}

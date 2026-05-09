import { describe, expect, it } from "vitest";
import { MAX_FROZEN_ARGS_BYTES } from "../src/budgets.ts";
import { asSha256Hex, isSha256Hex, sha256Hex } from "../src/sha256.ts";

describe("sha256", () => {
  it("hashes empty input to the standard SHA-256 digest", () => {
    expect(sha256Hex("")).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    expect(sha256Hex(new Uint8Array())).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    );
  });

  it("hashes strings and their UTF-8 bytes equivalently", () => {
    const input = "WUPHF \u{1f389} cafe\u0301";
    const bytes = new TextEncoder().encode(input);

    expect(sha256Hex(bytes)).toBe(sha256Hex(input));
  });

  it("hashes max-budget byte input deterministically", () => {
    const input = new Uint8Array(MAX_FROZEN_ARGS_BYTES);

    expect(sha256Hex(input)).toBe(
      "30e14955ebf1352266dc2ff8067e68104607e750abb9d3b36582b8af909fcb58",
    );
  });

  it("accepts only lowercase 64-character hex digests", () => {
    const digest = sha256Hex("abc");

    expect(isSha256Hex(digest)).toBe(true);
    expect(asSha256Hex(digest)).toBe(digest);
    expect(isSha256Hex(digest.toUpperCase())).toBe(false);
    expect(isSha256Hex(digest.slice(1))).toBe(false);
    expect(isSha256Hex(`${digest}0`)).toBe(false);
    expect(isSha256Hex(`${digest.slice(0, 63)}g`)).toBe(false);
    expect(() => asSha256Hex(digest.toUpperCase())).toThrow(/not a sha256 hex digest/);
  });
});

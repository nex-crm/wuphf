import type { Brand } from "./brand.ts";

export type Sha256Hex = Brand<string, "Sha256Hex">;

export const SHA256_HEX_RE = /^[0-9a-f]{64}$/;

export function isSha256Hex(value: unknown): value is Sha256Hex {
  return typeof value === "string" && SHA256_HEX_RE.test(value);
}

export function asSha256Hex(value: string): Sha256Hex {
  if (!SHA256_HEX_RE.test(value)) {
    throw new Error("not a sha256 hex digest");
  }
  return value as Sha256Hex;
}

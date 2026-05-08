import { createHash } from "node:crypto";
import type { Brand } from "./brand.ts";

export type Sha256Hex = Brand<string, "Sha256Hex">;

const SHA256_HEX_RE = /^[0-9a-f]{64}$/;

export function sha256Hex(input: string | Uint8Array): Sha256Hex {
  const hash = createHash("sha256");
  hash.update(input);
  return hash.digest("hex") as Sha256Hex;
}

export function isSha256Hex(value: unknown): value is Sha256Hex {
  return typeof value === "string" && SHA256_HEX_RE.test(value);
}

export function asSha256Hex(value: string): Sha256Hex {
  if (!SHA256_HEX_RE.test(value)) {
    throw new Error("not a sha256 hex digest");
  }
  return value as Sha256Hex;
}

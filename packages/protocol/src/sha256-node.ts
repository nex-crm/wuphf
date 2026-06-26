import { createHash } from "node:crypto";
import type { Sha256Hex } from "./sha256.ts";

export function sha256Hex(input: string | Uint8Array): Sha256Hex {
  const hash = createHash("sha256");
  hash.update(input);
  return hash.digest("hex") as Sha256Hex;
}

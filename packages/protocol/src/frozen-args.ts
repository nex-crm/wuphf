import { decode, encode } from "cbor-x";
import { canonicalJSON } from "./canonical-json.ts";
import { type Sha256Hex, sha256Hex } from "./sha256.ts";

// CBOR carries the JCS canonical JSON string, never the bytes being hashed as a
// separate authority. fromCBOR parses that string, re-canonicalizes the decoded
// JSON value, and requires byte-for-byte equality before returning a frozen
// instance, keeping the hash invariant rooted in JCS rather than CBOR encoding.
export class FrozenArgs {
  private constructor(
    readonly canonicalJson: string,
    readonly hash: Sha256Hex,
  ) {
    Object.freeze(this);
  }

  static freeze(input: unknown): FrozenArgs {
    const canonical = canonicalJSON(input);
    const hash = sha256Hex(canonical);
    const instance = new FrozenArgs(canonical, hash);
    Object.freeze(instance);
    return instance;
  }

  toCBOR(): Uint8Array {
    return encode(this.canonicalJson);
  }

  static fromCBOR(bytes: Uint8Array): FrozenArgs {
    const decoded: unknown = decode(bytes);
    if (typeof decoded !== "string") {
      throw new Error("FrozenArgs.fromCBOR: expected canonical JSON string");
    }

    const value: unknown = JSON.parse(decoded);
    const instance = FrozenArgs.freeze(value);
    if (instance.canonicalJson !== decoded) {
      throw new Error("FrozenArgs.fromCBOR: decoded JSON was not JCS canonical");
    }

    const decodedHash = sha256Hex(decoded);
    if (instance.hash !== decodedHash) {
      throw new Error("FrozenArgs.fromCBOR: hash mismatch");
    }

    return instance;
  }

  equals(other: FrozenArgs): boolean {
    return this.hash === other.hash;
  }
}

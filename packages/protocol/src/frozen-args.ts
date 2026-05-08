import { canonicalJSON } from "./canonical-json.ts";
import { type Sha256Hex, sha256Hex } from "./sha256.ts";

// FrozenArgs is the RFC 8785 (JCS) freeze boundary for tool-call arguments.
// `freeze` rejects any input that would break JCS hash invariance — see
// canonical-json.ts for the full rejection list. The hash is derived from the
// canonical JSON string; `equals` compares hashes only because the constructor
// is private and `freeze` enforces `hash === sha256Hex(canonicalJson)`.
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
    return new FrozenArgs(canonical, hash);
  }

  equals(other: FrozenArgs): boolean {
    return this.hash === other.hash;
  }
}

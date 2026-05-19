// nfkc.ts — the moat's frozen NFKC normaliser.
//
// Binds the version-pinned tables in `nfkc-table.generated.ts` (GENERATED
// from Unicode 15.1 by scripts/generate-nfkc-table.ts) to the table-agnostic
// algorithm in `nfkc-core.ts`. `sanitized-string.ts` calls `frozenNfkc`
// instead of `String.prototype.normalize("NFKC")`, so the moat normalisation is
// byte-identical on every runtime regardless of the Unicode version it ships.
//
// `frozenNfkc` is exported from this module but NOT re-exported by `index.ts`,
// so it is not public package API — the same convention as
// `isMoatDisallowedCodePoint`. Tests import it directly as the oracle.

import { composeKey, type NfkcTables, normalizeNfkc } from "./nfkc-core.ts";
import {
  NFKC_COMBINING_CLASS_ENTRIES,
  NFKC_COMPOSITION_ENTRIES,
  NFKC_DECOMPOSITION_ENTRIES,
} from "./nfkc-table.generated.ts";

// Built once at module load from the frozen generated entry lists. The
// composition entries are stored as readable `[starter, second, composite]`
// triples; they are packed into single-number keys here, matching the lookup
// in `composePair`.
const NFKC_TABLES: NfkcTables = {
  decomposition: new Map(NFKC_DECOMPOSITION_ENTRIES),
  composition: new Map(
    NFKC_COMPOSITION_ENTRIES.map(([starter, second, composite]) => [
      composeKey(starter, second),
      composite,
    ]),
  ),
  combiningClass: new Map(NFKC_COMBINING_CLASS_ENTRIES),
};

// Normalise `input` to Unicode Normalization Form KC against the frozen
// Unicode 15.1 tables. Pure: never calls `String.prototype.normalize`, so the
// result does not depend on the host runtime's Unicode version.
export function frozenNfkc(input: string): string {
  return normalizeNfkc(input, NFKC_TABLES);
}

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

// Defence in depth: a `composeKey` collision would silently drop a pair when
// the Map is built above, yielding a normaliser that fails to compose. The
// generator's `assertWellFormed` proves no collision at table-generation time;
// this re-checks the COMMITTED table at module load, so a hand-edit cannot
// slip past.
/* v8 ignore next 6 -- unreachable with the committed table (the generator's
   assertWellFormed proves no composeKey collision); this guard only fires on a
   corrupt hand-edit, so it has no covering test by design. */
if (NFKC_TABLES.composition.size !== NFKC_COMPOSITION_ENTRIES.length) {
  throw new Error(
    "nfkc-table.generated.ts: composition entries collide on a composeKey — " +
      "regenerate with scripts/generate-nfkc-table.ts",
  );
}

// Normalise `input` to Unicode Normalization Form KC against the frozen
// Unicode 15.1 tables. Pure: never calls `String.prototype.normalize`, so the
// result does not depend on the host runtime's Unicode version.
export function frozenNfkc(input: string): string {
  return normalizeNfkc(input, NFKC_TABLES);
}

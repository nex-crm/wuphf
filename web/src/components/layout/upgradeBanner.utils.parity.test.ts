/// <reference types="node" />

import { describe, expect, it } from "vitest";

import {
  type CommitEntry,
  hasNotable,
  isMajorBump,
} from "./upgradeBanner.utils";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Cross-language parity gate. The Go side
// (internal/upgradecheck.TestParity_NotableAndIsMajorBump) reads the same
// JSON fixture; both implementations must agree on every case. If you
// change the rule on EITHER side without updating the fixture, the
// other side's test fails — which is the whole point. The fixture is
// the single source of truth across:
//   - this file (web/src/components/layout/upgradeBanner.utils.ts: hasNotable, isMajorBump)
//   - internal/upgradecheck (Notable, IsMajorBump)
//   - internal/team.upgradeVersionParam regex (drift here is what surfaces
//     as banner / broker disagreement on a given release).

const __dirname = path.dirname(fileURLToPath(import.meta.url));
// web/src/components/layout → repo root is 4 hops up.
const FIXTURE_PATH = path.resolve(
  __dirname,
  "../../../../testdata/upgrade-parity.json",
);

interface NotableCase {
  name: string;
  // Fixture entries carry only {type, breaking} since those are the
  // ONLY fields hasNotable / Notable look at. Cast at use-site so we
  // don't have to pad the fixture with fields nobody reads.
  input: Array<{ type: string; breaking: boolean }>;
  want: boolean;
}

interface MajorBumpCase {
  name: string;
  from: string;
  to: string;
  want: boolean;
}

interface ParityFixture {
  notable: NotableCase[];
  isMajorBump: MajorBumpCase[];
}

const fixture: ParityFixture = JSON.parse(readFileSync(FIXTURE_PATH, "utf8"));

describe("parity: hasNotable matches fixture (mirrors Go Notable)", () => {
  // Belt-and-suspenders: a typo in the JSON keys would silently leave
  // this describe block with zero cases, turning the test into a no-op.
  // Force the fixture to actually carry content so a future "fix" that
  // accidentally renames `notable` to `notables` fails loudly.
  it("fixture has cases", () => {
    expect(fixture.notable.length).toBeGreaterThan(0);
  });
  for (const c of fixture.notable) {
    it(`notable: ${c.name}`, () => {
      // Cast: hasNotable's signature wants the full CommitEntry but
      // only reads .type/.breaking. The fixture omits fields that
      // would be noise.
      expect(hasNotable(c.input as unknown as CommitEntry[])).toBe(c.want);
    });
  }
});

describe("parity: isMajorBump matches fixture (mirrors Go IsMajorBump)", () => {
  it("fixture has cases", () => {
    expect(fixture.isMajorBump.length).toBeGreaterThan(0);
  });
  for (const c of fixture.isMajorBump) {
    it(`isMajorBump: ${c.name}`, () => {
      expect(isMajorBump(c.from, c.to)).toBe(c.want);
    });
  }
});

import { describe, expect, it } from "vitest";

import { isLifecycleState, stageForState } from "./lifecycle";
import stageMap from "./lifecycleStageMap.json";

// lifecycle.stagemap.test.ts pins the web board's lifecycle_state -> stage
// grouping (stageForState) to the shared source of truth lifecycleStageMap.json.
// The Go broker grouping (lifecycleStageFor) has its own oracle against the same
// file (internal/team/lifecycle_stage_oracle_test.go), so the two independent
// language copies of the 13->7 table cannot drift without a red test on a side.

// The wire lifecycle states, mirrored from the Go wireLifecycleStates list.
// Excludes any Go-only migration fallback (e.g. "unknown"). Keep in sync with
// the Go oracle when a wire state is added.
const EXPECTED_WIRE_STATES = [
  "drafting",
  "intake",
  "ready",
  "planning",
  "running",
  "review",
  "changes_requested",
  "blocked_on_pr_merge",
  "queued_behind_owner",
  "decision",
  "approved",
  "rejected",
  "archived",
] as const;

describe("lifecycle stage map oracle", () => {
  it("every shared-map entry matches stageForState", () => {
    for (const [state, stage] of Object.entries(stageMap)) {
      expect(isLifecycleState(state)).toBe(true);
      // Guard so the cast is sound even if a bogus key sneaks into the JSON.
      if (isLifecycleState(state)) {
        expect(stageForState(state)).toBe(stage);
      }
    }
  });

  it("covers exactly the wire lifecycle states", () => {
    expect(new Set(Object.keys(stageMap))).toEqual(
      new Set(EXPECTED_WIRE_STATES),
    );
  });
});

import { describe, expect, it } from "vitest";

import {
  humanizeActivity,
  humanizeLifecycleState,
  humanizeStateTokens,
  humanizeTurnOutcome,
  looksLikeRawToolPayload,
} from "./humanizeActivity";

// Render-boundary regression guards (ten-out-of-ten E1): lifecycle enums,
// tool JSON, MCP ids, and process exhaust never render raw at the human
// boundary.

describe("humanizeLifecycleState", () => {
  it("maps every known enum to a plain label", () => {
    expect(humanizeLifecycleState("blocked")).toBe(
      "Blocked",
    );
    expect(humanizeLifecycleState("queued_behind_owner")).toBe(
      "Queued behind owner",
    );
    expect(humanizeLifecycleState("changes_requested")).toBe(
      "Changes requested",
    );
    expect(humanizeLifecycleState("running")).toBe("Running");
    expect(humanizeLifecycleState("review")).toBe("In review");
    expect(humanizeLifecycleState("decision")).toBe("Needs decision");
    expect(humanizeLifecycleState("drafting")).toBe("Parked");
  });

  it("degrades unknown snake_case states to capitalized words", () => {
    expect(humanizeLifecycleState("waiting_on_widget")).toBe(
      "Waiting on widget",
    );
    expect(humanizeLifecycleState("")).toBe("");
  });

  it("never returns a string containing an underscore", () => {
    for (const state of [
      "blocked",
      "queued_behind_owner",
      "changes_requested",
      "some_future_state",
    ]) {
      expect(humanizeLifecycleState(state)).not.toContain("_");
    }
  });
});

describe("humanizeStateTokens", () => {
  it("replaces embedded enum tokens in prose", () => {
    expect(
      humanizeStateTokens("running → queued_behind_owner [deps]"),
    ).toBe("running → Queued behind owner [deps]");
  });

  it("leaves plain-word state tokens like 'blocked' untouched in prose", () => {
    // Only snake_case enum tokens are rewritten; single plain words read
    // fine in prose, so 'blocked' (renamed from blocked_on_pr_merge) passes
    // through unchanged — same treatment as 'review' / 'running'.
    expect(humanizeStateTokens("running → blocked [deps]")).toBe(
      "running → blocked [deps]",
    );
  });

  it("leaves ordinary prose untouched", () => {
    const prose = "Reviewed the draft and asked for one change.";
    expect(humanizeStateTokens(prose)).toBe(prose);
  });
});

describe("looksLikeRawToolPayload / humanizeActivity", () => {
  it("collapses raw tool-call JSON to Working…", () => {
    const raw =
      '[{"tool_name":"mcp__wuphf-office__team_task","type":"tool_reference"}]';
    expect(looksLikeRawToolPayload(raw)).toBe(true);
    expect(humanizeActivity(raw)).toBe("Working…");
  });

  it("collapses MCP ids and snake_case tool names", () => {
    expect(humanizeActivity("running mcp__wuphf_wiki_lookup")).toBe("Working…");
    expect(humanizeActivity("wuphf_office_members")).toBe("Working…");
  });

  it("passes genuine prose through", () => {
    expect(humanizeActivity("drafting the renewal brief")).toBe(
      "drafting the renewal brief",
    );
    expect(humanizeActivity("waiting for work")).toBe("waiting for work");
  });
});

describe("humanizeTurnOutcome", () => {
  it("maps killed-process exhaust to an honest one-liner", () => {
    expect(humanizeTurnOutcome("signal: killed: signal: killed")).toBe(
      "Turn was interrupted before finishing.",
    );
    expect(humanizeTurnOutcome("exit status 1: exit status 1")).toBe(
      "Turn was interrupted before finishing.",
    );
  });

  it("drops machine-shaped noise instead of rendering it raw", () => {
    expect(humanizeTurnOutcome('{"tool_name":"x"}')).toBe("");
  });

  it("passes prose through with state tokens humanized", () => {
    expect(humanizeTurnOutcome("ok (no durable trace)")).toBe(
      "ok (no durable trace)",
    );
    expect(humanizeTurnOutcome("moved to queued_behind_owner")).toBe(
      "moved to Queued behind owner",
    );
  });
});

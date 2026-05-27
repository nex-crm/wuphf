/**
 * CeoExecutionLineup — Phase 4 card tests.
 *
 * Covers:
 *  - Pending state: agents listed, accept/decline chips, submit button.
 *  - Decline toggles an agent out; accept toggles it back.
 *  - Submitting state: button disabled, spinner visible.
 *  - Committed state: one-line confirmation.
 *  - XSS: attack strings in agent role/reason render as text (sanitization
 *    regression per PR #684 confused-deputy bypass closure).
 */

import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CeoExecutionLineupPayload } from "../../onboarding/types";
import { CeoExecutionLineup } from "./CeoExecutionLineup";

// ── Fixtures ───────────────────────────────────────────────────────────

const PAYLOAD: CeoExecutionLineupPayload = {
  suggestion_id: "sug-001",
  agents: [
    {
      slug: "engineer",
      role: "Founding Engineer",
      reason: "Handles backend implementation and infra.",
    },
    {
      slug: "designer",
      role: "Product Designer",
      reason: "Designs the UI and reviews component interactions.",
    },
    {
      slug: "pm",
      role: "Product Manager",
      reason: "Owns scope and acceptance criteria.",
    },
  ],
};

const XSS_PAYLOAD: CeoExecutionLineupPayload = {
  suggestion_id: "sug-xss",
  agents: [
    {
      slug: "xss-agent",
      role: '<script>alert("xss")</script>',
      reason: '<img src="x" onerror="alert(1)">',
    },
  ],
};

// ── Helpers ────────────────────────────────────────────────────────────

function setup(
  payload: CeoExecutionLineupPayload = PAYLOAD,
  initialStage: "pending" | "submitting" | "committed" = "pending",
) {
  const onStageChange = vi.fn();
  const { container } = render(
    <CeoExecutionLineup
      payload={payload}
      stage={initialStage}
      onStageChange={onStageChange}
    />,
  );
  return { onStageChange, container };
}

// ── Tests ──────────────────────────────────────────────────────────────

describe("<CeoExecutionLineup>", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ── Pending state ─────────────────────────────────────────────────

  it("renders all agents in pending state", () => {
    setup();
    expect(screen.getByTestId("ceo-execution-lineup")).toBeInTheDocument();
    for (const agent of PAYLOAD.agents) {
      expect(
        screen.getByTestId(`lineup-agent-row-${agent.slug}`),
      ).toBeInTheDocument();
    }
  });

  it("renders each agent's role and reason as text (not HTML)", () => {
    setup();
    expect(screen.getByText("Founding Engineer")).toBeInTheDocument();
    expect(
      screen.getByText(/Handles backend implementation/),
    ).toBeInTheDocument();
  });

  it("submit button shows correct agent count", () => {
    setup();
    const btn = screen.getByTestId("lineup-submit");
    expect(btn).toHaveTextContent("Spin up 3 agents");
  });

  it("all chips default to Accept", () => {
    setup();
    for (const agent of PAYLOAD.agents) {
      const chip = screen.getByTestId(`lineup-chip-${agent.slug}`);
      expect(chip).toHaveTextContent("Accept");
      expect(chip).toHaveAttribute("aria-pressed", "true");
    }
  });

  // ── Accept / Decline toggle ────────────────────────────────────────

  it("clicking Decline chip toggles agent off", () => {
    setup();
    const chip = screen.getByTestId("lineup-chip-engineer");
    fireEvent.click(chip);
    expect(chip).toHaveTextContent("Decline");
    expect(chip).toHaveAttribute("aria-pressed", "false");
  });

  it("clicking Decline then Accept toggles agent back on", () => {
    setup();
    const chip = screen.getByTestId("lineup-chip-engineer");
    fireEvent.click(chip); // → Decline
    fireEvent.click(chip); // → Accept
    expect(chip).toHaveTextContent("Accept");
    expect(chip).toHaveAttribute("aria-pressed", "true");
  });

  it("declining one agent updates submit button count", () => {
    setup();
    fireEvent.click(screen.getByTestId("lineup-chip-engineer"));
    const btn = screen.getByTestId("lineup-submit");
    expect(btn).toHaveTextContent("Spin up 2 agents");
  });

  it("declining all agents disables submit button", () => {
    setup();
    for (const agent of PAYLOAD.agents) {
      fireEvent.click(screen.getByTestId(`lineup-chip-${agent.slug}`));
    }
    expect(screen.getByTestId("lineup-submit")).toBeDisabled();
  });

  // ── Submitting state ──────────────────────────────────────────────

  it("renders submit button as disabled in submitting state", () => {
    setup(PAYLOAD, "submitting");
    expect(screen.getByTestId("lineup-submit")).toBeDisabled();
  });

  it("keeps the action label in submitting state (no spinner swap)", () => {
    setup(PAYLOAD, "submitting");
    // The submitting state no longer swaps the button label to a
    // spinner — between-question loaders read as flicker because the
    // next card swaps in immediately via setQueryData. The button just
    // stays disabled with the same label so the user sees what they
    // submitted.
    expect(screen.getByTestId("lineup-submit")).toHaveTextContent("Spin up");
    expect(screen.getByTestId("lineup-submit")).toBeDisabled();
  });

  // ── Committed state ───────────────────────────────────────────────

  it("keeps the lineup visible in committed state — no chip swap", () => {
    setup(PAYLOAD, "committed");
    // The committed state no longer replaces the lineup with a
    // "✓ N agents added to roster" chip — that flashed between cards
    // because the sticky-suggestion swap is near-instant. The lineup
    // stays mounted, just disabled.
    expect(screen.queryByTestId("lineup-committed")).toBeNull();
    expect(screen.getByTestId("ceo-execution-lineup")).toBeInTheDocument();
    for (const agent of PAYLOAD.agents) {
      expect(
        screen.getByTestId(`lineup-agent-row-${agent.slug}`),
      ).toBeInTheDocument();
    }
    expect(screen.getByTestId("lineup-submit")).toBeDisabled();
  });

  // ── XSS sanitization regression ───────────────────────────────────

  it("renders XSS attack strings in role as plain text, not HTML", () => {
    setup(XSS_PAYLOAD);
    const row = screen.getByTestId("lineup-agent-row-xss-agent");
    // The role text is inside a span, not interpreted as HTML.
    const roleEl = row.querySelector(".ceo-lineup-agent-role");
    expect(roleEl?.textContent).toContain("<script>");
    // Crucially, no actual <script> element should be in the DOM.
    expect(row.querySelector("script")).toBeNull();
  });

  it("renders XSS attack strings in reason as plain text, not HTML", () => {
    setup(XSS_PAYLOAD);
    const row = screen.getByTestId("lineup-agent-row-xss-agent");
    const reasonEl = row.querySelector(".ceo-lineup-agent-reason");
    // The img with onerror should appear as text, not an actual img element.
    expect(reasonEl?.textContent).toContain("onerror");
    expect(row.querySelector("img")).toBeNull();
  });
});

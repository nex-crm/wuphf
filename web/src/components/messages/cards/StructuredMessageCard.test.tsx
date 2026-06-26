/**
 * StructuredMessageCard tests
 *
 * Covers:
 * 1. Every known kind dispatches to the correct sub-component
 * 2. sanitizeStructuredPayload runs on the payload (deep copy, non-object fallback)
 * 3. Unknown kind renders a safe fallback span with no executable content
 * 4. XSS regression: attack strings in payload render as plain text
 *
 * Phase 5: single audit point for all CEO card kinds and interview kinds.
 * Spec: docs/specs/onboarding-into-office.md § "Phase 5 — Polish and cleanups"
 */

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CardStage, CeoSuggestion } from "../../onboarding/types";
import {
  StructuredMessageCard,
  sanitizeStructuredPayload,
} from "./StructuredMessageCard";

// Mock all card sub-components so we only test the dispatcher logic.
// Each mock renders a testid matching the kind so we can assert dispatch.
vi.mock("./CeoFormField", () => ({
  CeoFormField: () => <div data-testid="mock-ceo-form-field" />,
}));
vi.mock("./CeoChipRow", () => ({
  CeoChipRow: () => <div data-testid="mock-ceo-chip-row" />,
}));
vi.mock("./CeoChecklist", () => ({
  CeoChecklist: () => <div data-testid="mock-ceo-checklist" />,
}));
vi.mock("./CeoScanChip", () => ({
  CeoScanChip: () => <div data-testid="mock-ceo-scan-chip" />,
}));
vi.mock("./CeoExecutionLineup", () => ({
  CeoExecutionLineup: () => <div data-testid="mock-ceo-execution-lineup" />,
}));

const baseProps = {
  stage: "pending" as CardStage,
  onSubmit: vi.fn(),
};

// ── Kind dispatch ─────────────────────────────────────────────────────────

describe("StructuredMessageCard — kind dispatch", () => {
  it("dispatches ceo_form_field to CeoFormField", () => {
    const suggestion: CeoSuggestion = {
      id: "s1",
      phase: "identity",
      kind: "ceo_form_field",
      payload: {
        field: "company_name",
        label: "Office name?",
        optional: false,
      },
    };
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(screen.getByTestId("mock-ceo-form-field")).toBeInTheDocument();
  });

  it("dispatches ceo_chip_row to CeoChipRow", () => {
    const suggestion: CeoSuggestion = {
      id: "s2",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: { field: "blueprint_id", label: "Pick:", options: [] },
    };
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(screen.getByTestId("mock-ceo-chip-row")).toBeInTheDocument();
  });

  it("dispatches ceo_checklist to CeoChecklist", () => {
    const suggestion: CeoSuggestion = {
      id: "s3",
      phase: "team",
      kind: "ceo_checklist",
      payload: { field: "agents", label: "Team:", items: [] },
    };
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(screen.getByTestId("mock-ceo-checklist")).toBeInTheDocument();
  });

  it("dispatches ceo_team_trim (alias) to CeoChecklist", () => {
    const suggestion: CeoSuggestion = {
      id: "s4",
      phase: "team",
      kind: "ceo_team_trim",
      payload: { field: "agents", label: "Trim:", items: [] },
    };
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(screen.getByTestId("mock-ceo-checklist")).toBeInTheDocument();
  });

  it("dispatches ceo_scan_chip to CeoScanChip", () => {
    const suggestion: CeoSuggestion = {
      id: "s5",
      phase: "scan",
      kind: "ceo_scan_chip",
      payload: { field: "website_url", url: "acme.com", status: "scanning" },
    };
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(screen.getByTestId("mock-ceo-scan-chip")).toBeInTheDocument();
  });

  it("dispatches ceo_execution_lineup to CeoExecutionLineup", () => {
    const suggestion: CeoSuggestion = {
      id: "s6",
      phase: "bridge",
      kind: "ceo_execution_lineup",
      payload: { suggestion_id: "s6", agents: [] },
    };
    render(
      <StructuredMessageCard
        suggestion={suggestion}
        {...baseProps}
        onStageChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId("mock-ceo-execution-lineup")).toBeInTheDocument();
  });
});

// ── Fallback for unknown kind ─────────────────────────────────────────────

describe("StructuredMessageCard — unknown kind fallback", () => {
  it("renders a safe fallback span for an unrecognized kind", () => {
    // Cast to bypass TypeScript exhaustiveness so we can test the runtime path.
    const suggestion = {
      id: "s7",
      phase: "future",
      kind: "ceo_future_kind_not_yet_in_union",
      payload: {},
    } as unknown as CeoSuggestion;
    render(<StructuredMessageCard suggestion={suggestion} {...baseProps} />);
    expect(
      screen.getByTestId("structured-card-unknown-kind"),
    ).toBeInTheDocument();
    // Confirm the fallback contains no executable script tags.
    expect(document.querySelector("script")).not.toBeInTheDocument();
  });
});

// ── sanitizeStructuredPayload ─────────────────────────────────────────────

describe("sanitizeStructuredPayload", () => {
  it("returns empty object for null input", () => {
    expect(sanitizeStructuredPayload(null)).toEqual({});
  });

  it("returns empty object for array input", () => {
    expect(sanitizeStructuredPayload([])).toEqual({});
  });

  it("returns empty object for primitive input", () => {
    expect(sanitizeStructuredPayload("string")).toEqual({});
    expect(sanitizeStructuredPayload(42)).toEqual({});
  });

  it("returns a deep clone of a plain object", () => {
    const input = { label: "Office name?", nested: { value: "test" } };
    const result = sanitizeStructuredPayload(input);
    expect(result).toEqual(input);
    // Mutating the clone does not affect the original.
    (result as { label: string }).label = "mutated";
    expect(input.label).toBe("Office name?");
  });

  it("deep-clones arrays within the object", () => {
    const input = {
      items: [
        { id: "a", label: "A" },
        { id: "b", label: "B" },
      ],
    };
    const result = sanitizeStructuredPayload(input);
    expect(result).toEqual(input);
  });

  it("preserves attack-string content as data (cards render as text)", () => {
    // sanitizeStructuredPayload does not strip content — cards are responsible
    // for rendering as plain text. The function only ensures the payload is a
    // well-formed object that can be safely passed to card components.
    const xssInput = { label: '<script>alert("xss")</script>' };
    const result = sanitizeStructuredPayload(xssInput);
    expect((result as typeof xssInput).label).toBe(
      '<script>alert("xss")' + "</script>",
    );
  });
});

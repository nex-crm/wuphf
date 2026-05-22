/**
 * humanEcho — unit tests.
 *
 * Pins the "echo human answer into the CEO DM" rules:
 *   - form-field free text bubbles back verbatim
 *   - chip rows resolve id → human-readable label
 *   - checklists resolve all picked ids → comma list
 *   - non-conversational fields (bridge_choice, scan_complete) return null
 *   - read-only kinds (scan chip, execution lineup) return null
 *   - empty / skipped answers return null
 */

import { describe, expect, it } from "vitest";

import { humanEchoForCeoAnswer } from "./humanEcho";
import type {
  CeoChecklistSuggestion,
  CeoChipRowSuggestion,
  CeoExecutionLineupSuggestion,
  CeoFormFieldSuggestion,
  CeoScanChipSuggestion,
  CeoTeamTrimSuggestion,
} from "./types";

const formField: CeoFormFieldSuggestion = {
  id: "greet-company-name",
  phase: "greet",
  kind: "ceo_form_field",
  payload: {
    field: "company_name",
    label: "Office name",
  },
};

const chipRow: CeoChipRowSuggestion = {
  id: "blueprint-pick",
  phase: "blueprint",
  kind: "ceo_chip_row",
  payload: {
    field: "blueprint_id",
    label: "Pick a starter template",
    options: [
      { id: "bookkeeping-invoicing-service", label: "Bookkeeping" },
      { id: "niche-crm", label: "Niche CRM" },
      { id: "", label: "Start from scratch" },
    ],
  },
};

const checklist: CeoChecklistSuggestion = {
  id: "team-trim",
  phase: "team",
  kind: "ceo_checklist",
  payload: {
    field: "picked_agents",
    label: "Keep or trim",
    items: [
      { id: "engineer", label: "Engineer" },
      { id: "designer", label: "Designer" },
      { id: "marketer", label: "Marketer" },
    ],
  },
};

const teamTrim: CeoTeamTrimSuggestion = {
  ...checklist,
  kind: "ceo_team_trim",
};

const scanChip: CeoScanChipSuggestion = {
  id: "scan-progress",
  phase: "scan",
  kind: "ceo_scan_chip",
  payload: {
    field: "scan_progress",
    url: "acme.com",
    status: "scanning",
  },
};

const executionLineup: CeoExecutionLineupSuggestion = {
  id: "execution-lineup",
  phase: "team",
  kind: "ceo_execution_lineup",
  payload: {
    suggestion_id: "execution-lineup",
    agents: [
      { slug: "engineer", role: "Eng", reason: "ships features" },
    ],
  },
};

describe("humanEchoForCeoAnswer", () => {
  it("echoes a form-field free-text answer verbatim", () => {
    expect(humanEchoForCeoAnswer(formField, "company_name", "Acme Test QA")).toBe(
      "Acme Test QA",
    );
  });

  it("trims whitespace from a form-field answer", () => {
    expect(humanEchoForCeoAnswer(formField, "company_name", "  Acme  ")).toBe(
      "Acme",
    );
  });

  it("returns null when a form-field answer is empty", () => {
    expect(humanEchoForCeoAnswer(formField, "description", "")).toBeNull();
    expect(humanEchoForCeoAnswer(formField, "description", "   ")).toBeNull();
  });

  it("resolves chip-row id into the human-readable label", () => {
    expect(
      humanEchoForCeoAnswer(chipRow, "blueprint_id", "niche-crm"),
    ).toBe("Niche CRM");
  });

  it("renders the 'start from scratch' chip even when its id is empty", () => {
    expect(humanEchoForCeoAnswer(chipRow, "blueprint_id", "")).toBe(
      "Start from scratch",
    );
  });

  it("falls back to the raw id when chip option is unknown", () => {
    expect(
      humanEchoForCeoAnswer(chipRow, "blueprint_id", "unknown-bp"),
    ).toBe("unknown-bp");
  });

  it("joins picked checklist labels with commas", () => {
    expect(
      humanEchoForCeoAnswer(checklist, "picked_agents", [
        "engineer",
        "marketer",
      ]),
    ).toBe("Engineer, Marketer");
  });

  it("handles a team-trim suggestion the same as a checklist", () => {
    expect(
      humanEchoForCeoAnswer(teamTrim, "picked_agents", ["designer"]),
    ).toBe("Designer");
  });

  it("renders an explicit '(nobody)' when no items are picked", () => {
    expect(humanEchoForCeoAnswer(checklist, "picked_agents", [])).toBe(
      "(nobody)",
    );
  });

  it("returns null for bridge_choice", () => {
    expect(
      humanEchoForCeoAnswer(chipRow, "bridge_choice", "start_issue"),
    ).toBeNull();
  });

  it("returns null for scan_complete", () => {
    expect(
      humanEchoForCeoAnswer(formField, "scan_complete", true),
    ).toBeNull();
  });

  it("returns null when there is no pending suggestion", () => {
    expect(humanEchoForCeoAnswer(null, "company_name", "Acme")).toBeNull();
    expect(
      humanEchoForCeoAnswer(undefined, "company_name", "Acme"),
    ).toBeNull();
  });

  it("returns null for the read-only scan chip", () => {
    expect(
      humanEchoForCeoAnswer(scanChip, "scan_progress", "anything"),
    ).toBeNull();
  });

  it("returns null for the execution-lineup confirmation card", () => {
    expect(
      humanEchoForCeoAnswer(executionLineup, "picked_agents", ["engineer"]),
    ).toBeNull();
  });
});

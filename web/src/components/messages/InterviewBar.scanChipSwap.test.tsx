/**
 * Regression test for nex-crm/wuphf#936 — frontend desync after scan failure.
 *
 * The CEO wizard renders the current PendingSuggestion in a single composer
 * slot. When the broker swaps the suggestion from `ceo_scan_chip`
 * (phase=scan) to `ceo_chip_row` (phase=blueprint) — which is exactly what
 * happens when a website scan fails and `advanceAfterScan` transitions to
 * PhaseBlueprint — the slot must re-render with the new card immediately.
 *
 * Before the fix the slot kept showing the scan chip until a hard reload
 * because `useOnboardingState` only polled every 3s and the read-only scan
 * chip never had buttons. This test pins the swap behaviour at the
 * CeoCardSection layer: switch the pendingSuggestion in the
 * OnboardingDMContext and assert the scan chip is gone and the new
 * chip-row buttons render.
 */

import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OnboardingDMContextProvider } from "../onboarding/OnboardingDMRoute";
import type { CeoSuggestion } from "../onboarding/types";
import { CeoCardSection } from "./InterviewBar";

// ── Mocks ─────────────────────────────────────────────────────────────────

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, get: vi.fn(), post: vi.fn() };
});

vi.mock("../../hooks/useRequests", () => ({
  useRequests: () => ({ pending: [] }),
}));

// ── Helpers ───────────────────────────────────────────────────────────────

const scanChipSuggestion: CeoSuggestion = {
  id: "scan-progress-example-com",
  phase: "scan",
  kind: "ceo_scan_chip",
  payload: {
    field: "website_url",
    url: "https://example.com",
    status: "scanning",
  },
};

const blueprintChipRowSuggestion: CeoSuggestion = {
  id: "blueprint-pick",
  phase: "blueprint",
  kind: "ceo_chip_row",
  payload: {
    field: "blueprint_id",
    label: "Pick a starter template, or start from scratch:",
    options: [
      { id: "bookkeeping-invoicing-service", label: "Bookkeeping" },
      { id: "niche-crm", label: "Niche CRM" },
      { id: "", label: "Start from scratch" },
    ],
  },
};

interface HarnessProps {
  initial: CeoSuggestion;
  next: CeoSuggestion;
}

/**
 * Test harness that exposes a button to swap the OnboardingDMContext's
 * pendingSuggestion from `initial` to `next`. Mirrors how the SSE-driven
 * invalidation of `["onboarding-state"]` in useBrokerEvents.ts causes the
 * `useOnboardingState` query to refetch with the new suggestion — but
 * compressed to a single synchronous click so we don't depend on real
 * SSE or polling.
 */
function SwapHarness({ initial, next }: HarnessProps) {
  const [suggestion, setSuggestion] = useState<CeoSuggestion>(initial);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <OnboardingDMContextProvider
        value={{ phase: suggestion.phase, pendingSuggestion: suggestion }}
      >
        <button type="button" onClick={() => setSuggestion(next)}>
          swap
        </button>
        <CeoCardSection />
      </OnboardingDMContextProvider>
    </QueryClientProvider>
  );
}

// ── Tests ────────────────────────────────────────────────────────────────

afterEach(() => {
  cleanup();
});

describe("CeoCardSection — scan-chip → blueprint-pick swap (regression #936)", () => {
  it("renders the scan chip initially, then swaps to the blueprint chip-row when the suggestion id changes", () => {
    render(
      <SwapHarness
        initial={scanChipSuggestion}
        next={blueprintChipRowSuggestion}
      />,
    );

    // Initial render: read-only scan chip, no buttons in the slot.
    const section = screen.getByTestId("ceo-card-section");
    expect(section.getAttribute("data-kind")).toBe("ceo_scan_chip");
    expect(section.getAttribute("data-suggestion-id")).toBe(
      "scan-progress-example-com",
    );
    expect(screen.getByTestId("ceo-scan-chip")).toBeInTheDocument();
    // ceo-chip-row is the testid CeoChipRow renders.
    expect(screen.queryByTestId("ceo-chip-row")).toBeNull();

    // Swap the pending suggestion (simulating the SSE-driven onboarding-state
    // invalidate landing the new blueprint-pick into the context).
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "swap" }));
    });

    // Post-swap: scan chip is gone, chip-row is mounted, and the section's
    // data attributes reflect the new suggestion identity.
    const swapped = screen.getByTestId("ceo-card-section");
    expect(swapped.getAttribute("data-kind")).toBe("ceo_chip_row");
    expect(swapped.getAttribute("data-suggestion-id")).toBe("blueprint-pick");
    expect(screen.queryByTestId("ceo-scan-chip")).toBeNull();
    expect(screen.getByTestId("ceo-chip-row")).toBeInTheDocument();
    // The blueprint options must render as interactive chips (role=option
    // inside the listbox) — this is what the user was missing while
    // stranded with the scan chip pinned.
    expect(
      screen.getByRole("option", { name: "Bookkeeping" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: "Start from scratch" }),
    ).toBeInTheDocument();
  });
});

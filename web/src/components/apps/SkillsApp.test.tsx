import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getSkills: vi.fn().mockResolvedValue({ skills: [] }),
    compileSkills: vi.fn().mockResolvedValue({
      scanned: 0,
      matched: 0,
      proposed: 0,
      deduped: 0,
      rejected_by_guard: 0,
      errors: [],
      duration_ms: 0,
      trigger: "manual",
    }),
  };
});

import { SkillsApp } from "./SkillsApp";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<SkillsApp> empty state", () => {
  it("shows a Compile call-to-action when there are no skills", async () => {
    render(wrap(<SkillsApp />));

    await waitFor(() => {
      // The friendly empty-state copy should be rendered.
      expect(screen.getByText(/No skills yet\./i)).toBeInTheDocument();
    });

    // The Compile button must be present in the empty state so users have a
    // warm CTA without first having to find the header action.
    const buttons = screen
      .getAllByRole("button")
      .filter((b) => /Compile/.test(b.textContent ?? ""));
    expect(buttons.length).toBeGreaterThanOrEqual(1);
  });
});

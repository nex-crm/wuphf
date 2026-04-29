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
    getSkillsList: vi.fn().mockResolvedValue({ skills: [] }),
    getOfficeMembers: vi.fn().mockResolvedValue({ members: [] }),
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

import * as clientMod from "../../api/client";
import { OwnersChip, SkillsApp } from "./SkillsApp";

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

describe("<SkillsApp> similar_to_existing badge", () => {
  it("shows a similar-to indicator on proposed skills flagged by the similarity gate", async () => {
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [
        {
          name: "send-email",
          status: "proposed",
          description: "Send emails",
          similar_to_existing: {
            slug: "email-ops",
            score: 0.82,
            method: "embedding-cosine",
          },
        },
      ],
    });

    render(wrap(<SkillsApp />));

    await waitFor(() => {
      expect(screen.getByText(/Similar to:/i)).toBeInTheDocument();
      expect(screen.getByText("email-ops")).toBeInTheDocument();
    });
  });

  it("does not show a similar-to indicator on proposed skills without the flag", async () => {
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [{ name: "clean-skill", status: "proposed" }],
    });

    render(wrap(<SkillsApp />));

    await waitFor(() => {
      expect(screen.queryByText(/Similar to:/i)).not.toBeInTheDocument();
    });
  });
});

describe("<OwnersChip>", () => {
  it("renders 'lead-routable' when slugs are missing or empty", () => {
    render(<OwnersChip />);
    expect(screen.getByText(/lead-routable/i)).toBeInTheDocument();
  });

  it("renders comma-separated @-prefixed slugs when provided", () => {
    render(<OwnersChip slugs={["deploy-bot", "csm"]} />);
    expect(screen.getByText("@deploy-bot, @csm")).toBeInTheDocument();
  });

  it("ignores empty/whitespace slugs and falls back to lead-routable", () => {
    render(<OwnersChip slugs={["", "   "]} />);
    expect(screen.getByText(/lead-routable/i)).toBeInTheDocument();
  });
});

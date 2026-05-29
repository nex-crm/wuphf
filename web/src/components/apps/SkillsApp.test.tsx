import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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
    patchSkill: vi.fn().mockResolvedValue({}),
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

vi.mock("../../lib/router", () => ({
  router: { navigate: vi.fn() },
}));

import * as clientMod from "../../api/client";
import { router } from "../../lib/router";
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

describe("<SkillsApp> SidePanel editor", () => {
  it("navigates to the full-screen SKILL.md detail page when View SKILL.md is clicked", async () => {
    // In v3-mvp the inline SidePanel editor was replaced by a full-screen
    // editor at /skills/$skillName (see SkillsApp.handlePreview →
    // router.navigate, SkillDetailRoute). The "View SKILL.md →" button on
    // each card is now a navigation trigger, not an inline-editor toggle.
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [
        {
          name: "draft-skill",
          status: "proposed",
          description: "A draft.",
          content: "## Steps\n1. do thing",
        },
      ],
    });

    render(wrap(<SkillsApp />));

    const viewFull = await screen.findByRole("button", {
      name: /View SKILL\.md/i,
    });
    fireEvent.click(viewFull);

    const navigate = vi.mocked(router.navigate);
    await waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({
        to: "/skills/$skillName",
        params: { skillName: "draft-skill" },
      });
    });
  });

  it.skip("calls patchSkill on Save with the new body", async () => {
    // FIXME(v3-mvp): The save flow moved from the SidePanel inline editor to
    // the full-screen SkillDetailRoute. The patch surface there is
    // editSkillContent (PUT /skills/{name}), not patchSkill. Rewrite this
    // test against SkillDetailRoute when that route gains its own test file
    // (web/src/routes/SkillDetailRoute.test.tsx).
  });

  it.skip("preserves chars typed between save resolve and parent post-save effect", async () => {
    // FIXME(v3-mvp): Same as above — the stale-closure regression now lives
    // in SkillDetailRoute's draft buffer (see SkillDetailRoute.tsx: seedKey
    // gating around setDraft). Move this regression test to the new route's
    // test file rather than re-asserting it against an unreachable surface.
  });

  it("does not show the editor for active skills (preview only)", async () => {
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [
        {
          name: "live-skill",
          status: "active",
          content: "playbook body",
        },
      ],
    });

    render(wrap(<SkillsApp />));

    // The PixelSkillCard renders the skill name on both the front header
    // and the back-face detail panel, so we expect at least one match.
    // The point of the test is that the editor textarea must not mount
    // for active skills — the SidePanel preview is reachable but read-only.
    await waitFor(() => {
      expect(screen.getAllByText("live-skill").length).toBeGreaterThan(0);
    });
    expect(
      screen.queryByRole("textbox", { name: /Edit body/i }),
    ).not.toBeInTheDocument();
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

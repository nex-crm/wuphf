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

describe("<SkillsApp> SidePanel editor", () => {
  it("opens an editable textarea for proposed skills via View full link", async () => {
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
      name: /View full SKILL\.md/i,
    });
    fireEvent.click(viewFull);

    // Editor is keyed by the body label/aria-label.
    const editor = await screen.findByRole("textbox", {
      name: /Edit body for draft-skill/i,
    });
    expect(editor).toHaveValue("## Steps\n1. do thing");

    // Save is disabled when not dirty; revert too.
    expect(
      screen.getByRole("button", { name: /Save edits to draft-skill/i }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: /Revert/i })).toBeDisabled();
  });

  it("calls patchSkill on Save with the new body", async () => {
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [
        {
          name: "draft-skill",
          status: "proposed",
          content: "old body",
        },
      ],
    });
    const patchMock = vi.mocked(clientMod.patchSkill).mockResolvedValueOnce({});

    render(wrap(<SkillsApp />));

    const viewFull = await screen.findByRole("button", {
      name: /View full SKILL\.md/i,
    });
    fireEvent.click(viewFull);

    const editor = await screen.findByRole("textbox", {
      name: /Edit body for draft-skill/i,
    });
    fireEvent.change(editor, { target: { value: "new body" } });

    const save = screen.getByRole("button", {
      name: /Save edits to draft-skill/i,
    });
    expect(save).not.toBeDisabled();
    fireEvent.click(save);

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledWith("draft-skill", {
        old_string: "old body",
        new_string: "new body",
        replace_all: false,
      });
    });
  });

  it("preserves chars typed between save resolve and parent post-save effect", async () => {
    // Regression for the stale-closure bug devils-advocate flagged: after
    // a successful save, the parent passes back res.skill with new content,
    // which used to land in the reset useEffect's dep array and silently
    // wipe any chars the user typed in the gap between the patch resolving
    // and the effect running.
    vi.mocked(clientMod.patchSkill).mockClear();
    vi.mocked(clientMod.getSkillsList).mockResolvedValueOnce({
      skills: [
        {
          name: "draft-skill",
          status: "proposed",
          content: "first body",
        },
      ],
    });
    // patchSkill resolves with the new server-known body.
    vi.mocked(clientMod.patchSkill).mockResolvedValueOnce({
      skill: {
        name: "draft-skill",
        status: "proposed",
        content: "second body",
      },
    });

    render(wrap(<SkillsApp />));

    const viewFull = await screen.findByRole("button", {
      name: /View full SKILL\.md/i,
    });
    fireEvent.click(viewFull);

    const editor = await screen.findByRole("textbox", {
      name: /Edit body for draft-skill/i,
    });

    // First save with "second body".
    fireEvent.change(editor, { target: { value: "second body" } });
    fireEvent.click(
      screen.getByRole("button", { name: /Save edits to draft-skill/i }),
    );

    await waitFor(() => {
      expect(vi.mocked(clientMod.patchSkill)).toHaveBeenCalledTimes(1);
    });

    // User keeps typing after save resolves. The parent has now updated
    // previewSkill.content via onSaved → handlePreviewSaved → setPreviewSkill.
    // The reset effect must NOT fire (dep array is keyed on skill.name only)
    // and the new chars must be preserved in the editor.
    fireEvent.change(editor, {
      target: { value: "second body and more" },
    });

    await waitFor(() => {
      expect((editor as HTMLTextAreaElement).value).toBe(
        "second body and more",
      );
    });
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

    // Active cards don't surface the View full link in v1; the SidePanel
    // is reachable only on proposed cards. So the editor textarea must
    // not be in the DOM.
    await waitFor(() => {
      expect(screen.getByText("live-skill")).toBeInTheDocument();
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

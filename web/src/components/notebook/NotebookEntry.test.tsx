import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NotebookEntry } from "../../api/notebook";
import * as api from "../../api/notebook";
import * as richApi from "../../api/richArtifacts";
import NotebookEntryView from "./NotebookEntry";

const DRAFT_ENTRY: NotebookEntry = {
  agent_slug: "pm",
  entry_slug: "customer-acme-rough-notes",
  title: "Customer Acme — rough notes",
  subtitle: "Thursday, April 20th · working draft",
  body_md: "## First\n\nBody content here.",
  last_edited_ts: new Date().toISOString(),
  revisions: 3,
  status: "draft",
  file_path: "~/.wuphf/wiki/agents/pm/notebook/2026-04-20.md",
  reviewer_slug: "ceo",
};

const PROMOTED_ENTRY: NotebookEntry = {
  ...DRAFT_ENTRY,
  status: "promoted",
  promoted_to_path: "playbooks/customer-onboarding",
};

describe("<NotebookEntryView>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([]);
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue({
      artifact: {
        id: "ra_0123456789abcdef",
        kind: "notebook_html",
        title: "Visual plan",
        summary: "A richer plan.",
        trustLevel: "draft",
        representation: "html",
        htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
        sourceMarkdownPath: DRAFT_ENTRY.file_path,
        createdBy: "pm",
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        contentHash: "hash",
        sanitizerVersion: "sandbox-v1",
      },
      html: "<h1>Inline visual</h1>",
    });
  });

  it("renders title, subtitle, and DRAFT stamp for a draft entry", () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />);
    expect(
      screen.getByRole("heading", { name: "Customer Acme — rough notes" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Thursday, April 20th · working draft"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("img", { name: "Draft entry, not yet reviewed" }),
    ).toBeInTheDocument();
  });

  it("does NOT render the DRAFT stamp for a promoted entry", () => {
    render(<NotebookEntryView entry={PROMOTED_ENTRY} />);
    expect(
      screen.queryByRole("img", { name: /Draft entry/ }),
    ).not.toBeInTheDocument();
  });

  it("carries the draft aria-label on main", () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />);
    expect(
      screen.getByLabelText(
        "Draft: Customer Acme — rough notes. Not yet reviewed.",
      ),
    ).toBeInTheDocument();
  });

  it("renders markdown body from the entry", () => {
    render(<NotebookEntryView entry={DRAFT_ENTRY} />);
    expect(
      screen.getByRole("heading", { name: "First", level: 2 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Body content here.")).toBeInTheDocument();
  });

  it("renders promoted-back callout when the entry has a back-link", () => {
    const withBack: NotebookEntry = {
      ...DRAFT_ENTRY,
      promoted_back: {
        section: "onboarding gotchas",
        promoted_to_path: "playbooks/customer-onboarding",
        promoted_by_slug: "ceo",
        promoted_ts: new Date().toISOString(),
      },
    };
    render(<NotebookEntryView entry={withBack} />);
    expect(screen.getByText("onboarding gotchas")).toBeInTheDocument();
  });

  it("renders visual artifact cards attached to the notebook entry", async () => {
    vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([
      {
        id: "ra_0123456789abcdef",
        kind: "notebook_html",
        title: "Visual plan",
        summary: "A richer plan.",
        trustLevel: "draft",
        representation: "html",
        htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
        sourceMarkdownPath: DRAFT_ENTRY.file_path,
        createdBy: "pm",
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        contentHash: "hash",
        sanitizerVersion: "sandbox-v1",
      },
    ]);

    render(<NotebookEntryView entry={DRAFT_ENTRY} />);

    expect(
      await screen.findByRole("heading", { name: "Visual artifacts" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Visual plan").length).toBeGreaterThan(0);
    expect(
      await screen.findByTestId("nb-visual-artifact-inline"),
    ).toBeInTheDocument();
    expect(screen.getByTitle("Visual plan")).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("Inline visual"),
    );
  });

  it("re-promotes visual artifacts by replacing the default wiki target", async () => {
    const user = userEvent.setup();
    const artifact = {
      id: "ra_0123456789abcdef",
      kind: "notebook_html" as const,
      title: "Visual plan",
      summary: "A richer plan.",
      trustLevel: "draft" as const,
      representation: "html" as const,
      htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
      sourceMarkdownPath: DRAFT_ENTRY.file_path,
      createdBy: "pm",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      contentHash: "hash",
      sanitizerVersion: "sandbox-v2",
    };
    vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([artifact]);
    const promoteSpy = vi
      .spyOn(richApi, "promoteRichArtifact")
      .mockResolvedValue({
        ...artifact,
        kind: "wiki_visual",
        trustLevel: "promoted",
        promotedWikiPath: "team/drafts/pm-customer-acme-rough-notes-visual.md",
      });

    render(<NotebookEntryView entry={DRAFT_ENTRY} />);

    await screen.findByRole("heading", { name: "Visual artifacts" });
    await user.click(screen.getByRole("button", { name: "Open" }));
    await user.click(await screen.findByRole("button", { name: "Promote" }));

    await waitFor(() =>
      expect(promoteSpy).toHaveBeenCalledWith(
        "ra_0123456789abcdef",
        expect.objectContaining({ mode: "replace" }),
      ),
    );
  });

  it("transitions to pending-pill state after Promote click", async () => {
    const promoteSpy = vi.spyOn(api, "promoteEntry").mockResolvedValue({
      id: "mock",
      agent_slug: DRAFT_ENTRY.agent_slug,
      entry_slug: DRAFT_ENTRY.entry_slug,
      entry_title: DRAFT_ENTRY.title,
      proposed_wiki_path: "drafts/pm-customer-acme-rough-notes",
      excerpt: "",
      reviewer_slug: "ceo",
      state: "pending",
      submitted_ts: new Date().toISOString(),
      updated_ts: new Date().toISOString(),
      comments: [],
    });
    render(<NotebookEntryView entry={DRAFT_ENTRY} />);
    await userEvent.setup().click(
      screen.getByRole("button", {
        name: /Submit this draft for review by CEO/,
      }),
    );
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /Pending review by CEO/ }),
      ).toBeDisabled();
    });
    expect(promoteSpy).toHaveBeenCalledWith(
      "pm",
      "customer-acme-rough-notes",
      expect.any(Object),
    );
  });
});

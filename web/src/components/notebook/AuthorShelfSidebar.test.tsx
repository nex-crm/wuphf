import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  NotebookAgentSummary,
  NotebookEntrySummary,
} from "../../api/notebook";
import * as richApi from "../../api/richArtifacts";
import AuthorShelfSidebar from "./AuthorShelfSidebar";

beforeEach(() => {
  // Default to an empty artifact list so the per-test focus stays on the
  // markdown-entry shelf. Per-test overrides cover the new artifact UI.
  vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([]);
});

afterEach(() => {
  vi.restoreAllMocks();
});

const AGENT: NotebookAgentSummary = {
  agent_slug: "pm",
  name: "PM",
  role: "Product Manager · agent",
  entries: [],
  total: 2,
  promoted_count: 1,
  last_updated_ts: new Date().toISOString(),
};

const ENTRIES: NotebookEntrySummary[] = [
  {
    entry_slug: "customer-acme-rough-notes",
    title: "Customer Acme — rough notes",
    last_edited_ts: new Date().toISOString(),
    status: "draft",
  },
  {
    entry_slug: "onboarding-gotchas-checklist",
    title: "Onboarding gotchas checklist",
    last_edited_ts: new Date(Date.now() - 60 * 60 * 26 * 1000).toISOString(),
    status: "promoted",
  },
];

describe("<AuthorShelfSidebar>", () => {
  it("renders author label and role", () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "PM's notebook" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Product Manager · agent")).toBeInTheDocument();
  });

  it("lists entries grouped by day with their title", () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText("Customer Acme — rough notes")).toBeInTheDocument();
    expect(
      screen.getByText("Onboarding gotchas checklist"),
    ).toBeInTheDocument();
  });

  it("highlights the current entry with aria-current", () => {
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug="customer-acme-rough-notes"
        onSelect={() => {}}
      />,
    );
    const current = screen
      .getByText("Customer Acme — rough notes")
      .closest("button");
    expect(current).toHaveAttribute("aria-current", "page");
  });

  it("invokes onSelect when an entry is clicked", async () => {
    const onSelect = vi.fn();
    render(
      <AuthorShelfSidebar
        agent={AGENT}
        entries={ENTRIES}
        currentEntrySlug={null}
        onSelect={onSelect}
      />,
    );
    await userEvent
      .setup()
      .click(screen.getByText("Customer Acme — rough notes"));
    expect(onSelect).toHaveBeenCalledWith("customer-acme-rough-notes");
  });

  it("shows an empty state when no entries exist", () => {
    render(
      <AuthorShelfSidebar
        agent={{ ...AGENT, entries: [], total: 0 }}
        entries={[]}
        currentEntrySlug={null}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText("No entries yet.")).toBeInTheDocument();
  });

  describe("visual artifacts section", () => {
    const baseArtifact: richApi.RichArtifact = {
      id: "ra_aaaaaaaaaaaaaaaa",
      kind: "notebook_html",
      title: "Coffee extraction yield map",
      summary: "Diagram of yield vs flavor.",
      trustLevel: "draft",
      representation: "html",
      htmlPath: "wiki/visual-artifacts/ra_aaaaaaaaaaaaaaaa.html",
      createdBy: "pm",
      createdAt: "2026-05-20T12:00:00Z",
      updatedAt: "2026-05-20T12:00:00Z",
      contentHash: "h",
      sanitizerVersion: "sandbox-v2",
    };

    it("renders artifacts sorted most-recent-first", async () => {
      vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([
        { ...baseArtifact, id: "ra_old", createdAt: "2026-05-01T00:00:00Z" },
        { ...baseArtifact, id: "ra_new", createdAt: "2026-05-28T00:00:00Z" },
      ]);
      render(
        <AuthorShelfSidebar
          agent={AGENT}
          entries={ENTRIES}
          currentEntrySlug={null}
          onSelect={() => {}}
        />,
      );
      await waitFor(() => {
        expect(screen.getByLabelText("Visual artifacts")).toBeInTheDocument();
      });
      const items = screen.getAllByRole("button", {
        name: /Open visual artifact:/,
      });
      expect(items).toHaveLength(2);
      // Most-recent first: ra_new before ra_old.
      expect(items[0].closest("li")?.getAttribute("data-testid")).toBe(
        "nb-shelf-artifact-ra_new",
      );
      expect(items[1].closest("li")?.getAttribute("data-testid")).toBe(
        "nb-shelf-artifact-ra_old",
      );
    });

    it("navigates to the wiki when an artifact promoted to wiki is clicked", async () => {
      vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([
        {
          ...baseArtifact,
          promotion: {
            status: "promoted_to_wiki",
            wiki_path: "team/reference/coffee.md",
          },
        },
      ]);
      const user = userEvent.setup();
      window.location.hash = "";
      render(
        <AuthorShelfSidebar
          agent={AGENT}
          entries={ENTRIES}
          currentEntrySlug={null}
          onSelect={() => {}}
        />,
      );
      const link = await screen.findByRole("button", {
        name: "Open visual artifact: Coffee extraction yield map",
      });
      await user.click(link);
      await waitFor(() => {
        expect(window.location.hash).toBe("#/wiki/team/reference/coffee");
      });
    });

    it("hides the section when the agent has no artifacts", async () => {
      vi.spyOn(richApi, "fetchRichArtifacts").mockResolvedValue([]);
      render(
        <AuthorShelfSidebar
          agent={AGENT}
          entries={ENTRIES}
          currentEntrySlug={null}
          onSelect={() => {}}
        />,
      );
      // No section appears when the list is empty.
      expect(screen.queryByLabelText("Visual artifacts")).toBeNull();
    });

    it("surfaces a load failure inline without breaking the shelf", async () => {
      vi.spyOn(richApi, "fetchRichArtifacts").mockRejectedValue(
        new Error("boom"),
      );
      render(
        <AuthorShelfSidebar
          agent={AGENT}
          entries={ENTRIES}
          currentEntrySlug={null}
          onSelect={() => {}}
        />,
      );
      await waitFor(() => {
        expect(
          screen.getByText(/Could not load artifacts: boom/),
        ).toBeInTheDocument();
      });
      // The markdown shelf still rendered.
      expect(
        screen.getByText("Customer Acme — rough notes"),
      ).toBeInTheDocument();
    });
  });
});

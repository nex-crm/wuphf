import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  NotebookPromotionRequestedCard,
  notebookEntryFromSourcePath,
} from "./NotebookPromotionRequestedCard";

const navigate = vi.fn();
vi.mock("../../../lib/router", () => ({
  router: { navigate: (...args: unknown[]) => navigate(...args) },
}));

describe("notebookEntryFromSourcePath", () => {
  it("parses agents/<slug>/notebook/<entry>.md", () => {
    expect(
      notebookEntryFromSourcePath("agents/pm/notebook/acme-brief.md"),
    ).toEqual({ agentSlug: "pm", entrySlug: "acme-brief" });
  });

  it("returns null for unparseable paths", () => {
    expect(notebookEntryFromSourcePath("")).toBeNull();
    expect(notebookEntryFromSourcePath("team/wiki/page.md")).toBeNull();
    expect(notebookEntryFromSourcePath("agents/pm/notes/x.md")).toBeNull();
  });
});

describe("NotebookPromotionRequestedCard", () => {
  afterEach(() => {
    cleanup();
    navigate.mockReset();
  });

  it("opens the notebook item itself when source_path is parseable", () => {
    render(
      <NotebookPromotionRequestedCard
        payload={{
          promotion_id: "p1",
          source_path: "agents/pm/notebook/acme-brief.md",
          target_path: "team/accounts/acme.md",
        }}
      />,
    );
    fireEvent.click(screen.getByTestId("notebook-promotion-requested-card"));
    expect(navigate).toHaveBeenCalledWith({
      to: "/notebooks/$agentSlug/$entrySlug",
      params: { agentSlug: "pm", entrySlug: "acme-brief" },
    });
  });

  it("falls back to /reviews when source_path is missing", () => {
    render(<NotebookPromotionRequestedCard payload={{ promotion_id: "p2" }} />);
    fireEvent.click(screen.getByTestId("notebook-promotion-requested-card"));
    expect(navigate).toHaveBeenCalledWith({ to: "/reviews" });
  });
});

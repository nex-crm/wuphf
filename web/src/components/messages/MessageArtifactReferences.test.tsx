import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as richApi from "../../api/richArtifacts";
import MessageArtifactReferences from "./MessageArtifactReferences";

function makeDetail(
  overrides: Partial<richApi.RichArtifact> = {},
): richApi.RichArtifactDetail {
  return {
    artifact: {
      id: "ra_0123456789abcdef",
      kind: "notebook_html",
      title: "Coffee extraction science",
      summary: "How extraction yield maps to flavor.",
      trustLevel: "draft",
      representation: "html",
      htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
      sourceMarkdownPath: "agents/barista/notebook/coffee.md",
      createdBy: "barista",
      createdAt: "2026-05-16T12:00:00Z",
      updatedAt: "2026-05-16T12:00:00Z",
      contentHash: "hash",
      sanitizerVersion: "sandbox-v2",
      ...overrides,
    },
    html: "<h1>Body</h1>",
  };
}

function renderWithQueryClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

describe("<MessageArtifactReferences> destination routing", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    window.location.hash = "";
  });

  it("navigates to the wiki splat when the artifact is promoted to wiki", async () => {
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(
      makeDetail({
        promotion: {
          status: "promoted_to_wiki",
          wiki_path: "team/reference/coffee-extraction-science.md",
        },
      }),
    );
    const user = userEvent.setup();
    renderWithQueryClient(
      <MessageArtifactReferences artifactIds={["ra_0123456789abcdef"]} />,
    );
    const card = await screen.findByRole("button", {
      name: "Open article: Coffee extraction science",
    });
    await user.click(card);
    await waitFor(() => {
      expect(window.location.hash).toBe(
        "#/wiki/team/reference/coffee-extraction-science",
      );
    });
  });

  it("navigates to the notebook entry when the artifact is promoted to a notebook", async () => {
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(
      makeDetail({
        promotion: {
          status: "promoted_to_notebook",
          owner_slug: "barista",
          entry_slug: "extraction-yield",
        },
      }),
    );
    const user = userEvent.setup();
    renderWithQueryClient(
      <MessageArtifactReferences artifactIds={["ra_0123456789abcdef"]} />,
    );
    const card = await screen.findByRole("button", {
      name: "Open article: Coffee extraction science",
    });
    await user.click(card);
    await waitFor(() => {
      expect(window.location.hash).toBe("#/notebooks/barista/extraction-yield");
    });
  });

  it("navigates to the notebook entry home for a draft attached to a notebook", async () => {
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(
      makeDetail({
        promotion: { status: "draft" },
        attached_to_notebook_entry: {
          owner_slug: "pm",
          entry_slug: "kickoff-notes",
        },
      }),
    );
    const user = userEvent.setup();
    renderWithQueryClient(
      <MessageArtifactReferences artifactIds={["ra_0123456789abcdef"]} />,
    );
    const card = await screen.findByRole("button", {
      name: "Open article: Coffee extraction science",
    });
    await user.click(card);
    await waitFor(() => {
      expect(window.location.hash).toBe("#/notebooks/pm/kickoff-notes");
    });
  });

  it("falls back to /articles/$id for unpromoted drafts", async () => {
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(
      makeDetail({ promotion: { status: "draft" } }),
    );
    const user = userEvent.setup();
    renderWithQueryClient(
      <MessageArtifactReferences artifactIds={["ra_0123456789abcdef"]} />,
    );
    const card = await screen.findByRole("button", {
      name: "Open article: Coffee extraction science",
    });
    await user.click(card);
    await waitFor(() => {
      expect(window.location.hash).toBe("#/articles/ra_0123456789abcdef");
    });
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import {
  createRichArtifact,
  fetchRichArtifact,
  fetchRichArtifacts,
  fetchWikiVisualArtifact,
  promoteRichArtifact,
  type RichArtifact,
} from "./richArtifacts";

const ARTIFACT: RichArtifact = {
  id: "ra_0123456789abcdef",
  kind: "notebook_html",
  title: "Visual plan",
  summary: "A richer plan.",
  trustLevel: "draft",
  representation: "html",
  htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
  sourceMarkdownPath: "agents/pm/notebook/plan.md",
  createdBy: "pm",
  createdAt: "2026-05-12T08:00:00Z",
  updatedAt: "2026-05-12T08:00:00Z",
  contentHash: "hash",
  sanitizerVersion: "sandbox-v1",
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("rich artifact API", () => {
  it("creates notebook visual artifacts with backend snake_case fields", async () => {
    const postSpy = vi
      .spyOn(client, "post")
      .mockResolvedValue({ artifact: ARTIFACT });

    const artifact = await createRichArtifact({
      slug: "pm",
      title: "Visual plan",
      summary: "A richer plan.",
      html: "<html></html>",
      sourceMarkdownPath: "agents/pm/notebook/plan.md",
      relatedReceiptIds: ["rcpt-1"],
    });

    expect(postSpy).toHaveBeenCalledWith("/notebook/visual-artifacts", {
      slug: "pm",
      title: "Visual plan",
      summary: "A richer plan.",
      html: "<html></html>",
      source_markdown_path: "agents/pm/notebook/plan.md",
      related_task_id: undefined,
      related_message_id: undefined,
      related_receipt_ids: ["rcpt-1"],
      commit_message: undefined,
    });
    expect(artifact.id).toBe(ARTIFACT.id);
  });

  it("lists visual artifacts by source path", async () => {
    const getSpy = vi
      .spyOn(client, "get")
      .mockResolvedValue({ artifacts: [ARTIFACT] });

    const artifacts = await fetchRichArtifacts({
      sourceMarkdownPath: "agents/pm/notebook/plan.md",
    });

    expect(getSpy).toHaveBeenCalledWith("/notebook/visual-artifacts", {
      source_path: "agents/pm/notebook/plan.md",
    });
    expect(artifacts).toHaveLength(1);
  });

  it("fetches a visual artifact detail", async () => {
    vi.spyOn(client, "get").mockResolvedValue({
      artifact: ARTIFACT,
      html: "<h1>Visual</h1>",
    });

    const detail = await fetchRichArtifact(ARTIFACT.id);

    expect(detail.html).toContain("Visual");
  });

  it("promotes visual artifacts into wiki articles", async () => {
    const promoted: RichArtifact = {
      ...ARTIFACT,
      kind: "wiki_visual",
      trustLevel: "promoted",
      promotedWikiPath: "team/drafts/visual-plan.md",
    };
    const postSpy = vi
      .spyOn(client, "post")
      .mockResolvedValue({ artifact: promoted });

    const result = await promoteRichArtifact(ARTIFACT.id, {
      targetWikiPath: "team/drafts/visual-plan.md",
      markdownSummary: "# Visual plan\n",
    });

    expect(postSpy).toHaveBeenCalledWith(
      "/notebook/visual-artifacts/ra_0123456789abcdef/promote",
      {
        target_wiki_path: "team/drafts/visual-plan.md",
        markdown_summary: "# Visual plan\n",
        mode: "create",
        commit_message: undefined,
      },
    );
    expect(result.trustLevel).toBe("promoted");
  });

  it("returns null when a wiki article has no visual view", async () => {
    vi.spyOn(client, "get").mockRejectedValue(new Error("not found"));

    await expect(fetchWikiVisualArtifact("team/drafts/x.md")).resolves.toBe(
      null,
    );
  });

  it("surfaces unexpected wiki visual fetch failures", async () => {
    vi.spyOn(client, "get").mockRejectedValue(new Error("network down"));
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    await expect(fetchWikiVisualArtifact("team/drafts/x.md")).rejects.toThrow(
      "network down",
    );
    expect(warnSpy).toHaveBeenCalledWith(
      "Failed to fetch wiki visual artifact",
      expect.any(Error),
    );
  });
});

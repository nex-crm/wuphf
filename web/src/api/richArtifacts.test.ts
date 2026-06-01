import { afterEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import {
  createRichArtifact,
  fetchRichArtifact,
  fetchRichArtifacts,
  fetchWikiVisualArtifact,
  promoteRichArtifact,
  type RichArtifact,
  resolveArtifactDestination,
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

  describe("resolveArtifactDestination", () => {
    it("routes promoted_to_wiki artifacts to the wiki splat", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: {
          status: "promoted_to_wiki",
          wiki_path: "team/reference/coffee-extraction-science.md",
        },
      });
      expect(dest).toEqual({
        to: "/wiki/$",
        params: { _splat: "team/reference/coffee-extraction-science" },
      });
    });

    it("routes promoted_to_notebook artifacts to the notebook entry route", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: {
          status: "promoted_to_notebook",
          owner_slug: "pm",
          entry_slug: "handoff",
        },
      });
      expect(dest).toEqual({
        to: "/notebooks/$agentSlug/$entrySlug",
        params: { agentSlug: "pm", entrySlug: "handoff" },
      });
    });

    it("falls back to the standalone /articles route for drafts", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: { status: "draft" },
      });
      expect(dest).toEqual({
        to: "/articles/$articleId",
        params: { articleId: ARTIFACT.id },
      });
    });

    it("falls back to /articles when no promotion field is set", () => {
      const dest = resolveArtifactDestination({ id: ARTIFACT.id });
      expect(dest).toEqual({
        to: "/articles/$articleId",
        params: { articleId: ARTIFACT.id },
      });
    });

    it("routes draft artifacts to their attached notebook entry home", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: { status: "draft" },
        attached_to_notebook_entry: {
          owner_slug: "barista",
          entry_slug: "extraction-yield",
        },
      });
      expect(dest).toEqual({
        to: "/notebooks/$agentSlug/$entrySlug",
        params: { agentSlug: "barista", entrySlug: "extraction-yield" },
      });
    });

    it("attached_to_notebook_entry resolves even when promotion is absent", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        attached_to_notebook_entry: {
          owner_slug: "pm",
          entry_slug: "kickoff-notes",
        },
      });
      expect(dest).toEqual({
        to: "/notebooks/$agentSlug/$entrySlug",
        params: { agentSlug: "pm", entrySlug: "kickoff-notes" },
      });
    });

    it("promoted_to_wiki still wins over attached_to_notebook_entry", () => {
      // If the artifact was already promoted to the wiki, the wiki is the
      // canonical home; the notebook attachment is its historical origin.
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: {
          status: "promoted_to_wiki",
          wiki_path: "team/reference/coffee.md",
        },
        attached_to_notebook_entry: {
          owner_slug: "barista",
          entry_slug: "extraction-yield",
        },
      });
      expect(dest).toEqual({
        to: "/wiki/$",
        params: { _splat: "team/reference/coffee" },
      });
    });

    it("null attached_to_notebook_entry falls through to /articles", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: { status: "draft" },
        attached_to_notebook_entry: null,
      });
      expect(dest).toEqual({
        to: "/articles/$articleId",
        params: { articleId: ARTIFACT.id },
      });
    });

    it("legacy promotedWikiPath still routes to the wiki when promotion is absent", () => {
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotedWikiPath: "team/drafts/legacy.md",
      });
      expect(dest).toEqual({
        to: "/wiki/$",
        params: { _splat: "team/drafts/legacy" },
      });
    });

    it("legacy promotedWikiPath wins over attached_to_notebook_entry when promotion is absent", () => {
      // A backfilled legacy artifact can carry BOTH the legacy wiki path and
      // a notebook attachment. The wiki page is its canonical home, so the
      // wiki branch must win — mirroring the promoted_to_wiki precedence.
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotedWikiPath: "team/reference/coffee.md",
        attached_to_notebook_entry: {
          owner_slug: "barista",
          entry_slug: "extraction-yield",
        },
      });
      expect(dest).toEqual({
        to: "/wiki/$",
        params: { _splat: "team/reference/coffee" },
      });
    });

    it("explicit draft promotion overrides legacy promotedWikiPath", () => {
      // Older artifacts that were drafted, promoted, then unpromoted could
      // carry both fields. The new promotion field is the source of truth.
      const dest = resolveArtifactDestination({
        id: ARTIFACT.id,
        promotion: { status: "draft" },
        promotedWikiPath: "team/drafts/stale.md",
      });
      expect(dest).toEqual({
        to: "/articles/$articleId",
        params: { articleId: ARTIFACT.id },
      });
    });
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

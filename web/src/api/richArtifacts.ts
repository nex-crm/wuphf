import { get, post } from "./client";

export type RichArtifactKind = "notebook_html" | "wiki_visual";
export type RichArtifactTrustLevel = "draft" | "reviewed" | "promoted";

// ArtifactPromotion is the canonical "where does this artifact live now?"
// signal that drives link routing in chat bubbles and embed wiring in the
// notebook + wiki detail views. Backend contract (agreed with the Go side):
//
//   { status: "draft" }                                            // unpromoted, only at /articles/$id
//   { status: "promoted_to_notebook", owner_slug, entry_slug }     // attached to a notebook entry
//   { status: "promoted_to_wiki", wiki_path }                      // promoted into the team wiki
//
// wiki_path is relative to the wiki root, e.g.
// "team/reference/coffee-extraction-science.md".
export type ArtifactPromotion =
  | { status: "draft" }
  | {
      status: "promoted_to_notebook";
      owner_slug: string;
      entry_slug: string;
    }
  | { status: "promoted_to_wiki"; wiki_path: string };

export interface RichArtifact {
  id: string;
  kind: RichArtifactKind;
  title: string;
  summary: string;
  trustLevel: RichArtifactTrustLevel;
  representation: "html";
  htmlPath: string;
  sourceMarkdownPath?: string;
  promotedWikiPath?: string;
  relatedTaskId?: string;
  relatedMessageId?: string;
  relatedReceiptIds?: string[];
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  contentHash: string;
  sanitizerVersion: string;
  // promotion is the new canonical promotion-state field. It is optional
  // for backward compatibility with older broker responses that have not
  // been re-emitted yet; consumers should fall back to deriving a
  // best-effort state from promotedWikiPath when promotion is absent (see
  // resolveArtifactDestination).
  promotion?: ArtifactPromotion;
  // attached_to_notebook_entry is the artifact's notebook home, set by the
  // broker when the artifact is created against a source_markdown_path that
  // resolves to a known notebook entry. Even draft artifacts get this set,
  // so the chat link card can deep-link the user to the entry page (which
  // embeds the artifact inline via NotebookVisualArtifacts) instead of the
  // standalone /articles/$id viewer.
  attached_to_notebook_entry?: {
    owner_slug: string;
    entry_slug: string;
  } | null;
}

export interface RichArtifactDetail {
  artifact: RichArtifact;
  html: string;
}

export interface CreateRichArtifactParams {
  slug: string;
  title: string;
  summary?: string;
  html: string;
  sourceMarkdownPath?: string;
  relatedTaskId?: string;
  relatedMessageId?: string;
  relatedReceiptIds?: string[];
  commitMessage?: string;
}

export interface PromoteRichArtifactParams {
  targetWikiPath: string;
  markdownSummary: string;
  mode?: "create" | "replace" | "append_section";
  commitMessage?: string;
}

export async function createRichArtifact(
  params: CreateRichArtifactParams,
): Promise<RichArtifact> {
  const res = await post<{ artifact: RichArtifact }>(
    "/notebook/visual-artifacts",
    {
      slug: params.slug,
      title: params.title,
      summary: params.summary ?? "",
      html: params.html,
      source_markdown_path: params.sourceMarkdownPath,
      related_task_id: params.relatedTaskId,
      related_message_id: params.relatedMessageId,
      related_receipt_ids: params.relatedReceiptIds ?? [],
      commit_message: params.commitMessage,
    },
  );
  return res.artifact;
}

export async function fetchRichArtifacts(params: {
  slug?: string;
  sourceMarkdownPath?: string;
}): Promise<RichArtifact[]> {
  const query: Record<string, string> = {};
  if (params.slug) query.slug = params.slug;
  if (params.sourceMarkdownPath) {
    query.source_path = params.sourceMarkdownPath;
  }
  const res = await get<{ artifacts: RichArtifact[] }>(
    "/notebook/visual-artifacts",
    query,
  );
  return Array.isArray(res.artifacts) ? res.artifacts : [];
}

export async function fetchRichArtifact(
  id: string,
): Promise<RichArtifactDetail> {
  return await get<RichArtifactDetail>(
    `/notebook/visual-artifacts/${encodeURIComponent(id)}`,
  );
}

export async function promoteRichArtifact(
  id: string,
  params: PromoteRichArtifactParams,
): Promise<RichArtifact> {
  const res = await post<{ artifact: RichArtifact }>(
    `/notebook/visual-artifacts/${encodeURIComponent(id)}/promote`,
    {
      target_wiki_path: params.targetWikiPath,
      markdown_summary: params.markdownSummary,
      mode: params.mode ?? "create",
      commit_message: params.commitMessage,
    },
  );
  return res.artifact;
}

// ArtifactDestination describes where a clickable reference to an artifact
// should navigate. It is intentionally shaped like a router NavigateOptions
// payload (to + params) so call sites can splat it straight into
// router.navigate(). The resolver derives the destination from the new
// promotion field when present, then falls back to legacy fields, and
// finally to the standalone /articles/$id viewer.
export type ArtifactDestination =
  | {
      to: "/wiki/$";
      params: { _splat: string };
    }
  | {
      to: "/articles/$articleId";
      params: { articleId: string };
    };

// stripWikiSuffix lops off the trailing ".md" (if any) so the wiki splat
// matches the same shape the router uses for in-app navigation (e.g.
// "team/reference/coffee" instead of "team/reference/coffee.md"). The wiki
// route handler accepts both, but the trimmed form keeps the URL clean.
function stripWikiSuffix(path: string): string {
  return path.replace(/\.md$/i, "");
}

export function resolveArtifactDestination(
  artifact: Pick<
    RichArtifact,
    "id" | "promotion" | "promotedWikiPath" | "attached_to_notebook_entry"
  >,
): ArtifactDestination {
  const promotion = artifact.promotion;
  if (promotion?.status === "promoted_to_wiki" && promotion.wiki_path) {
    return {
      to: "/wiki/$",
      params: { _splat: stripWikiSuffix(promotion.wiki_path) },
    };
  }
  // Legacy fallback for artifacts emitted before the promotion field
  // existed: if we know a wiki path was set, route there. Runs only when no
  // promotion field is present so an explicit `draft` promotion still wins.
  // The notebook surface has been retired, so notebook-promoted /
  // notebook-attached artifacts fall through to the standalone /articles/$id
  // viewer below.
  if (!promotion && artifact.promotedWikiPath) {
    return {
      to: "/wiki/$",
      params: { _splat: stripWikiSuffix(artifact.promotedWikiPath) },
    };
  }
  // Draft / notebook / unknown / unpromoted: fall back to the standalone
  // viewer.
  return {
    to: "/articles/$articleId",
    params: { articleId: artifact.id },
  };
}

export async function fetchWikiVisualArtifact(
  path: string,
): Promise<RichArtifactDetail | null> {
  try {
    return await get<RichArtifactDetail>("/wiki/visual", { path });
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    if (/404|not found/i.test(message)) {
      return null;
    }
    console.warn("Failed to fetch wiki visual artifact", err);
    throw err;
  }
}

import { get, post } from "./client";

export type RichArtifactKind = "notebook_html" | "wiki_visual";
export type RichArtifactTrustLevel = "draft" | "reviewed" | "promoted";

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

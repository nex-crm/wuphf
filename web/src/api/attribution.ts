import { get } from "./client";

// Article ↔ task attribution. Wiki content is the artifacts agents produce;
// this resolves which task produced a given article so the article view can
// show it. Served by GET /article-attribution (snake_case wire).

export interface ArticleAttribution {
  taskId: string;
  taskTitle: string;
  owner?: string;
}

interface WireAttribution {
  task_id: string;
  task_title: string;
  owner?: string;
}

/**
 * Resolve the producing task for an article. `ref` is a visual-artifact id
 * ("ra_…") or a wiki-relative article path. Returns null when no producing
 * task is recorded.
 */
export async function fetchArticleAttribution(
  ref: string,
): Promise<ArticleAttribution | null> {
  const res = await get<{ attribution: WireAttribution | null }>(
    "/article-attribution",
    { ref },
  );
  const a = res.attribution;
  if (!(a && a.task_id)) return null;
  return { taskId: a.task_id, taskTitle: a.task_title, owner: a.owner };
}

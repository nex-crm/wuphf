// knowledgeClient — reads the app's REAL synthesized knowledge pages from the
// broker. The broker gathers the app's real artifacts (spec, data model, source,
// roster) and synthesizes cited Wikipedia-style pages grounded in them; the tab
// renders them exactly like the mock. Cached server-side after first synthesis.

import { get } from "../../api/client";
import type { KnowledgePage } from "../mock/data";

export interface AppKnowledgeResult {
  pages: KnowledgePage[];
  /** "ai_unavailable" (no provider / nothing to synthesize) or "rate_limited". */
  error?: string;
}

/**
 * getAppKnowledge fetches the app's cited knowledge pages. First open triggers a
 * grounded synthesis (a few seconds); it is cached after, so later reads are
 * instant. Pass refresh to force a re-synthesis.
 */
export async function getAppKnowledge(
  appId: string,
  refresh = false,
): Promise<AppKnowledgeResult> {
  const res = await get<{ pages?: KnowledgePage[]; error?: string }>(
    `/apps/${encodeURIComponent(appId)}/knowledge${refresh ? "?refresh=1" : ""}`,
  );
  return { pages: res.pages ?? [], error: res.error };
}

import { useQuery } from "@tanstack/react-query";

import { fetchArticleAttribution } from "../../api/attribution";
import "../../styles/article-attribution.css";

interface ArticleAttributionProps {
  /** A visual-artifact id ("ra_…") or a wiki-relative article path. */
  articleRef: string;
}

/**
 * "Produced for <task>" provenance line shown on an article. Wiki content is
 * the work agents produce; this surfaces the task that produced it, in-place.
 * Renders nothing while loading or when no producing task is recorded, so it
 * is safe to drop into any article header.
 */
export function ArticleAttribution({ articleRef }: ArticleAttributionProps) {
  const { data } = useQuery({
    queryKey: ["article-attribution", articleRef],
    queryFn: () => fetchArticleAttribution(articleRef),
    enabled: articleRef.trim().length > 0,
    staleTime: 60_000,
  });

  if (!data) return null;

  return (
    <a
      className="article-attribution"
      href={`#/tasks/${encodeURIComponent(data.taskId)}`}
      data-testid="article-attribution"
      title={`Produced for ${data.taskId}: ${data.taskTitle}`}
    >
      <span className="article-attribution-label">Produced for</span>
      <span className="article-attribution-task">
        {data.taskId} · {data.taskTitle}
      </span>
      {data.owner ? (
        <span className="article-attribution-owner">@{data.owner}</span>
      ) : null}
    </a>
  );
}

import { formatAgentName } from "../../lib/agentName";
import PixelAvatar from "./PixelAvatar";

/** Numbered references — each entry is one git commit that informed the article. */

export interface SourceItem {
  commitSha: string;
  authorSlug: string;
  authorName?: string;
  msg: string;
  date: string;
}

interface SourcesProps {
  items: SourceItem[];
  /** When true, shows an inline placeholder while history is fetching. */
  loading?: boolean;
}

export default function Sources({ items, loading = false }: SourcesProps) {
  if (loading && items.length === 0) {
    return (
      <section className="wk-sources" aria-labelledby="wk-sources-heading">
        <h2 id="wk-sources-heading">Sources</h2>
        <p className="wk-sources-loading">loading sources…</p>
      </section>
    );
  }
  if (items.length === 0) return null;
  return (
    <section className="wk-sources" aria-labelledby="wk-sources-heading">
      <h2 id="wk-sources-heading">Sources</h2>
      <ol>
        {items.map((item, i) => (
          <li key={item.commitSha || `src-${i}`} id={`s${i + 1}`}>
            <span className="wk-commit-msg">{item.msg}</span>
            <span className="wk-agent">
              <PixelAvatar slug={item.authorSlug} size={12} />
              {item.authorName || formatAgentName(item.authorSlug)}
              {" • "}
              <a href={`#/wiki/commit/${item.commitSha}`}>
                {item.commitSha.slice(0, 7)}
              </a>
              {" • "}
              {formatShortDate(item.date)}
            </span>
          </li>
        ))}
      </ol>
    </section>
  );
}

function formatShortDate(iso: string): string {
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toISOString().slice(0, 10);
  } catch {
    return iso;
  }
}

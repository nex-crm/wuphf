import { useMemo, useState } from "react";

import type { WikiCatalogEntry } from "../../api/wiki";
import { formatRelativeTime } from "../../lib/format";
import { resolveGroupOrder } from "../../lib/groupOrder";
import NewArticleModal from "./NewArticleModal";
import PixelAvatar from "./PixelAvatar";

/** `/wiki` landing view: grid of thematic dir groups with recent articles. */

interface WikiCatalogProps {
  catalog: WikiCatalogEntry[];
  onNavigate: (path: string) => void;
  onOpenAudit?: () => void;
  articlesCount?: number;
  commitsCount?: number;
  agentsCount?: number;
}

export default function WikiCatalog({
  catalog,
  onNavigate,
  onOpenAudit,
  articlesCount,
  commitsCount,
  agentsCount,
}: WikiCatalogProps) {
  const [showNew, setShowNew] = useState(false);
  const grouped = useMemo(() => groupByGroup(catalog), [catalog]);
  const groupOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  );
  // Top-decile threshold for the "verbose" prune-signal badge. We sort a
  // copy of the catalog by prune_score descending and read the score at
  // index `floor(len * 0.1)`. Any entry whose score is at or above the
  // threshold (and strictly greater than zero) earns the badge. Skipping
  // zero-score entries avoids painting the badge on a wiki where nothing
  // has been read yet.
  const verboseThreshold = useMemo(
    () => computeVerboseThreshold(catalog),
    [catalog],
  );
  const stats = useMemo(
    () =>
      [
        `${articlesCount ?? catalog.length} articles`,
        typeof commitsCount === "number" ? `${commitsCount} commits` : null,
        typeof agentsCount === "number"
          ? `${agentsCount} agents writing`
          : null,
      ]
        .filter(Boolean)
        .join(" · "),
    [catalog.length, articlesCount, commitsCount, agentsCount],
  );

  return (
    <main className="wk-catalog" data-testid="wk-catalog">
      <header className="wk-catalog-header">
        <h1 className="wk-catalog-title">Team Wiki</h1>
        <div className="wk-catalog-stats">{stats}</div>
        <div className="wk-catalog-clone">
          Your wiki lives on your disk. <code>git clone ~/.wuphf/wiki</code>
          {" · "}
          <button
            type="button"
            className="wk-catalog-new-link"
            data-testid="wk-catalog-new"
            onClick={() => setShowNew(true)}
          >
            + New article
          </button>
          {onOpenAudit ? (
            <>
              {" · "}
              <button
                type="button"
                className="wk-catalog-audit-link"
                onClick={(e) => {
                  e.preventDefault();
                  onOpenAudit();
                }}
              >
                Audit log
              </button>
            </>
          ) : null}
        </div>
      </header>
      {showNew ? (
        <NewArticleModal
          catalog={catalog}
          onCancel={() => setShowNew(false)}
          onCreated={(path) => {
            setShowNew(false);
            onNavigate(path);
          }}
        />
      ) : null}
      <div className="wk-catalog-grid">
        {groupOrder.map((group) => {
          const items = grouped[group];
          if (!items || items.length === 0) return null;
          return (
            <section key={group} className="wk-catalog-card">
              <h3>
                {group}
                <span className="wk-count">{items.length}</span>
              </h3>
              <ul>
                {items.slice(0, 6).map((item) => (
                  <li key={item.path}>
                    <PixelAvatar slug={item.author_slug} size={16} />
                    <a
                      className="wk-title"
                      href={`#/wiki/${item.path}`}
                      onClick={(e) => {
                        e.preventDefault();
                        onNavigate(item.path);
                      }}
                    >
                      {item.title}
                    </a>
                    {isVerbose(item, verboseThreshold) && (
                      <span
                        className="wk-staleness-badge wk-prune-verbose"
                        title={`Verbose: ${item.word_count ?? 0} words, ${
                          item.days_unread ?? 0
                        } days since last read`}
                        data-testid="wk-prune-verbose-badge"
                      >
                        verbose
                      </span>
                    )}
                    <span className="wk-when">
                      {safeRelative(item.last_edited_ts)}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          );
        })}
      </div>
    </main>
  );
}

/**
 * computeVerboseThreshold returns the prune_score at the top-decile cutoff
 * across the catalog. Returns 0 when the catalog is empty or no entry has
 * a positive score, which means the badge stays hidden. Sorting a copy
 * keeps the original catalog ordering stable.
 */
function computeVerboseThreshold(entries: WikiCatalogEntry[]): number {
  if (entries.length === 0) return 0;
  const sorted = [...entries].sort(
    (a, b) => (b.prune_score ?? 0) - (a.prune_score ?? 0),
  );
  const idx = Math.floor(entries.length * 0.1);
  const cutoff = sorted[idx]?.prune_score ?? 0;
  return cutoff;
}

function isVerbose(entry: WikiCatalogEntry, threshold: number): boolean {
  if (threshold <= 0) return false;
  const score = entry.prune_score ?? 0;
  // Strictly greater-than so the boundary entry (exactly at the 90th percentile
  // cutoff) does not earn the badge — only entries above it do.
  return score > threshold;
}

function groupByGroup(
  catalog: WikiCatalogEntry[],
): Record<string, WikiCatalogEntry[]> {
  const out: Record<string, WikiCatalogEntry[]> = {};
  for (const entry of catalog) {
    if (!out[entry.group]) out[entry.group] = [];
    out[entry.group].push(entry);
  }
  for (const k of Object.keys(out)) {
    out[k].sort((a, b) => (a.last_edited_ts < b.last_edited_ts ? 1 : -1));
  }
  return out;
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso);
  } catch {
    return iso;
  }
}

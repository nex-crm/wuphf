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
  catalogSort?: string;
  articlesCount?: number;
  commitsCount?: number;
  agentsCount?: number;
}

export default function WikiCatalog({
  catalog,
  onNavigate,
  onOpenAudit,
  catalogSort = "last_edited_ts",
  articlesCount,
  commitsCount,
  agentsCount,
}: WikiCatalogProps) {
  const [showNew, setShowNew] = useState(false);
  const grouped = useMemo(
    () => groupByGroup(catalog, catalogSort),
    [catalog, catalogSort],
  );
  const groupOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  );
  // Top-decile threshold for the "verbose" prune-signal badge. Only positive
  // scores participate so a sparse catalog can still surface a real outlier.
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
                    </a>
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
 * across the catalog. Returns 0 when there is no lower positive boundary,
 * which lets isolated positive outliers still earn the badge.
 */
function computeVerboseThreshold(entries: WikiCatalogEntry[]): number {
  const sorted = entries
    .map((entry) => entry.prune_score ?? 0)
    .filter((score) => score > 0)
    .sort((a, b) => b - a);
  if (sorted.length === 0) return 0;
  // For catalogs < 10 articles floor(n*0.1) is 0, which points at the highest
  // scorer and means the badge is never shown under strict >. Use at least 1
  // so the top entry in any non-empty catalog can qualify.
  const idx = Math.max(1, Math.floor(entries.length * 0.1));
  const cutoff = sorted[idx] ?? 0;
  return cutoff;
}

function isVerbose(entry: WikiCatalogEntry, threshold: number): boolean {
  const score = entry.prune_score ?? 0;
  // Strictly greater-than so the boundary entry (exactly at the 90th percentile
  // cutoff) does not earn the badge — only entries above it do.
  return score > 0 && score > threshold;
}

function groupByGroup(
  catalog: WikiCatalogEntry[],
  catalogSort: string,
): Record<string, WikiCatalogEntry[]> {
  const out: Record<string, WikiCatalogEntry[]> = {};
  for (const entry of catalog) {
    if (!out[entry.group]) out[entry.group] = [];
    out[entry.group].push(entry);
  }
  if (!catalogSort || catalogSort === "last_edited_ts") {
    for (const k of Object.keys(out)) {
      out[k].sort((a, b) =>
        a.last_edited_ts < b.last_edited_ts
          ? 1
          : a.last_edited_ts > b.last_edited_ts
            ? -1
            : 0,
      );
    }
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

import { useEffect, useMemo, useState } from "react";

import { fetchAuditLog, type WikiAuditEntry } from "../../api/wiki";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";
import PixelAvatar from "./PixelAvatar";

/**
 * Audit-log view at #/wiki/_audit.
 *
 * This is the compliance / "who edited what when" surface — distinct from
 * the per-article Sources panel (which scopes to one article) and the
 * bottom EditLogFooter (which is a live pulse of recent writes only).
 *
 * UX goals, in priority order:
 *   1. You can find one edit fast. Filters compose (author + path + since).
 *   2. You can see the shape of activity at a glance. Bootstrap commits
 *      are visually distinct from agent writes; system / recovery commits
 *      too. No color soup — it's a serious page.
 *   3. You can export. The CSV copy button dumps exactly what's filtered.
 *
 * Out of scope for v1: diff viewer, commit SHA → GitHub linking, tamper
 * detection sidecar. These are v1.1 items (see TESTING-WIKI.md).
 */
interface WikiAuditProps {
  onNavigate: (path: string | null) => void;
}

type AuthorBucket = "all" | "agents" | "system" | string;

export default function WikiAudit({ onNavigate }: WikiAuditProps) {
  const [entries, setEntries] = useState<WikiAuditEntry[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [authorFilter, setAuthorFilter] = useState<AuthorBucket>("all");
  const [pathQuery, setPathQuery] = useState("");
  const [sinceDays, setSinceDays] = useState<number | null>(null);
  const [limit, setLimit] = useState(200);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    const since =
      typeof sinceDays === "number"
        ? new Date(Date.now() - sinceDays * 86400 * 1000).toISOString()
        : undefined;
    fetchAuditLog({ limit, since })
      .then((res) => {
        if (cancelled) return;
        setEntries(res.entries ?? []);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load audit log",
        );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [limit, sinceDays]);

  const knownAuthors = useMemo(() => {
    const set = new Set<string>();
    for (const e of entries ?? []) set.add(e.author_slug);
    return Array.from(set).sort();
  }, [entries]);

  const filtered = useMemo(() => {
    if (!entries) return [];
    const q = pathQuery.trim().toLowerCase();
    return entries.filter((e) => {
      if (!passesAuthor(e.author_slug, authorFilter)) return false;
      if (q) {
        const hit =
          e.message.toLowerCase().includes(q) ||
          e.paths.some((p) => p.toLowerCase().includes(q));
        if (!hit) return false;
      }
      return true;
    });
  }, [entries, pathQuery, authorFilter]);

  const stats = useMemo(() => summarize(filtered), [filtered]);

  return (
    <main className="wk-audit" data-testid="wk-audit">
      <header className="wk-audit-header">
        <div>
          <h1 className="wk-audit-title">Audit log</h1>
          <p className="wk-audit-strapline">
            Every edit to the team wiki, newest first. Attribution comes from
            per-commit git identity — same data `git log` would give you on
            disk.
          </p>
        </div>
        <div className="wk-audit-stats" aria-live="polite">
          {loading
            ? "Loading…"
            : error
              ? "Error"
              : `${stats.total} entries · ${stats.authors} authors · ${stats.paths} paths touched`}
        </div>
      </header>

      <section className="wk-audit-filters" aria-label="Filters">
        <label className="wk-audit-filter">
          <span>Author</span>
          <select
            value={authorFilter}
            onChange={(e) => setAuthorFilter(e.target.value as AuthorBucket)}
          >
            <option value="all">All</option>
            <option value="agents">Agents only</option>
            <option value="system">System (bootstrap + recovery + init)</option>
            {knownAuthors.map((a) => (
              <option key={a} value={a}>
                @{a}
              </option>
            ))}
          </select>
        </label>
        <label className="wk-audit-filter">
          <span>Search</span>
          <input
            type="search"
            placeholder="Match message or path (e.g. playbooks, renewal, sarah)"
            value={pathQuery}
            onChange={(e) => setPathQuery(e.target.value)}
          />
        </label>
        <label className="wk-audit-filter">
          <span>Window</span>
          <select
            value={sinceDays ?? "all"}
            onChange={(e) => {
              const v = e.target.value;
              setSinceDays(v === "all" ? null : Number(v));
            }}
          >
            <option value="all">All time</option>
            <option value="1">Last 24 hours</option>
            <option value="7">Last 7 days</option>
            <option value="30">Last 30 days</option>
            <option value="90">Last 90 days</option>
          </select>
        </label>
        <label className="wk-audit-filter">
          <span>Limit</span>
          <select
            value={String(limit)}
            onChange={(e) => setLimit(Number(e.target.value))}
          >
            <option value="50">50</option>
            <option value="200">200</option>
            <option value="500">500</option>
            <option value="0">No limit</option>
          </select>
        </label>
        <button
          type="button"
          className="wk-audit-export"
          onClick={() => downloadCSV(filtered)}
          disabled={filtered.length === 0}
        >
          Export CSV
        </button>
      </section>

      {loading && !entries ? (
        <div className="wk-loading">Loading audit log…</div>
      ) : error ? (
        <div className="wk-error">Error: {error}</div>
      ) : filtered.length === 0 ? (
        <div className="wk-audit-empty">
          {entries && entries.length === 0
            ? "No edits yet. This page will populate as soon as any agent (or bootstrap pass) commits to the wiki."
            : "No entries match your filters."}
        </div>
      ) : (
        <table className="wk-audit-table">
          <thead>
            <tr>
              <th scope="col">When</th>
              <th scope="col">Author</th>
              <th scope="col">Message</th>
              <th scope="col">Paths</th>
              <th scope="col">SHA</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((e) => (
              <tr
                key={e.sha}
                className={`wk-audit-row ${rowClass(e.author_slug)}`}
              >
                <td className="wk-audit-when" title={e.timestamp}>
                  {safeRelative(e.timestamp)}
                </td>
                <td className="wk-audit-author">
                  <PixelAvatar slug={e.author_slug} size={16} />
                  <span>{formatAgentName(e.author_slug)}</span>
                  {authorTag(e.author_slug) && (
                    <span className="wk-audit-tag">
                      {authorTag(e.author_slug)}
                    </span>
                  )}
                </td>
                <td className="wk-audit-msg">{e.message}</td>
                <td className="wk-audit-paths">
                  {e.paths.length === 0 ? (
                    <span className="wk-audit-paths-empty">—</span>
                  ) : (
                    <ul>
                      {e.paths.map((p) => (
                        <li key={p}>
                          {isArticlePath(p) ? (
                            <a
                              href={`#/wiki/${encodeURI(p)}`}
                              onClick={(ev) => {
                                ev.preventDefault();
                                onNavigate(p);
                              }}
                            >
                              {p}
                            </a>
                          ) : (
                            <span>{p}</span>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </td>
                <td className="wk-audit-sha">{e.sha}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}

function passesAuthor(slug: string, filter: AuthorBucket): boolean {
  if (filter === "all") return true;
  if (filter === "system") return isSystemSlug(slug);
  if (filter === "agents") return !isSystemSlug(slug);
  return slug === filter;
}

function isSystemSlug(slug: string): boolean {
  return (
    slug === "system" || slug === "wuphf-bootstrap" || slug === "wuphf-recovery"
  );
}

function authorTag(slug: string): string | null {
  if (slug === "wuphf-bootstrap") return "bootstrap";
  if (slug === "wuphf-recovery") return "recovery";
  if (slug === "system") return "system";
  return null;
}

function rowClass(slug: string): string {
  if (slug === "wuphf-bootstrap") return "is-bootstrap";
  if (slug === "wuphf-recovery") return "is-recovery";
  if (slug === "system") return "is-system";
  return "is-agent";
}

function isArticlePath(p: string): boolean {
  return p.startsWith("team/") && p.endsWith(".md");
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso);
  } catch {
    return iso;
  }
}

function summarize(entries: WikiAuditEntry[]): {
  total: number;
  authors: number;
  paths: number;
} {
  const authors = new Set<string>();
  const paths = new Set<string>();
  for (const e of entries) {
    authors.add(e.author_slug);
    for (const p of e.paths) paths.add(p);
  }
  return { total: entries.length, authors: authors.size, paths: paths.size };
}

function downloadCSV(entries: WikiAuditEntry[]): void {
  const rows: string[] = ["timestamp,author,sha,message,paths"];
  for (const e of entries) {
    rows.push(
      [
        csvField(e.timestamp),
        csvField(e.author_slug),
        csvField(e.sha),
        csvField(e.message),
        csvField(e.paths.join(" | ")),
      ].join(","),
    );
  }
  const blob = new Blob([`${rows.join("\n")}\n`], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `wuphf-wiki-audit-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function csvField(raw: string): string {
  const s = String(raw ?? "");
  if (/[",\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

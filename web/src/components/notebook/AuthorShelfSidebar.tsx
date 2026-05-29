import { useEffect, useMemo, useState } from "react";

import type {
  NotebookAgentSummary,
  NotebookEntrySummary,
} from "../../api/notebook";
import {
  fetchRichArtifacts,
  type RichArtifact,
  resolveArtifactDestination,
} from "../../api/richArtifacts";
import { formatDateLabel } from "../../lib/format";
import { router } from "../../lib/router";
import { PixelAvatar } from "../ui/PixelAvatar";

/**
 * Left-hand author shelf for `/notebooks/{agent-slug}`. Shows the agent's
 * avatar + "PM's notebook" label, then a reverse-chron dated log of their
 * entries grouped by date header (Caveat display).
 */

interface AuthorShelfSidebarProps {
  agent: NotebookAgentSummary;
  entries: NotebookEntrySummary[];
  currentEntrySlug?: string | null;
  onSelect: (entrySlug: string) => void;
}

interface Group {
  label: string;
  key: string;
  items: NotebookEntrySummary[];
}

function groupByDay(entries: NotebookEntrySummary[]): Group[] {
  const groups: Record<string, Group> = {};
  const order: string[] = [];
  for (const e of entries) {
    const d = new Date(e.last_edited_ts);
    const key = Number.isNaN(d.getTime())
      ? "unknown"
      : `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    if (!groups[key]) {
      const label = Number.isNaN(d.getTime())
        ? "Unknown"
        : `${formatDateLabel(e.last_edited_ts)} · ${key}`;
      groups[key] = { label, key, items: [] };
      order.push(key);
    }
    groups[key].items.push(e);
  }
  order.sort((a, b) => (a < b ? 1 : -1));
  return order.map((k) => groups[k]);
}

function statusTag(
  status: NotebookEntrySummary["status"],
): { label: string; className: string } | null {
  if (status === "promoted")
    return { label: "→ Promoted", className: "nb-promoted" };
  if (status === "draft")
    return { label: "DRAFT", className: "nb-status-draft" };
  if (status === "in-review")
    return { label: "in review", className: "nb-status-review" };
  if (status === "changes-requested")
    return { label: "changes req.", className: "nb-status-changes" };
  return null;
}

function formatTimeOnly(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

// Sort the agent's rich artifacts most-recent-first using created_at, with
// a stable id-tiebreaker so identical timestamps stay deterministic across
// renders.
function sortArtifacts(artifacts: RichArtifact[]): RichArtifact[] {
  return [...artifacts].sort((a, b) => {
    const ta = new Date(a.createdAt).getTime();
    const tb = new Date(b.createdAt).getTime();
    if (Number.isNaN(ta) && Number.isNaN(tb)) return a.id.localeCompare(b.id);
    if (Number.isNaN(ta)) return 1;
    if (Number.isNaN(tb)) return -1;
    if (tb !== ta) return tb - ta;
    return a.id.localeCompare(b.id);
  });
}

export default function AuthorShelfSidebar({
  agent,
  entries,
  currentEntrySlug,
  onSelect,
}: AuthorShelfSidebarProps) {
  const groups = useMemo(() => groupByDay(entries), [entries]);
  const [artifacts, setArtifacts] = useState<RichArtifact[]>([]);
  const [artifactsError, setArtifactsError] = useState<string | null>(null);

  // Fetch the agent's rich (HTML) artifacts. These are stored at
  // .wuphf/wiki/wiki/visual-artifacts/ra_*.{html,json} and surfaced through
  // /notebook/visual-artifacts?slug=<agent>. Listing them next to the
  // markdown entries is how the notebook becomes the single home for
  // everything the agent has authored — markdown drafts and HTML visuals
  // both. Failure is silent (the shelf still renders the markdown column).
  useEffect(() => {
    let cancelled = false;
    setArtifactsError(null);
    setArtifacts([]);
    fetchRichArtifacts({ slug: agent.agent_slug })
      .then((items) => {
        if (cancelled) return;
        setArtifacts(sortArtifacts(items));
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setArtifactsError(
          err instanceof Error ? err.message : "Failed to load artifacts",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [agent.agent_slug]);

  return (
    <aside className="nb-shelf" aria-label={`${agent.name}'s notebook entries`}>
      <div className="nb-shelf-head">
        <PixelAvatar slug={agent.agent_slug} size={22} />
        <div>
          <h2>{agent.name}'s notebook</h2>
          <div className="nb-shelf-role">{agent.role}</div>
        </div>
      </div>
      {entries.length === 0 ? (
        <p className="nb-shelf-empty">No entries yet.</p>
      ) : (
        <ul className="nb-shelf-list">
          {groups.flatMap((g) => [
            <li key={`head-${g.key}`} className="nb-date-head">
              {g.label}
            </li>,
            ...g.items.map((item) => {
              const tag = statusTag(item.status);
              const isCurrent = item.entry_slug === currentEntrySlug;
              return (
                <li
                  key={item.entry_slug}
                  style={{ padding: 0, listStyle: "none" }}
                >
                  <button
                    type="button"
                    className={`nb-shelf-item${isCurrent ? " is-current" : ""}`}
                    onClick={() => onSelect(item.entry_slug)}
                    aria-current={isCurrent ? "page" : undefined}
                  >
                    <span className="nb-shelf-t">{item.title}</span>
                    <span className="nb-shelf-meta">
                      {formatTimeOnly(item.last_edited_ts)}
                      {tag && (
                        <>
                          {" · "}
                          <span className={tag.className}>{tag.label}</span>
                        </>
                      )}
                    </span>
                  </button>
                </li>
              );
            }),
          ])}
        </ul>
      )}
      <RichArtifactsShelfSection artifacts={artifacts} error={artifactsError} />
    </aside>
  );
}

interface RichArtifactsShelfSectionProps {
  artifacts: RichArtifact[];
  error: string | null;
}

function RichArtifactsShelfSection({
  artifacts,
  error,
}: RichArtifactsShelfSectionProps) {
  if (error) {
    return (
      <section
        className="nb-shelf-artifacts"
        aria-label="Visual artifacts"
        data-test-rich-artifacts-list="error"
      >
        <h3 className="nb-shelf-section-head">Visual artifacts</h3>
        <p className="nb-shelf-empty" role="alert">
          Could not load artifacts: {error}
        </p>
      </section>
    );
  }
  if (artifacts.length === 0) return null;
  return (
    <section
      className="nb-shelf-artifacts"
      aria-label="Visual artifacts"
      data-test-rich-artifacts-list="ok"
    >
      <h3 className="nb-shelf-section-head">Visual artifacts</h3>
      <ul className="nb-shelf-list">
        {artifacts.map((artifact) => (
          <li
            key={artifact.id}
            style={{ padding: 0, listStyle: "none" }}
            data-testid={`nb-shelf-artifact-${artifact.id}`}
          >
            <button
              type="button"
              className="nb-shelf-item"
              onClick={() => {
                void router.navigate(resolveArtifactDestination(artifact));
              }}
              aria-label={`Open visual artifact: ${artifact.title}`}
            >
              <span className="nb-shelf-t">{artifact.title}</span>
              <span className="nb-shelf-meta">
                {formatTimeOnly(artifact.createdAt)}
                {" · "}
                <span className="rich-artifact-trust">
                  {artifact.trustLevel}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

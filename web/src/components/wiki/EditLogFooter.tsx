// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import { useEffect, useState } from "react";

import {
  fetchHistory,
  subscribeEditLog,
  type WikiEditLogEntry,
} from "../../api/wiki";
import { formatRelativeTime } from "../../lib/format";
import PixelAvatar from "./PixelAvatar";

/** Fixed-bottom live edit-log: streams wiki:write events, newest on the left. */

const MAX_ENTRIES = 20;

interface EditLogFooterProps {
  /** Override stream source — primarily for tests. */
  initialEntries?: WikiEditLogEntry[];
  /** Article path used to hydrate the footer from real git history. */
  historyPath?: string | null;
  onNavigate?: (path: string) => void;
}

export default function EditLogFooter({
  initialEntries,
  historyPath,
  onNavigate,
}: EditLogFooterProps) {
  const [entries, setEntries] = useState<WikiEditLogEntry[]>(() =>
    mergeEditLogEntries([], initialEntries?.slice(0, MAX_ENTRIES) ?? []),
  );

  useEffect(() => {
    let cancelled = false;
    let unsubscribe: (() => void) | null = null;
    let bufferedLiveEntries: WikiEditLogEntry[] = [];
    const seedEntries = initialEntries?.slice(0, MAX_ENTRIES) ?? [];

    setEntries(mergeEditLogEntries([], seedEntries));

    async function start() {
      unsubscribe = subscribeEditLog((entry) => {
        bufferedLiveEntries = [entry, ...bufferedLiveEntries].slice(
          0,
          MAX_ENTRIES,
        );
        setEntries((prev) => mergeEditLogEntries([entry], prev));
      });
      if (cancelled) return;
      if (seedEntries.length > 0) {
        setEntries(mergeEditLogEntries(bufferedLiveEntries, seedEntries));
        bufferedLiveEntries = [];
      } else if (historyPath) {
        const history = await fetchHistory(historyPath);
        if (cancelled) return;
        const historyEntries = history.commits.map((commit) =>
          historyCommitToEditLog(historyPath, commit),
        );
        setEntries(mergeEditLogEntries(bufferedLiveEntries, historyEntries));
        bufferedLiveEntries = [];
      } else {
        setEntries(mergeEditLogEntries([], bufferedLiveEntries));
        bufferedLiveEntries = [];
      }
    }

    void start();
    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, [historyPath, initialEntries]);

  return (
    <div className="wk-edit-log" aria-label="Live wiki edit log">
      <span className="wk-label">Live</span>
      {entries.map((entry, idx) => {
        const isLive = idx === 0;
        return (
          <span
            key={editLogEntryKey(entry)}
            className={isLive ? "wk-entry wk-live" : "wk-entry"}
            data-testid={isLive ? "wk-live-entry" : undefined}
          >
            <PixelAvatar slug={entry.who.toLowerCase()} size={14} />
            <span className="wk-who">{entry.who}</span>{" "}
            <span className="wk-action">{entry.action}</span>{" "}
            <a
              className="wk-what"
              href={`#/wiki/${encodeURI(entry.article_path)}`}
              onClick={(e) => {
                if (onNavigate) {
                  e.preventDefault();
                  onNavigate(entry.article_path);
                }
              }}
            >
              {entry.article_title}
            </a>{" "}
            <span className="wk-when">
              {isLive ? "just now" : safeRelative(entry.timestamp)}
            </span>
          </span>
        );
      })}
    </div>
  );
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso);
  } catch {
    return iso;
  }
}

function mergeEditLogEntries(
  priorityEntries: WikiEditLogEntry[],
  entries: WikiEditLogEntry[],
): WikiEditLogEntry[] {
  const seen = new Set<string>();
  const merged: WikiEditLogEntry[] = [];
  for (const entry of [...priorityEntries, ...entries]) {
    const key = editLogEntryKey(entry);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(entry);
    if (merged.length >= MAX_ENTRIES) break;
  }
  return merged;
}

function editLogEntryKey(entry: WikiEditLogEntry): string {
  if (entry.commit_sha) return `sha:${entry.commit_sha}`;
  return [
    "entry",
    entry.article_path,
    entry.timestamp,
    entry.who,
    entry.action,
  ].join(":");
}

function historyCommitToEditLog(
  path: string,
  commit: { sha: string; author_slug: string; msg: string; date: string },
): WikiEditLogEntry {
  return {
    who: commit.author_slug,
    action: "edited",
    article_path: path,
    article_title: titleFromPath(path),
    timestamp: commit.date,
    commit_sha: commit.sha,
  };
}

function titleFromPath(path: string): string {
  const base = path.split("/").pop() ?? path;
  return base.replace(/\.md$/, "").replace(/-/g, " ");
}

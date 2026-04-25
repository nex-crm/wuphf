import { useEffect, useState } from "react";

import {
  MOCK_EDIT_LOG,
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
  onNavigate?: (path: string) => void;
}

export default function EditLogFooter({
  initialEntries,
  onNavigate,
}: EditLogFooterProps) {
  const [entries, setEntries] = useState<WikiEditLogEntry[]>(
    initialEntries ?? MOCK_EDIT_LOG,
  );

  useEffect(() => {
    const unsubscribe = subscribeEditLog((entry) => {
      setEntries((prev) => [entry, ...prev].slice(0, MAX_ENTRIES));
    });
    return unsubscribe;
  }, []);

  return (
    <div className="wk-edit-log" aria-label="Live wiki edit log">
      <span className="wk-label">Live</span>
      {entries.map((entry, idx) => {
        const isLive = idx === 0;
        return (
          <span
            key={`${entry.commit_sha}-${idx}`}
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

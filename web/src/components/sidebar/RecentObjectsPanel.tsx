/**
 * RecentObjectsPanel — "Recently visited" list in the sidebar.
 *
 * Shows the last N items the user opened (from localStorage via
 * useRecentObjects). Each item renders as a clickable link using the
 * href from resolveObjectRoute so it works even after a full reload.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { readRecentObjects } from "../../hooks/useRecentObjects";

const DISPLAY_COUNT = 8;

export function RecentObjectsPanel() {
  // Read synchronously — the list only changes on navigation, and the
  // sidebar re-renders on every route change anyway.
  const items = readRecentObjects().slice(0, DISPLAY_COUNT);

  if (items.length === 0) {
    return null;
  }

  return (
    <div className="recent-objects">
      <div className="sidebar-section-title recent-objects-title">Recent</div>
      <div className="recent-objects-list">
        {items.map((item) => (
          <a
            key={`${item.ref.kind}:${item.href}`}
            href={item.href}
            className="sidebar-item recent-objects-item"
            title={item.label}
          >
            <RecentObjectIcon kind={item.ref.kind} />
            <span className="recent-objects-label">{humanLabel(item.label)}</span>
          </a>
        ))}
      </div>
    </div>
  );
}

/** Strip the object-kind prefix that resolveObjectRoute adds.
 * E.g. "Wiki: people/nazz" → "people/nazz", "Agent: gaia" → "gaia".
 */
function humanLabel(label: string): string {
  const colonIdx = label.indexOf(": ");
  return colonIdx >= 0 ? label.slice(colonIdx + 2) : label;
}

type Kind =
  | "agent"
  | "run"
  | "task"
  | "wiki-page"
  | "workbench-item"
  | "artifact"
  | "settings-section";

function RecentObjectIcon({ kind }: { kind: Kind }) {
  switch (kind) {
    case "agent":
      return (
        <svg
          aria-hidden="true"
          focusable="false"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="recent-objects-icon"
        >
          <circle cx="12" cy="7" r="4" />
          <path d="M5.5 20a6.5 6.5 0 0 1 13 0" />
        </svg>
      );
    case "task":
    case "workbench-item":
      return (
        <svg
          aria-hidden="true"
          focusable="false"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="recent-objects-icon"
        >
          <path d="M9 11l3 3L22 4" />
          <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11" />
        </svg>
      );
    case "wiki-page":
      return (
        <svg
          aria-hidden="true"
          focusable="false"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="recent-objects-icon"
        >
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
        </svg>
      );
    case "artifact":
    case "run":
      return (
        <svg
          aria-hidden="true"
          focusable="false"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="recent-objects-icon"
        >
          <polygon points="5 3 19 12 5 21 5 3" />
        </svg>
      );
    case "settings-section":
      return (
        <svg
          aria-hidden="true"
          focusable="false"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="recent-objects-icon"
        >
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
        </svg>
      );
  }
}

import type { CustomAppVersion } from "../../api/apps";
import { formatRelativeTime } from "../../lib/format";

interface AppVersionTimelineProps {
  versions: CustomAppVersion[];
  isLoading: boolean;
  /** The history fetch failed — distinct from a genuinely empty history. */
  isError?: boolean;
  /** The version being previewed, or null when viewing the current build. */
  selectedVersion: number | null;
  currentVersion: number;
  onSelect: (version: number) => void;
}

/**
 * AppVersionTimeline is the read rail of an app's append-only history. Each row
 * is one retained build (who/when), newest first. Selecting a row previews that
 * build non-destructively in the frame beside it — restoring is a separate,
 * explicit action on the preview banner. Presentational: the parent owns the
 * data and the preview/restore state.
 */
export function AppVersionTimeline({
  versions,
  isLoading,
  isError,
  selectedVersion,
  currentVersion,
  onSelect,
}: AppVersionTimelineProps) {
  return (
    <aside className="app-version-timeline" aria-label="Version history">
      <div className="app-version-timeline__header">Version history</div>
      {isLoading ? (
        <div className="app-version-timeline__empty">Loading history…</div>
      ) : isError ? (
        // A failed fetch must not read as an empty history.
        <div className="app-version-timeline__empty" role="alert">
          Couldn’t load version history.
        </div>
      ) : versions.length === 0 ? (
        <div className="app-version-timeline__empty">
          No saved versions yet. Each published build is kept here.
        </div>
      ) : (
        <ul className="app-version-timeline__list">
          {versions.map((v) => {
            // The "current build" row is active when nothing older is selected.
            const isActive =
              selectedVersion === v.version ||
              (selectedVersion === null && v.version === currentVersion);
            return (
              <li key={v.version}>
                <button
                  type="button"
                  className={`app-version-timeline__row${
                    isActive ? " app-version-timeline__row--active" : ""
                  }`}
                  aria-current={isActive ? "true" : undefined}
                  onClick={() => onSelect(v.version)}
                >
                  <span className="app-version-timeline__badge">
                    v{v.version}
                  </span>
                  {v.current ? (
                    <span className="app-version-timeline__current">
                      Current
                    </span>
                  ) : null}
                  <span className="app-version-timeline__meta">
                    {[
                      v.updatedBy,
                      v.updatedAt && formatRelativeTime(v.updatedAt),
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </aside>
  );
}

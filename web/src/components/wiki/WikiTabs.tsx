// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import Pam from "./Pam";

export type WikiTab = "wiki";

interface WikiTabsProps {
  current: WikiTab;
  onSelect: (tab: WikiTab) => void;
  /**
   * Pam sits inside the tab bar so her desk can rest on the bottom
   * divider line. `pamArticlePath` is the article she should act on;
   * pass `null` outside an article view (or outside the Wiki tab
   * entirely) and her menu falls back to a "Open an article…" empty
   * state.
   */
  pamArticlePath?: string | null;
  onPamActionDone?: () => void;
}

/**
 * Top tab bar for the unified Wiki app. The canonical team reference
 * lives over a git repo of markdown files; Pam the Archivist rides
 * inside the tab bar.
 *
 * Lives above the per-surface design system so it reads as app chrome,
 * not as a wiki-themed element.
 */
export default function WikiTabs({
  current,
  onSelect,
  pamArticlePath = null,
  onPamActionDone,
}: WikiTabsProps) {
  const tabs: Array<{ id: WikiTab; label: string; badge?: number }> = [
    { id: "wiki", label: "Wiki" },
  ];

  return (
    <nav className="wiki-tabs" aria-label="Wiki surfaces">
      {tabs.map((tab) => {
        const isActive = current === tab.id;
        return (
          <button
            key={tab.id}
            role="tab"
            type="button"
            aria-selected={isActive}
            className={`wiki-tab${isActive ? " is-active" : ""}`}
            onClick={() => onSelect(tab.id)}
          >
            <span className="wiki-tab-label">{tab.label}</span>
            {tab.badge !== undefined && (
              <span className="wiki-tab-badge" title={`${tab.badge} pending`}>
                {tab.badge}
              </span>
            )}
          </button>
        );
      })}
      {/* Pam the Archivist rides inside the tab bar so her desk can sit
          exactly on the bottom divider line — see pam.css for the absolute
          positioning. */}
      <Pam articlePath={pamArticlePath} onActionDone={onPamActionDone} />
    </nav>
  );
}

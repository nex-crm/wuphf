/** Minimal docmost-style view tabs at the top of the article (Read / Edit / History / Raw). */

import { keyedByOccurrence } from "../../lib/reactKeys";

export type HatBarTab = "article" | "edit" | "history" | "raw";

interface HatBarProps {
  active: HatBarTab;
  onChange?: (tab: HatBarTab) => void;
  rightRail?: string[];
  disabledTabs?: HatBarTab[];
}

const LABELS: Record<HatBarTab, string> = {
  article: "Read",
  edit: "Edit",
  history: "History",
  raw: "Raw markdown",
};

const ORDER: HatBarTab[] = ["article", "edit", "history", "raw"];

export default function HatBar({
  active,
  onChange,
  rightRail,
  disabledTabs = [],
}: HatBarProps) {
  return (
    <nav className="wk-hatbar" aria-label="Article views">
      {ORDER.map((tab) => {
        const isActive = tab === active;
        const disabled = disabledTabs.includes(tab);
        const className = `wk-tab${isActive ? " active" : ""}`;
        return (
          <button
            key={tab}
            type="button"
            className={className}
            // The active tab is indicated by more than color: aria-current
            // exposes "this is the current view" to assistive tech.
            aria-current={isActive ? "page" : undefined}
            disabled={disabled}
            onClick={() => !disabled && onChange?.(tab)}
          >
            {LABELS[tab]}
          </button>
        );
      })}
      {rightRail && rightRail.length > 0 && (
        <span className="wk-rail-right">
          {keyedByOccurrence(rightRail, (item) => item).map(
            ({ key, value: item, index: i }) => (
              <span key={key}>
                {i > 0 && <span>•</span>} {item}
              </span>
            ),
          )}
        </span>
      )}
    </nav>
  );
}

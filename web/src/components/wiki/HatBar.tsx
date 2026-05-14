/** Wikipedia-style tabs at the top of the article (Article / Talk / Edit / History / Raw). */

import { keyedByOccurrence } from "../../lib/reactKeys";

export type HatBarTab =
  | "article"
  | "visual"
  | "talk"
  | "edit"
  | "history"
  | "raw";

interface HatBarProps {
  active: HatBarTab;
  onChange?: (tab: HatBarTab) => void;
  rightRail?: string[];
  disabledTabs?: HatBarTab[];
}

const LABELS: Record<HatBarTab, string> = {
  article: "Article",
  visual: "Visual",
  talk: "Talk",
  edit: "Edit source",
  history: "History",
  raw: "Raw markdown",
};

const ORDER: HatBarTab[] = [
  "article",
  "visual",
  "talk",
  "edit",
  "history",
  "raw",
];

export default function HatBar({
  active,
  onChange,
  rightRail,
  disabledTabs = ["talk"],
}: HatBarProps) {
  return (
    <nav className="wk-hatbar" aria-label="Article views">
      {ORDER.map((tab) => {
        const disabled = disabledTabs.includes(tab);
        const className = `wk-tab${tab === active ? " active" : ""}`;
        return (
          <button
            key={tab}
            type="button"
            className={className}
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

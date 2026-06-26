import { useEffect, useState } from "react";

import { scrollToInPageAnchor } from "../../lib/wikiMarkdownConfig";
import type { TocEntry } from "./TocBox";

/**
 * Vector-2022-style left "Contents" panel: sticky beside the article,
 * generated from the article's H2/H3s, collapsible, with the section
 * currently in view highlighted (scroll-spy via IntersectionObserver).
 *
 * Clicks scroll within the article column instead of following the `#…`
 * fragment, because the app's hash router owns `location.hash`.
 */

interface ArticleContentsProps {
  entries: TocEntry[];
}

/**
 * Observe the article headings and report the anchor of the section the
 * reader is currently in. Biased toward the upper quarter of the viewport
 * (Wikipedia's behavior: a section counts as active while its heading is
 * at or above the reading line).
 */
function useActiveSection(entries: TocEntry[]): string | null {
  const [active, setActive] = useState<string | null>(null);

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    if (entries.length === 0) return;
    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (records) => {
        for (const record of records) {
          const { id } = record.target;
          if (record.isIntersecting) visible.add(id);
          else visible.delete(id);
        }
        // The first (topmost) visible heading in document order wins.
        for (const entry of entries) {
          if (visible.has(entry.anchor)) {
            setActive(entry.anchor);
            return;
          }
        }
      },
      { rootMargin: "0px 0px -60% 0px" },
    );
    const observed: Element[] = [];
    for (const entry of entries) {
      const el = document.getElementById(entry.anchor);
      if (el) {
        observer.observe(el);
        observed.push(el);
      }
    }
    return () => {
      for (const el of observed) observer.unobserve(el);
      observer.disconnect();
    };
  }, [entries]);

  return active;
}

export default function ArticleContents({ entries }: ArticleContentsProps) {
  const [collapsed, setCollapsed] = useState(false);
  const active = useActiveSection(entries);

  if (entries.length === 0) return null;
  return (
    <nav
      className="wk-contents"
      data-testid="wk-contents"
      aria-label="Contents"
    >
      <div className="wk-contents-header">
        <span className="wk-contents-title">Contents</span>
        <button
          type="button"
          className="wk-hide-link"
          onClick={() => setCollapsed((v) => !v)}
          aria-expanded={!collapsed}
        >
          [{collapsed ? "show" : "hide"}]
        </button>
      </div>
      {!collapsed ? (
        <ul className="wk-contents-list">
          {entries.map((entry) => (
            <li
              key={`${entry.anchor}-${entry.num}`}
              className={`wk-contents-item wk-contents-lvl-${entry.level}${
                active === entry.anchor ? " is-active" : ""
              }`}
            >
              <a
                href={`#${entry.anchor}`}
                aria-current={active === entry.anchor ? "true" : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  scrollToInPageAnchor(`#${entry.anchor}`);
                }}
              >
                <span className="wk-num">{entry.num}</span>
                {entry.title}
              </a>
            </li>
          ))}
        </ul>
      ) : null}
    </nav>
  );
}

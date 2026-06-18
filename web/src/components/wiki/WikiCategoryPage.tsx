import { useMemo } from "react";

import type { WikiCatalogEntry } from "../../api/wiki";
import { pluralize } from "../../lib/format";
import { categoryPath } from "./wikiPaths";

/**
 * Auto-generated Wikipedia-style category index page: every article filed in
 * this category, listed alphabetically and grouped by first letter. Membership
 * is the article's many-to-many `categories:` frontmatter (catalog
 * `categories`), with the folder `group` kept as a fallback so links stay
 * populated during the migration to real categories. Linked from each
 * article's category line and the home Browse panel — categories replace
 * folders as the wiki's organizing surface.
 */

interface WikiCategoryPageProps {
  /** Category slug, e.g. "people", "companies", "playbooks". */
  slug: string;
  catalog: WikiCatalogEntry[];
  onNavigate: (path: string) => void;
}

/** Human label for a category slug: "people" → "People". */
export function categoryLabel(slug: string): string {
  if (!slug) return slug;
  return slug
    .split(/[-_]/)
    .map((part) =>
      part.length > 0 ? part[0].toUpperCase() + part.slice(1) : part,
    )
    .join(" ");
}

function firstLetter(title: string): string {
  const ch = title.trim().charAt(0).toUpperCase();
  return /[A-Z]/.test(ch) ? ch : "#";
}

export default function WikiCategoryPage({
  slug,
  catalog,
  onNavigate,
}: WikiCategoryPageProps) {
  const normalized = slug.toLowerCase();
  const { letters, count, siblings } = useMemo(() => {
    // An article is in this category when its `categories:` frontmatter names
    // the slug, OR (fallback) its folder group matches — so links stay
    // populated until articles carry explicit categories.
    const inCategory = (entry: WikiCatalogEntry): boolean =>
      (entry.categories ?? []).some((c) => c.toLowerCase() === normalized) ||
      entry.group.toLowerCase() === normalized;
    const members = catalog
      .filter(inCategory)
      .sort((a, b) => a.title.localeCompare(b.title));
    const byLetter = new Map<string, WikiCatalogEntry[]>();
    for (const entry of members) {
      const letter = firstLetter(entry.title);
      const bucket = byLetter.get(letter);
      if (bucket) bucket.push(entry);
      else byLetter.set(letter, [entry]);
    }
    // Sibling categories: the union of every article's real categories and its
    // folder group, minus this one.
    const allCategories = new Set<string>();
    for (const entry of catalog) {
      for (const c of entry.categories ?? []) allCategories.add(c);
      if (entry.group) allCategories.add(entry.group);
    }
    const otherCategories = [...allCategories].filter(
      (c) => c.toLowerCase() !== normalized,
    );
    otherCategories.sort((a, b) => a.localeCompare(b));
    return {
      letters: [...byLetter.entries()].sort(([a], [b]) => a.localeCompare(b)),
      count: members.length,
      siblings: otherCategories,
    };
  }, [catalog, normalized]);

  const label = categoryLabel(slug);

  return (
    <main className="wk-category-page" data-testid="wk-category-page">
      <div className="wk-category-inner">
        <div className="wk-breadcrumb">
          <a
            href="#/wiki"
            onClick={(e) => {
              e.preventDefault();
              onNavigate("");
            }}
          >
            Team Wiki
          </a>
          <span className="sep">›</span>
          <span>Category</span>
        </div>
        <h1 className="wk-article-title">Category: {label}</h1>
        <hr className="wk-title-rule" />
        <p className="wk-category-summary">
          The following {count} {pluralize(count, "page")}{" "}
          {count === 1 ? "is" : "are"} in this category.
        </p>
        {count === 0 ? (
          <p className="wk-home-empty">No pages yet.</p>
        ) : (
          letters.map(([letter, entries]) => (
            <section
              key={letter}
              className="wk-category-letter"
              aria-label={`Pages starting with ${letter}`}
            >
              <h2>{letter}</h2>
              <ul>
                {entries.map((entry) => (
                  <li key={entry.path}>
                    <a
                      href={`#/wiki/${encodeURI(entry.path)}`}
                      onClick={(e) => {
                        e.preventDefault();
                        onNavigate(entry.path);
                      }}
                    >
                      {entry.title}
                    </a>
                  </li>
                ))}
              </ul>
            </section>
          ))
        )}
        {siblings.length > 0 ? (
          <div className="wk-categories" aria-label="Other categories">
            <span className="wk-label">Other categories:</span>
            {siblings.map((g) => (
              <a
                key={g}
                href={`#/wiki/${categoryPath(g)}`}
                onClick={(e) => {
                  e.preventDefault();
                  onNavigate(categoryPath(g));
                }}
              >
                {categoryLabel(g)}
              </a>
            ))}
          </div>
        ) : null}
      </div>
    </main>
  );
}

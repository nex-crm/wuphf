// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
/** Wikipedia-style category line above the page footer. */

import { categoryLabel } from "./WikiCategoryPage";
import { categoryPath } from "./wikiPaths";

interface CategoriesFooterProps {
  /** Category slugs (path kinds): "companies", "people", "playbooks", … */
  tags: string[];
  onSelect?: (tag: string) => void;
}

export default function CategoriesFooter({
  tags,
  onSelect,
}: CategoriesFooterProps) {
  if (tags.length === 0) return null;
  return (
    <div className="wk-categories" aria-label="Categories">
      <span className="wk-label">Categories:</span>
      {tags.map((tag) => (
        <a
          key={tag}
          href={`#/wiki/${categoryPath(tag)}`}
          onClick={(e) => {
            if (onSelect) {
              e.preventDefault();
              onSelect(tag);
            }
          }}
        >
          {categoryLabel(tag)}
        </a>
      ))}
    </div>
  );
}

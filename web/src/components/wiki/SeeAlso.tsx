import WikiLink from "./WikiLink";

/** "See also" italic list at the bottom of an article. */

export interface SeeAlsoItem {
  slug: string;
  display: string;
  broken?: boolean;
}

interface SeeAlsoProps {
  items: SeeAlsoItem[];
  onNavigate?: (slug: string) => void;
}

export default function SeeAlso({ items, onNavigate }: SeeAlsoProps) {
  if (items.length === 0) return null;
  return (
    <section className="wk-see-also" aria-labelledby="wk-see-also-heading">
      <h2 id="wk-see-also-heading">See also</h2>
      <ul>
        {items.map((item) => (
          <li key={item.slug}>
            <WikiLink
              slug={item.slug}
              display={item.display}
              broken={item.broken}
              onNavigate={onNavigate}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}

import { useMemo } from "react";
import ReactMarkdown from "react-markdown";

import {
  buildMarkdownComponents,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";
import type { InfoboxRow } from "./articleContent";

/**
 * Wikipedia-style infobox: a right-floating bordered table with a header
 * band carrying the article title and label/value rows lifted from B2's
 * `## Summary` definition list. Values are markdown — `[[kind/slug]]`
 * wikilinks and `[label](url)` links render through the shared wiki
 * markdown pipeline so blue/red link state and in-app navigation match
 * the article body.
 */

interface ArticleInfoboxProps {
  /** Header-band text — the article title. */
  title: string;
  rows: InfoboxRow[];
  /** Wikilink existence resolver (blue vs redlink). */
  resolver: (slug: string) => boolean;
  onNavigate?: (slug: string) => void;
  /** Canonical article path — resolves relative markdown links in values. */
  articlePath?: string;
}

export default function ArticleInfobox({
  title,
  rows,
  resolver,
  onNavigate,
  articlePath,
}: ArticleInfoboxProps) {
  const remarkPlugins = useMemo(() => buildRemarkPlugins(resolver), [resolver]);
  const components = useMemo(() => {
    const base = buildMarkdownComponents({ resolver, onNavigate, articlePath });
    return {
      ...base,
      // Values render inline inside <dd> — unwrap the paragraph remark
      // emits around each value so the definition grid stays one row.
      p: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
    };
  }, [resolver, onNavigate, articlePath]);

  if (rows.length === 0) return null;
  return (
    <aside className="wk-infobox" data-testid="wk-infobox" aria-label={title}>
      <div className="wk-ib-title">{title}</div>
      <div className="wk-ib-body">
        <dl>
          {rows.map((row) => (
            <div className="wk-ib-row" key={row.term}>
              <dt>{row.term}</dt>
              <dd>
                <ReactMarkdown
                  remarkPlugins={remarkPlugins}
                  components={components}
                >
                  {row.value}
                </ReactMarkdown>
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </aside>
  );
}

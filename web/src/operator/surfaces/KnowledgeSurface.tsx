// Knowledge — a Wikipedia-style reader over the company brain (gbrain). Every
// claim that came from somewhere carries a citation. Hovering (or focusing) a
// citation shows the source: what kind it is, where it came from, and the exact
// snippet the fact was drawn from. An "Explain" button reveals why the brain
// chose that source for this fact (e.g. why a specific chat backs an insight).
// Mock data only.

import { type ReactNode, useMemo, useState } from "react";
import { Sparkles } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import {
  type KnowledgeRef,
  type KnowledgeSourceKind,
  KNOWLEDGE,
} from "../mock/data";

const KIND_LABEL: Record<KnowledgeSourceKind, string> = {
  chat: "Chat",
  document: "Document",
  crm: "CRM",
  decision: "Decision",
  roster: "Roster",
};

// A single [n] citation with a hover/focus popover over its source.
function Citation({ n, source }: { n: number; source?: KnowledgeRef }) {
  const [explained, setExplained] = useState(false);

  if (!source) {
    return (
      <sup className="opr-cite">
        <a href={`#ref-${n}`}>[{n}]</a>
      </sup>
    );
  }

  return (
    <sup className="opr-cite opr-cite-has-pop">
      <a href={`#ref-${n}`} aria-label={`Source ${n}: ${source.title}`}>
        [{n}]
      </a>
      <span className="opr-cite-pop" role="note">
        <span className="opr-cite-pop-head">
          <span className="opr-cite-pop-kind">{KIND_LABEL[source.kind]}</span>
          <span className="opr-cite-pop-title">{source.title}</span>
        </span>
        <span className="opr-cite-pop-detail">{source.detail}</span>
        <span className="opr-cite-pop-snippet">{source.snippet}</span>
        {explained ? (
          <span className="opr-cite-pop-why">
            <span className="opr-cite-pop-why-label">
              <Sparkles size={11} strokeWidth={2} aria-hidden /> Why this source
            </span>
            {source.why}
          </span>
        ) : (
          <button
            type="button"
            className="opr-btn opr-btn-sm opr-cite-pop-explain"
            onClick={() => setExplained(true)}
          >
            <Sparkles size={12} strokeWidth={2} aria-hidden />
            Explain
          </button>
        )}
      </span>
    </sup>
  );
}

// Turn "...routes it.[[1]]" prose into text + citation popovers.
function renderProse(
  text: string,
  refByN: Map<number, KnowledgeRef>,
): ReactNode[] {
  return text.split(/(\[\[\d+\]\])/g).map((part, i) => {
    const m = part.match(/^\[\[(\d+)\]\]$/);
    if (m) {
      const n = Number(m[1]);
      return <Citation key={i} n={n} source={refByN.get(n)} />;
    }
    return <span key={i}>{part}</span>;
  });
}

export function KnowledgeSurface() {
  const [activeId, setActiveId] = useState(KNOWLEDGE[0]?.id ?? "");
  const page = KNOWLEDGE.find((k) => k.id === activeId) ?? KNOWLEDGE[0];
  const titleOf = (id: string) =>
    KNOWLEDGE.find((k) => k.id === id)?.title ?? id;

  const refByN = useMemo(
    () => new Map((page?.references ?? []).map((r) => [r.n, r])),
    [page],
  );

  return (
    <div className="opr-surface-wide">
      <div
        className="opr-surface-head"
        style={{ marginBottom: "var(--space-4)" }}
      >
        <div>
          <Eyebrow>Knowledge</Eyebrow>
          <h1 className="opr-surface-title">What your AI knows</h1>
          <p className="opr-surface-lede">
            The shared brain behind every tool. Each fact is cited back to where
            it came from, so you can trust what the tools act on. Hover a citation
            to see the source, and ask why it was chosen.
          </p>
        </div>
        <button type="button" className="opr-btn opr-btn-sm">
          New page
        </button>
      </div>

      <div className="opr-wiki">
        <nav className="opr-kn-list" aria-label="Knowledge pages">
          <div className="opr-eyebrow" style={{ marginBottom: "var(--space-2)" }}>
            Pages
          </div>
          {KNOWLEDGE.map((k) => (
            <button
              key={k.id}
              type="button"
              className={`opr-kn-item${k.id === activeId ? " is-active" : ""}`}
              onClick={() => setActiveId(k.id)}
            >
              {k.title}
            </button>
          ))}
        </nav>

        {page ? (
          <article className="opr-article">
            <aside className="opr-article-infobox">
              <div className="opr-infobox-title">{page.title}</div>
              <dl>
                {page.infobox.map((row) => (
                  <div className="opr-infobox-row" key={row.label}>
                    <dt>{row.label}</dt>
                    <dd>{row.value}</dd>
                  </div>
                ))}
              </dl>
            </aside>

            <h1>{page.title}</h1>
            <div className="opr-article-meta">
              From the company brain · {page.updatedAt}
            </div>

            <p className="opr-article-lead">{renderProse(page.lead, refByN)}</p>

            {page.sections.map((section) => (
              <section key={section.heading ?? "body"}>
                {section.heading ? <h2>{section.heading}</h2> : null}
                {section.paras.map((para, i) => (
                  <p key={i}>{renderProse(para, refByN)}</p>
                ))}
              </section>
            ))}

            <h2>References</h2>
            <ol className="opr-refs">
              {page.references.map((ref) => (
                <li id={`ref-${ref.n}`} key={ref.n}>
                  <span className="opr-ref-kind">{KIND_LABEL[ref.kind]}</span>
                  <span className="opr-ref-source">{ref.title}</span>
                  <span className="opr-ref-detail"> · {ref.detail}</span>
                </li>
              ))}
            </ol>

            {page.seeAlso.length > 0 ? (
              <>
                <h2>See also</h2>
                <ul className="opr-seealso">
                  {page.seeAlso.map((id) => (
                    <li key={id}>
                      <button
                        type="button"
                        className="opr-wikilink"
                        onClick={() => setActiveId(id)}
                      >
                        {titleOf(id)}
                      </button>
                    </li>
                  ))}
                </ul>
              </>
            ) : null}

            <div className="opr-categories">
              <span className="opr-cat-label">Categories</span>
              {page.categories.map((c) => (
                <span className="opr-cat" key={c}>
                  {c}
                </span>
              ))}
            </div>
          </article>
        ) : null}
      </div>
    </div>
  );
}

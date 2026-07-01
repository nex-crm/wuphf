// Knowledge — a Wikipedia-style reader over the company brain (gbrain). Every
// claim that came from somewhere carries a citation. Hovering (or focusing) a
// citation shows the source: what kind it is, where it came from, and the exact
// snippet the fact was drawn from. An "Explain" button reveals why the brain
// chose that source for this fact (e.g. why a specific chat backs an insight).
//
// With an `appId` this reads the app's REAL synthesized pages from the broker
// (grounded in the app's own artifacts, cached). Without one it renders the mock
// pages — the shape is identical, so the render code is shared verbatim.

import { type ReactNode, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";

import { getAppKnowledge } from "../apps/knowledgeClient";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow } from "../components/primitives";
import {
  KNOWLEDGE,
  type KnowledgePage,
  type KnowledgeRef,
  type KnowledgeSourceKind,
} from "../mock/data";

const KIND_LABEL: Record<KnowledgeSourceKind, string> = {
  chat: "Chat",
  document: "Document",
  crm: "CRM",
  decision: "Decision",
  roster: "Roster",
};

// Jump to a reference list item without mutating the URL hash. Writing
// `#ref-n` into window.location.hash would replace the /#/operator route and
// unmount the hash-routed shell, so we scroll to the target imperatively.
function jumpToRef(n: number) {
  document.getElementById(`ref-${n}`)?.scrollIntoView({ block: "start" });
}

// A single [n] citation with a hover/focus popover over its source.
function Citation({ n, source }: { n: number; source?: KnowledgeRef }) {
  const [explained, setExplained] = useState(false);

  if (!source) {
    return (
      <sup className="opr-cite">
        <button
          type="button"
          onClick={() => jumpToRef(n)}
          aria-label={`Jump to reference ${n}`}
        >
          [{n}]
        </button>
      </sup>
    );
  }

  return (
    <sup className="opr-cite opr-cite-has-pop">
      <button
        type="button"
        onClick={() => jumpToRef(n)}
        aria-label={`Source ${n}: ${source.title}`}
      >
        [{n}]
      </button>
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
              <Sparkles size={11} strokeWidth={2} aria-hidden={true} /> Why this
              source
            </span>
            {source.why}
          </span>
        ) : (
          <button
            type="button"
            className="opr-btn opr-btn-sm opr-cite-pop-explain"
            onClick={() => setExplained(true)}
          >
            <Sparkles size={12} strokeWidth={2} aria-hidden={true} />
            Explain
          </button>
        )}
      </span>
    </sup>
  );
}

// A reference at the bottom of a page. Clickable: opening it reveals the source
// itself — the exact excerpt the fact was drawn from, and why the brain chose
// it. So every reference is reachable, not just a label.
function ReferenceItem({ source }: { source: KnowledgeRef }) {
  const [open, setOpen] = useState(false);
  return (
    <li id={`ref-${source.n}`} className="opr-ref-item">
      <button
        type="button"
        className="opr-ref-row"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span className="opr-ref-kind">{KIND_LABEL[source.kind]}</span>
        <span className="opr-ref-source">{source.title}</span>
        <span className="opr-ref-detail"> · {source.detail}</span>
      </button>
      {open ? (
        <div className="opr-ref-expand">
          <p className="opr-ref-snippet">{source.snippet}</p>
          <p className="opr-ref-why">
            <span className="opr-ref-why-label">
              <Sparkles size={11} strokeWidth={2} aria-hidden={true} /> Why this
              source
            </span>
            {source.why}
          </p>
        </div>
      ) : null}
    </li>
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

interface KnowledgeSurfaceProps {
  /**
   * When set, read the app's REAL synthesized knowledge from the broker. When
   * absent, render the mock pages (same shape, same render).
   */
  appId?: string;
}

export function KnowledgeSurface({ appId }: KnowledgeSurfaceProps) {
  const query = useQuery({
    queryKey: ["operator-app-knowledge", appId],
    queryFn: () => getAppKnowledge(appId ?? ""),
    enabled: Boolean(appId),
    staleTime: 5 * 60_000,
  });

  const pages: KnowledgePage[] = appId ? (query.data?.pages ?? []) : KNOWLEDGE;
  const [activeId, setActiveId] = useState("");
  const page = pages.find((k) => k.id === activeId) ?? pages[0];
  const titleOf = (id: string) => pages.find((k) => k.id === id)?.title ?? id;

  const refByN = useMemo(
    () => new Map((page?.references ?? []).map((r) => [r.n, r])),
    [page],
  );

  // Real synthesis takes a few seconds on first open (cached after).
  const synthesizing = Boolean(appId) && query.isLoading;
  const emptyBrain = Boolean(appId) && !query.isLoading && pages.length === 0;

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
            it came from, so you can trust what the tools act on. Hover a
            citation to see the source, and ask why it was chosen.
          </p>
        </div>
        {appId ? null : (
          <button type="button" className="opr-btn opr-btn-sm">
            New page
          </button>
        )}
      </div>

      {synthesizing ? (
        <div className="opr-app-building" role="status">
          <span className="opr-work-dots" aria-hidden={true}>
            <span />
            <span />
            <span />
          </span>
          <div className="opr-empty-title">Reading what your AI knows…</div>
          <div className="opr-empty-hint">
            Synthesizing cited pages from this app's real sources.
          </div>
        </div>
      ) : emptyBrain ? (
        <EmptyState
          glyph="📖"
          title="No knowledge yet"
          hint="Your AI has not written any cited pages about this app yet. As it learns from the app and your workspace, they appear here."
        />
      ) : (
        <div className="opr-wiki">
          <nav className="opr-kn-list" aria-label="Knowledge pages">
            <div
              className="opr-eyebrow"
              style={{ marginBottom: "var(--space-2)" }}
            >
              Pages
            </div>
            {pages.map((k) => (
              <button
                key={k.id}
                type="button"
                className={`opr-kn-item${k.id === (page?.id ?? "") ? " is-active" : ""}`}
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
              {page.alsoIn && page.alsoIn.length > 0 ? (
                <div className="opr-article-alsoin">
                  Also used by{" "}
                  {page.alsoIn.map((a, i) => (
                    <span key={a.appId}>
                      {i > 0 ? ", " : ""}
                      <span className="opr-alsoin-app">{a.name}</span>
                    </span>
                  ))}
                </div>
              ) : null}

              <p className="opr-article-lead">
                {renderProse(page.lead, refByN)}
              </p>

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
                  <ReferenceItem key={ref.n} source={ref} />
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
            </article>
          ) : null}
        </div>
      )}
    </div>
  );
}

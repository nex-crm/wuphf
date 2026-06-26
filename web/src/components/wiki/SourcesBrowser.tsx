import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import {
  isSourceKind,
  listSources,
  readSource,
  SOURCE_KIND_LABELS,
  SOURCE_KINDS,
  type SourceKind,
  type SourceMetadata,
  type SourceRecord,
} from "../../api/sources";
import CompileButton from "./CompileButton";
import type { SourcesSelection } from "./wikiPaths";
import "../../styles/wiki-reader.css";

/**
 * Sources browser — the immutable source layer the Karpathy-style wiki is
 * compiled FROM. The list groups every captured record by kind (task,
 * decision, chat, doc, url, note); selecting a row opens the full record
 * (GET /sources/read) rendered as markdown. A Compile action lives in the
 * header so the operator can turn sources into cited articles in one place.
 */

interface SourcesBrowserProps {
  /** Selected record (deep-linked via `_sources/<kind>/<id>`), or null = list. */
  selection?: SourcesSelection | null;
  /** Open a record (drives the URL via the wiki shell). */
  onSelect: (kind: string, id: string) => void;
  /** Return to the grouped list. */
  onBack: () => void;
  /** Injectable list fetcher for tests/Storybook. */
  listSourcesFn?: () => Promise<SourceMetadata[]>;
  /** Injectable record fetcher for tests/Storybook. */
  readSourceFn?: (kind: SourceKind, id: string) => Promise<SourceRecord>;
}

type ListState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "loaded"; sources: SourceMetadata[] };

export default function SourcesBrowser({
  selection = null,
  onSelect,
  onBack,
  listSourcesFn = listSources,
  readSourceFn = readSource,
}: SourcesBrowserProps) {
  const [state, setState] = useState<ListState>({ status: "loading" });
  const [reloadNonce, setReloadNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    // `reloadNonce` is a retrigger handle (retry / post-compile refetch); it is
    // not read in the body, so reference it explicitly to keep it in the deps.
    void reloadNonce;
    setState({ status: "loading" });
    listSourcesFn()
      .then((sources) => {
        if (!cancelled) setState({ status: "loaded", sources });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Could not load sources.",
        });
      });
    return () => {
      cancelled = true;
    };
  }, [listSourcesFn, reloadNonce]);

  if (selection) {
    return (
      <SourceDetail
        selection={selection}
        onBack={onBack}
        readSourceFn={readSourceFn}
      />
    );
  }

  return (
    <main className="wiki-main wk-sources" data-testid="wk-sources">
      <header className="wk-sources-head">
        <div>
          <h1 className="wk-sources-title">Sources</h1>
          <p className="wk-sources-sub">
            The raw material the wiki is compiled from.
          </p>
        </div>
        <CompileButton onCompiled={() => setReloadNonce((n) => n + 1)} />
      </header>
      <SourcesListBody
        state={state}
        onSelect={onSelect}
        onRetry={() => setReloadNonce((n) => n + 1)}
      />
    </main>
  );
}

function SourcesListBody({
  state,
  onSelect,
  onRetry,
}: {
  state: ListState;
  onSelect: (kind: string, id: string) => void;
  onRetry: () => void;
}) {
  if (state.status === "loading") {
    return (
      <p className="wk-sources-status" aria-busy="true">
        Loading sources…
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <div className="wk-sources-status wk-sources-status--error">
        <p role="alert">Broker not responding — {state.message}</p>
        <button type="button" className="wk-shell-retry" onClick={onRetry}>
          Retry
        </button>
      </div>
    );
  }
  if (state.sources.length === 0) {
    return (
      <div className="wk-sources-empty" data-testid="wk-sources-empty">
        <p className="wk-sources-empty-title">No sources captured yet</p>
        <p className="wk-sources-empty-sub">
          Tasks, decisions, and chats are captured as the team works; documents,
          URLs, and notes can be ingested explicitly. Once there are sources,
          Compile turns them into cited wiki articles.
        </p>
      </div>
    );
  }
  return <GroupedSources sources={state.sources} onSelect={onSelect} />;
}

function GroupedSources({
  sources,
  onSelect,
}: {
  sources: SourceMetadata[];
  onSelect: (kind: string, id: string) => void;
}) {
  const groups = useMemo(() => {
    const byKind = new Map<SourceKind, SourceMetadata[]>();
    for (const source of sources) {
      const list = byKind.get(source.kind) ?? [];
      list.push(source);
      byKind.set(source.kind, list);
    }
    return SOURCE_KINDS.map((kind) => ({
      kind,
      items: byKind.get(kind) ?? [],
    })).filter((group) => group.items.length > 0);
  }, [sources]);

  return (
    <div className="wk-sources-groups">
      {groups.map((group) => (
        <section key={group.kind} className="wk-sources-group">
          <h2 className="wk-sources-group-title">
            {SOURCE_KIND_LABELS[group.kind]}
            <span className="wk-sources-group-count">{group.items.length}</span>
          </h2>
          <ul className="wk-sources-list">
            {group.items.map((source) => (
              <li key={`${source.kind}-${source.id}`}>
                <button
                  type="button"
                  className="wk-sources-row"
                  onClick={() => onSelect(source.kind, source.id)}
                >
                  <span className="wk-sources-row-title">
                    {source.title || source.id}
                  </span>
                  <span className="wk-sources-row-meta">
                    <span className="wk-sources-row-kind">
                      {SOURCE_KIND_LABELS[source.kind]}
                    </span>
                    {source.origin ? (
                      <span className="wk-sources-row-origin">
                        {source.origin}
                      </span>
                    ) : null}
                    <span className="wk-sources-row-date">
                      {formatCaptured(source.captured_at)}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

type DetailState =
  | { status: "loading" }
  | { status: "notfound" }
  | { status: "error"; message: string }
  | { status: "loaded"; record: SourceRecord };

function SourceDetail({
  selection,
  onBack,
  readSourceFn,
}: {
  selection: SourcesSelection;
  onBack: () => void;
  readSourceFn: (kind: SourceKind, id: string) => Promise<SourceRecord>;
}) {
  const [state, setState] = useState<DetailState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    if (!isSourceKind(selection.kind)) {
      setState({ status: "notfound" });
      return;
    }
    readSourceFn(selection.kind, selection.id)
      .then((record) => {
        if (!cancelled) setState({ status: "loaded", record });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        const { status } = err as { status?: number };
        if (status === 404) {
          setState({ status: "notfound" });
          return;
        }
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Could not load this source.",
        });
      });
    return () => {
      cancelled = true;
    };
  }, [selection.kind, selection.id, readSourceFn]);

  return (
    <main className="wiki-main wk-sources wk-sources--detail">
      <button type="button" className="wk-sources-back" onClick={onBack}>
        ← All sources
      </button>
      <SourceDetailBody state={state} selection={selection} />
    </main>
  );
}

function SourceDetailBody({
  state,
  selection,
}: {
  state: DetailState;
  selection: SourcesSelection;
}) {
  if (state.status === "loading") {
    return (
      <p className="wk-sources-status" aria-busy="true">
        Loading source…
      </p>
    );
  }
  if (state.status === "notfound") {
    return (
      <p className="wk-sources-status wk-sources-status--error" role="alert">
        Source not found: {selection.id}
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <p className="wk-sources-status wk-sources-status--error" role="alert">
        {state.message}
      </p>
    );
  }
  const { record } = state;
  return (
    <article className="wk-source-record">
      <header className="wk-source-record-head">
        <span className="wk-sources-row-kind">
          {SOURCE_KIND_LABELS[record.kind]}
        </span>
        <h1 className="wk-source-record-title">{record.title || record.id}</h1>
        <dl className="wk-source-record-meta">
          {record.origin ? (
            <div>
              <dt>Origin</dt>
              <dd>{record.origin}</dd>
            </div>
          ) : null}
          <div>
            <dt>Captured</dt>
            <dd>{formatCaptured(record.captured_at)}</dd>
          </div>
          <div>
            <dt>ID</dt>
            <dd className="wk-source-record-id">{record.id}</dd>
          </div>
        </dl>
      </header>
      <div className="wk-source-record-body wiki-reader">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>
          {record.content}
        </ReactMarkdown>
      </div>
    </article>
  );
}

/** Compact, locale-aware capture timestamp; falls back to the raw string. */
function formatCaptured(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

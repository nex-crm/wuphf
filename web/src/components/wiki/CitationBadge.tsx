import { createContext, useCallback, useContext, useId, useState } from "react";

import { ApiError } from "../../api/client";
import {
  kindFromSourceId,
  readSource,
  SOURCE_KIND_LABELS,
  type SourceKind,
  type SourceRecord,
} from "../../api/sources";

/**
 * Inline citation badge for a compiled-article `^[source-id]` marker.
 *
 * Renders a small superscript pill (`[n]`, Wikipedia-style) numbered by the
 * surrounding {@link CitationNumberContext}. On hover/focus/click it lazily
 * fetches the cited source (GET /sources/read, kind derived from the id
 * prefix) and shows a popover with the source's title, kind, and origin plus
 * a "View source" affordance. A missing source degrades to "source not found"
 * rather than blanking.
 */

/**
 * Maps each cited source id to its 1-based citation number (first-appearance
 * order, repeated ids share a number). Provided by the read view; defaults to
 * an empty registry so a standalone badge still renders a generic marker.
 */
export const CitationNumberContext = createContext<ReadonlyMap<string, number>>(
  new Map(),
);

type FetchState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "loaded"; record: SourceRecord }
  | { status: "notfound" }
  | { status: "error"; message: string };

interface CitationBadgeProps {
  /** The cited source id, e.g. "task-wup-12". */
  sourceId: string;
  /** Navigate to / open the Sources view for this record. */
  onViewSource?: (kind: SourceKind, id: string) => void;
  /** Injectable fetcher for tests/Storybook; defaults to the real client. */
  fetchSource?: (kind: SourceKind, id: string) => Promise<SourceRecord>;
}

export default function CitationBadge({
  sourceId,
  onViewSource,
  fetchSource = readSource,
}: CitationBadgeProps) {
  const numbers = useContext(CitationNumberContext);
  const kind = kindFromSourceId(sourceId);
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<FetchState>({ status: "idle" });
  const popoverId = useId();

  const number = numbers.get(sourceId);
  const label = number !== undefined ? `[${number}]` : "[cite]";

  const load = useCallback(() => {
    if (kind === null) {
      setState({ status: "notfound" });
      return;
    }
    setState({ status: "loading" });
    fetchSource(kind, sourceId)
      .then((record) => setState({ status: "loaded", record }))
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 404) {
          setState({ status: "notfound" });
          return;
        }
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Could not load this source.",
        });
      });
  }, [fetchSource, kind, sourceId]);

  const reveal = useCallback(() => {
    setOpen(true);
    // Fetch lazily on first reveal (or to retry a prior error). `idle`/`error`
    // are the only states that warrant a (re)fetch; loaded/loading are kept.
    if (state.status === "idle" || state.status === "error") {
      load();
    }
  }, [state.status, load]);

  return (
    <sup className="wk-cite">
      <button
        type="button"
        className="wk-cite-badge"
        aria-expanded={open}
        aria-describedby={open ? popoverId : undefined}
        onMouseEnter={reveal}
        onFocus={reveal}
        onMouseLeave={() => setOpen(false)}
        onBlur={() => setOpen(false)}
        onClick={() => (open ? setOpen(false) : reveal())}
      >
        {label}
      </button>
      {open ? (
        <span className="wk-cite-popover" id={popoverId} role="tooltip">
          <CitationPopoverBody
            state={state}
            kind={kind}
            sourceId={sourceId}
            onViewSource={onViewSource}
          />
        </span>
      ) : null}
    </sup>
  );
}

function CitationPopoverBody({
  state,
  kind,
  sourceId,
  onViewSource,
}: {
  state: FetchState;
  kind: SourceKind | null;
  sourceId: string;
  onViewSource?: (kind: SourceKind, id: string) => void;
}) {
  if (state.status === "loading") {
    return <span className="wk-cite-loading">Loading source…</span>;
  }
  if (state.status === "notfound") {
    return <span className="wk-cite-missing">Source not found</span>;
  }
  if (state.status === "error") {
    return <span className="wk-cite-missing">{state.message}</span>;
  }
  if (state.status === "loaded") {
    const { record } = state;
    return (
      <>
        <span className="wk-cite-kind">{SOURCE_KIND_LABELS[record.kind]}</span>
        <span className="wk-cite-title">{record.title || sourceId}</span>
        {record.origin ? (
          <span className="wk-cite-origin">{record.origin}</span>
        ) : null}
        {onViewSource && kind ? (
          <button
            type="button"
            className="wk-cite-view"
            onClick={() => onViewSource(kind, sourceId)}
          >
            View source
          </button>
        ) : null}
      </>
    );
  }
  return null;
}

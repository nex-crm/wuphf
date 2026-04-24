import { useEffect, useState } from "react";

import {
  fetchCatalog,
  type NotebookCatalogSummary,
  subscribeNotebookEvents,
} from "../../api/notebook";
import AgentNotebookView from "./AgentNotebookView";
import BookshelfCatalog from "./BookshelfCatalog";
import "../../styles/notebook.css";

/**
 * Route root for the `/notebooks` surface. Applies `.notebook-surface` so
 * tokens stay scoped, owns the SSE subscription, and dispatches between
 * bookshelf / agent / entry views.
 *
 * The app's top-level hash router writes to `notebookAgentSlug` +
 * `notebookEntrySlug` in the Zustand store; this component reads both and
 * calls the passed navigation handlers.
 */

interface NotebookProps {
  agentSlug: string | null;
  entrySlug: string | null;
  onOpenCatalog: () => void;
  onOpenAgent: (agentSlug: string) => void;
  onOpenEntry: (agentSlug: string, entrySlug: string | null) => void;
  onNavigateWiki: (wikiPath: string) => void;
}

export default function Notebook({
  agentSlug,
  entrySlug,
  onOpenCatalog,
  onOpenAgent,
  onOpenEntry,
  onNavigateWiki,
}: NotebookProps) {
  const [catalog, setCatalog] = useState<NotebookCatalogSummary | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(!agentSlug);
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  // Fetch catalog when rendering the bookshelf.
  useEffect(() => {
    if (agentSlug) return;
    let cancelled = false;
    setCatalogLoading(true);
    setCatalogError(null);
    fetchCatalog()
      .then((c) => {
        if (!cancelled) setCatalog(c);
      })
      .catch((err: unknown) => {
        if (!cancelled)
          setCatalogError(
            err instanceof Error ? err.message : "Failed to load",
          );
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [agentSlug]);

  // Subscribe to broker notebook:write + review:state_change events; on
  // any event, nudge the active view to refetch by bumping the tick.
  useEffect(() => {
    const unsub = subscribeNotebookEvents(() => {
      setRefreshTick((n) => n + 1);
    });
    return unsub;
  }, []);

  return (
    <div className="notebook-surface" data-testid="notebook-surface">
      <a href="#nb-entry-main" className="nb-skip-link">
        Skip to entry
      </a>
      {agentSlug ? (
        <AgentNotebookView
          key={`${agentSlug}-${refreshTick}`}
          agentSlug={agentSlug}
          entrySlug={entrySlug}
          onNavigateCatalog={onOpenCatalog}
          onSelectEntry={(slug) => onOpenEntry(agentSlug, slug)}
          onNavigateWiki={onNavigateWiki}
        />
      ) : catalogLoading ? (
        <div className="nb-loading" aria-busy="true">
          Loading bookshelf…
        </div>
      ) : catalogError ? (
        <div className="nb-article">
          <p className="nb-error" role="alert">
            Error: {catalogError}
          </p>
          <button
            type="button"
            className="nb-retry-btn"
            onClick={() => setRefreshTick((n) => n + 1)}
          >
            Retry
          </button>
        </div>
      ) : catalog ? (
        <BookshelfCatalog
          catalog={catalog}
          onOpenAgent={onOpenAgent}
          onOpenEntry={(a, e) => onOpenEntry(a, e)}
        />
      ) : null}
    </div>
  );
}

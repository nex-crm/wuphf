import { useCallback, useEffect, useState } from "react";

import {
  fetchMaintenanceSuggestion,
  type WikiMaintenanceAction,
  type WikiMaintenanceSuggestion,
  type WriteHumanResult,
  writeHumanArticle,
} from "../../api/wiki";

/**
 * WikiMaintenanceAssistant — collapsible side panel that lets the user ask
 * the broker for safe maintenance suggestions for the open article.
 *
 * Design rules (Phase 3 PR 7):
 *   - The panel is closed by default. Computing a suggestion is on-demand.
 *   - Suggestions never auto-apply. Accept routes through the existing
 *     /wiki/write-human path (same expected_sha conflict guard as the editor).
 *   - Reject is per (article SHA, action) and remembered for 24h in
 *     localStorage so the user is not pestered with the same suggestion.
 *   - The "resolve_contradiction" action delegates back to WikiLint's
 *     existing ResolveContradictionModal — we don't duplicate that flow.
 */

interface WikiMaintenanceAssistantProps {
  articlePath: string;
  /** SHA of the article currently rendered. Used as the rejection key. */
  articleSha: string;
  /**
   * Called when an accepted suggestion has been written. Lets the parent
   * (WikiArticle) bump its refresh nonce so the body reloads.
   */
  onApplied: () => void;
  /**
   * Optional initial action to focus when the panel opens. Set by callers
   * like WikiLint's "Suggest fix" button so the user lands directly on the
   * resolve-contradiction suggestion without scrolling.
   */
  initialAction?: WikiMaintenanceAction;
  /** Default to false. When true, the panel renders expanded on mount. */
  initiallyOpen?: boolean;
  /** Optional injection seam for tests — overrides the live writer. */
  writeArticle?: (params: {
    path: string;
    content: string;
    commitMessage: string;
    expectedSha: string;
  }) => Promise<WriteHumanResult>;
}

interface ActionMeta {
  action: WikiMaintenanceAction;
  label: string;
  blurb: string;
}

const ACTIONS: readonly ActionMeta[] = [
  {
    action: "summarize",
    label: "Summarize page",
    blurb: "Add a TL;DR derived from the lead paragraph.",
  },
  {
    action: "add_citation",
    label: "Add missing citation",
    blurb: "Mark un-sourced numeric or strong claims for follow-up.",
  },
  {
    action: "extract_facts",
    label: "Extract facts",
    blurb:
      "Propose structured triples for review before they go to the fact log.",
  },
  {
    action: "link_related",
    label: "Link related pages",
    blurb: "Append a Related section based on co-occurring entities.",
  },
  {
    action: "split_long_page",
    label: "Split long page",
    blurb: "Outline a per-section split when the article gets too long.",
  },
  {
    action: "refresh_stale",
    label: "Refresh stale page",
    blurb: "Surface recent fact-log activity for a stale page.",
  },
  {
    action: "resolve_contradiction",
    label: "Resolve contradiction",
    blurb: "Hand off to the existing contradiction-resolve flow.",
  },
];

const REJECT_KEY_PREFIX = "wuphf:wiki-maint:rejected:";
const REJECT_TTL_MS = 24 * 60 * 60 * 1000;

function rejectKey(sha: string, action: WikiMaintenanceAction): string {
  return `${REJECT_KEY_PREFIX}${sha || "unknown"}:${action}`;
}

function isRejectedRecently(
  sha: string,
  action: WikiMaintenanceAction,
  now: number,
): boolean {
  if (typeof window === "undefined") return false;
  try {
    const raw = window.localStorage.getItem(rejectKey(sha, action));
    if (!raw) return false;
    const ts = Number(raw);
    if (!Number.isFinite(ts)) return false;
    return now - ts < REJECT_TTL_MS;
  } catch {
    return false;
  }
}

function computeSuppressed(sha: string): Set<WikiMaintenanceAction> {
  const now = Date.now();
  const out = new Set<WikiMaintenanceAction>();
  for (const meta of ACTIONS) {
    if (isRejectedRecently(sha, meta.action, now)) {
      out.add(meta.action);
    }
  }
  return out;
}

function recordRejection(sha: string, action: WikiMaintenanceAction): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(rejectKey(sha, action), String(Date.now()));
  } catch {
    // localStorage quota / disabled — best effort only.
  }
}

interface SuggestionState {
  loading: boolean;
  suggestion: WikiMaintenanceSuggestion | null;
  error: string | null;
}

type SuggestionsByAction = Partial<
  Record<WikiMaintenanceAction, SuggestionState>
>;

export default function WikiMaintenanceAssistant({
  articlePath,
  articleSha,
  onApplied,
  initialAction,
  initiallyOpen = false,
  writeArticle = writeHumanArticle,
}: WikiMaintenanceAssistantProps) {
  const [open, setOpen] = useState(initiallyOpen || Boolean(initialAction));
  const [activeAction, setActiveAction] =
    useState<WikiMaintenanceAction | null>(initialAction ?? null);
  const [suggestions, setSuggestions] = useState<SuggestionsByAction>({});
  const [applyState, setApplyState] = useState<{
    action: WikiMaintenanceAction;
    pending: boolean;
    error: string | null;
  } | null>(null);

  // Re-evaluate the rejection list on every render. The check is a handful
  // of localStorage reads per article — cheap enough that memoization would
  // hide the real bug class (stale "snoozed" badges that don't refresh).
  const suppressed = computeSuppressed(articleSha);

  // Open the panel when an external trigger sets initialAction (e.g. WikiLint's
  // "Suggest fix" button hands us a contradiction). Synchronizing in an effect
  // (instead of just at mount) lets the same component instance accept new
  // targets without a remount.
  useEffect(() => {
    if (initialAction) {
      setOpen(true);
      setActiveAction(initialAction);
    }
  }, [initialAction]);

  const requestSuggestion = useCallback(
    async (action: WikiMaintenanceAction) => {
      setSuggestions((prev) => ({
        ...prev,
        [action]: { loading: true, suggestion: null, error: null },
      }));
      try {
        const s = await fetchMaintenanceSuggestion(action, articlePath);
        setSuggestions((prev) => ({
          ...prev,
          [action]: { loading: false, suggestion: s, error: null },
        }));
      } catch (err: unknown) {
        const msg =
          err instanceof Error ? err.message : "Failed to compute suggestion";
        setSuggestions((prev) => ({
          ...prev,
          [action]: { loading: false, suggestion: null, error: msg },
        }));
      }
    },
    [articlePath],
  );

  // When an action becomes active and we have not loaded its suggestion yet,
  // kick off the request. Subsequent clicks reuse the cached result.
  useEffect(() => {
    if (!activeAction) return;
    const existing = suggestions[activeAction];
    if (!existing) {
      void requestSuggestion(activeAction);
    }
  }, [activeAction, suggestions, requestSuggestion]);

  const handleSelect = (action: WikiMaintenanceAction) => {
    setActiveAction(action);
    setApplyState(null);
  };

  const handleReject = (action: WikiMaintenanceAction) => {
    recordRejection(articleSha, action);
    setSuggestions((prev) => ({ ...prev, [action]: undefined }));
    setActiveAction(null);
  };

  const handleAccept = async (suggestion: WikiMaintenanceSuggestion) => {
    if (!suggestion.diff?.proposed_content) return;
    setApplyState({ action: suggestion.action, pending: true, error: null });
    try {
      const res = await writeArticle({
        path: articlePath,
        content: suggestion.diff.proposed_content,
        commitMessage: `wiki: ${suggestion.action} via maintenance assistant`,
        expectedSha: suggestion.expected_sha ?? articleSha,
      });
      if ("conflict" in res && res.conflict) {
        setApplyState({
          action: suggestion.action,
          pending: false,
          error:
            "The article changed since the suggestion was computed. Re-open the assistant to recompute.",
        });
        return;
      }
      setApplyState(null);
      setSuggestions((prev) => ({ ...prev, [suggestion.action]: undefined }));
      setActiveAction(null);
      onApplied();
    } catch (err: unknown) {
      setApplyState({
        action: suggestion.action,
        pending: false,
        error:
          err instanceof Error ? err.message : "Failed to apply suggestion",
      });
    }
  };

  if (!open) {
    return (
      <section
        className="wk-related-panel"
        aria-label="Maintenance assistant (collapsed)"
        data-testid="wk-maint-collapsed"
      >
        <button
          type="button"
          className="wk-editor-save"
          style={{ padding: "6px 12px", fontSize: 13 }}
          onClick={() => setOpen(true)}
          data-testid="wk-maint-open"
        >
          Open maintenance assistant
        </button>
      </section>
    );
  }

  return (
    <section
      className="wk-related-panel"
      aria-label="Maintenance assistant"
      data-testid="wk-maint-panel"
    >
      <h2>Maintenance assistant</h2>
      <p
        style={{
          fontStyle: "italic",
          fontSize: 12,
          color: "var(--wk-text-tertiary)",
          margin: "0 0 10px",
        }}
      >
        Pick an action below. Suggestions are proposals only — nothing is
        written until you accept.
      </p>
      <ul className="wk-related-items">
        {ACTIONS.map((meta) => {
          const isActive = activeAction === meta.action;
          const isSuppressed = suppressed.has(meta.action);
          return (
            <li key={meta.action} className="wk-related-item">
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: 4,
                  flex: 1,
                }}
              >
                <button
                  type="button"
                  className="wk-related-link"
                  onClick={() => handleSelect(meta.action)}
                  disabled={isSuppressed}
                  data-testid={`wk-maint-action-${meta.action}`}
                  aria-expanded={isActive}
                  style={{
                    background: "none",
                    border: "none",
                    padding: 0,
                    cursor: isSuppressed ? "not-allowed" : "pointer",
                    textAlign: "left",
                    opacity: isSuppressed ? 0.45 : 1,
                  }}
                >
                  {meta.label}
                  {isSuppressed ? " (snoozed for 24h)" : ""}
                </button>
                <span className="wk-related-count">{meta.blurb}</span>
                {isActive ? (
                  <SuggestionView
                    state={suggestions[meta.action]}
                    onAccept={handleAccept}
                    onReject={() => handleReject(meta.action)}
                    applyState={
                      applyState && applyState.action === meta.action
                        ? applyState
                        : null
                    }
                  />
                ) : null}
              </div>
            </li>
          );
        })}
      </ul>
      <div style={{ marginTop: 12 }}>
        <button
          type="button"
          className="wk-related-link"
          onClick={() => setOpen(false)}
          data-testid="wk-maint-close"
          style={{
            background: "none",
            border: "none",
            padding: 0,
            cursor: "pointer",
          }}
        >
          Close panel
        </button>
      </div>
    </section>
  );
}

interface SuggestionViewProps {
  state: SuggestionState | undefined;
  onAccept: (s: WikiMaintenanceSuggestion) => Promise<void>;
  onReject: () => void;
  applyState: {
    action: WikiMaintenanceAction;
    pending: boolean;
    error: string | null;
  } | null;
}

function SuggestionView({
  state,
  onAccept,
  onReject,
  applyState,
}: SuggestionViewProps) {
  if (!state || state.loading) {
    return (
      <div
        className="wk-related-loading"
        role="status"
        data-testid="wk-maint-loading"
        style={{ marginTop: 8 }}
      >
        Computing…
      </div>
    );
  }
  if (state.error) {
    return (
      <div
        className="wk-related-error"
        role="alert"
        style={{ marginTop: 8 }}
        data-testid="wk-maint-error"
      >
        {state.error}
      </div>
    );
  }
  const s = state.suggestion;
  if (!s) return null;
  if (s.skipped) {
    return (
      <div
        className="wk-related-empty"
        role="status"
        style={{ marginTop: 8 }}
        data-testid="wk-maint-skipped"
      >
        {s.skipped_reason ?? "Nothing to do here right now."}
      </div>
    );
  }
  return (
    <SuggestionBody
      suggestion={s}
      onAccept={onAccept}
      onReject={onReject}
      applyState={applyState}
    />
  );
}

interface SuggestionBodyProps {
  suggestion: WikiMaintenanceSuggestion;
  onAccept: (s: WikiMaintenanceSuggestion) => Promise<void>;
  onReject: () => void;
  applyState: SuggestionViewProps["applyState"];
}

function SuggestionBody({
  suggestion: s,
  onAccept,
  onReject,
  applyState,
}: SuggestionBodyProps) {
  const canApply = Boolean(s.diff?.proposed_content);
  return (
    <div style={{ marginTop: 8 }} data-testid="wk-maint-suggestion">
      {s.description ? (
        <p
          style={{
            margin: "4px 0 8px",
            fontSize: 13,
            color: "var(--wk-text)",
          }}
        >
          {s.description}
        </p>
      ) : null}
      {s.diff ? <DiffPreview diff={s.diff} /> : null}
      {s.facts && s.facts.length > 0 ? (
        <FactProposalList facts={s.facts} />
      ) : null}
      {s.lint_finding ? (
        <ContradictionPointer
          summary={s.lint_finding.summary}
          reportDate={s.lint_report_date}
        />
      ) : null}
      {s.evidence && s.evidence.length > 0 ? (
        <EvidenceList items={s.evidence} />
      ) : null}
      <SuggestionActions
        canApply={canApply}
        pending={Boolean(applyState?.pending)}
        onAccept={() => {
          void onAccept(s);
        }}
        onReject={onReject}
      />
      {applyState?.error ? (
        <div
          className="wk-related-error"
          role="alert"
          style={{ marginTop: 6 }}
          data-testid="wk-maint-apply-error"
        >
          {applyState.error}
        </div>
      ) : null}
    </div>
  );
}

interface SuggestionActionsProps {
  canApply: boolean;
  pending: boolean;
  onAccept: () => void;
  onReject: () => void;
}

function SuggestionActions({
  canApply,
  pending,
  onAccept,
  onReject,
}: SuggestionActionsProps) {
  return (
    <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
      {canApply ? (
        <button
          type="button"
          className="wk-editor-save"
          style={{ padding: "4px 10px", fontSize: 12 }}
          disabled={pending}
          data-testid="wk-maint-accept"
          onClick={onAccept}
        >
          {pending ? "Applying…" : "Accept"}
        </button>
      ) : null}
      <button
        type="button"
        className="wk-related-link"
        onClick={onReject}
        data-testid="wk-maint-reject"
        style={{
          background: "none",
          border: "none",
          padding: "4px 10px",
          cursor: "pointer",
          fontSize: 12,
        }}
      >
        {canApply ? "Reject (snooze 24h)" : "Dismiss (snooze 24h)"}
      </button>
    </div>
  );
}

function DiffPreview({
  diff,
}: {
  diff: NonNullable<WikiMaintenanceSuggestion["diff"]>;
}) {
  const added = diff.added ?? [];
  const removed = diff.removed ?? [];
  if (added.length === 0 && removed.length === 0) return null;
  return (
    <pre
      data-testid="wk-maint-diff"
      style={{
        fontFamily: "var(--wk-mono)",
        background: "var(--wk-code-bg)",
        border: "1px solid var(--wk-border)",
        padding: 8,
        margin: 0,
        fontSize: 12,
        whiteSpace: "pre-wrap",
        maxHeight: 220,
        overflow: "auto",
      }}
    >
      {removed.map((line, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: Diff lines are intentionally rendered in source order; index is the stable key for this read-only preview.
          key={`r-${i}`}
          style={{ color: "#a14040" }}
        >
          {`- ${line}`}
        </div>
      ))}
      {added.map((line, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: Diff lines are intentionally rendered in source order; index is the stable key for this read-only preview.
          key={`a-${i}`}
          style={{ color: "#2a6a2a" }}
        >
          {`+ ${line}`}
        </div>
      ))}
    </pre>
  );
}

function FactProposalList({
  facts,
}: {
  facts: NonNullable<WikiMaintenanceSuggestion["facts"]>;
}) {
  return (
    <ul
      data-testid="wk-maint-facts"
      style={{
        listStyle: "none",
        margin: "8px 0",
        padding: 0,
        fontSize: 12,
      }}
    >
      {facts.map((f, i) => (
        <li
          // biome-ignore lint/suspicious/noArrayIndexKey: Proposed facts come from server in stable order; index suffices for this preview list.
          key={`${f.subject}-${f.predicate}-${i}`}
          style={{
            padding: "4px 0",
            borderBottom: "1px dashed var(--wk-border-light)",
          }}
        >
          <code>{f.subject}</code> <em>{f.predicate}</em>{" "}
          <code>{f.object}</code>
          <span
            className="wk-related-count"
            style={{ marginLeft: 6 }}
          >{`confidence ${f.confidence.toFixed(2)}`}</span>
        </li>
      ))}
    </ul>
  );
}

function ContradictionPointer({
  summary,
  reportDate,
}: {
  summary: string;
  reportDate: string | undefined;
}) {
  return (
    <div
      data-testid="wk-maint-contradiction-pointer"
      style={{
        padding: 8,
        background: "var(--wk-code-bg)",
        border: "1px solid var(--wk-border)",
        margin: "6px 0",
        fontSize: 12,
      }}
    >
      <strong>Contradiction:</strong> {summary}
      <div style={{ marginTop: 6 }}>
        Open the <a href="#/wiki/.lint">wiki health check</a>
        {reportDate ? ` (report ${reportDate})` : ""} to resolve through the
        existing flow.
      </div>
    </div>
  );
}

function EvidenceList({
  items,
}: {
  items: NonNullable<WikiMaintenanceSuggestion["evidence"]>;
}) {
  return (
    <details style={{ margin: "6px 0", fontSize: 12 }}>
      <summary>Evidence ({items.length})</summary>
      <ul style={{ listStyle: "none", padding: "4px 0 0", margin: 0 }}>
        {items.map((e, i) => (
          <li
            // biome-ignore lint/suspicious/noArrayIndexKey: Evidence list is server-ordered and read-only; index is sufficient as a key.
            key={`${e.kind}-${i}`}
            style={{ padding: "2px 0" }}
          >
            <span className="wk-related-count">[{e.kind}]</span>{" "}
            {e.path ? (
              <a
                href={`#/wiki/${e.path}`}
                className="wk-wikilink"
                data-wikilink="true"
              >
                {e.label}
              </a>
            ) : (
              <span>{e.label}</span>
            )}
            {e.snippet ? (
              <span style={{ color: "var(--wk-text-tertiary)" }}>
                {" "}
                — {e.snippet}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </details>
  );
}

import { useEffect, useRef, useState } from "react";

import {
  fetchHistory,
  fetchWikiDiff,
  restoreWikiVersion,
  type WikiHistoryCommit,
} from "../../api/wiki";
import { formatAgentName } from "../../lib/agentName";
import { formatRelativeTime } from "../../lib/format";
import { keyedByOccurrence } from "../../lib/reactKeys";

interface VersionHistoryProps {
  path: string;
  /**
   * Called after a successful restore with the NEW forward commit SHA the
   * broker created. The article view wires this to its refresh nonce so the
   * body refetches the now-current (restored) content.
   */
  onRestored?: (newSha: string) => void;
}

type CommitsState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; commits: WikiHistoryCommit[] };

type DiffState =
  | { status: "idle" }
  | { status: "loading"; sha: string }
  | { status: "error"; sha: string }
  | { status: "ready"; sha: string; diff: string };

type DiffLineKind = "added" | "removed" | "hunk" | "meta" | "context";

function classifyDiffLine(line: string): DiffLineKind {
  // File headers (+++/---) and the diff/index preamble are metadata, not
  // content changes. Check them before the single +/- content tests so a
  // `+++ b/file` header is never mis-coloured as an added line.
  if (line.startsWith("+++") || line.startsWith("---")) return "meta";
  if (
    line.startsWith("diff ") ||
    line.startsWith("index ") ||
    line.startsWith("new file") ||
    line.startsWith("deleted file") ||
    line.startsWith("rename ") ||
    line.startsWith("similarity ")
  ) {
    return "meta";
  }
  if (line.startsWith("@@")) return "hunk";
  if (line.startsWith("+")) return "added";
  if (line.startsWith("-")) return "removed";
  return "context";
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso);
  } catch {
    return iso;
  }
}

function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

export default function VersionHistory({
  path,
  onRestored,
}: VersionHistoryProps) {
  const [commitsState, setCommitsState] = useState<CommitsState>({
    status: "loading",
  });
  const [selectedSha, setSelectedSha] = useState<string | null>(null);
  const [diffState, setDiffState] = useState<DiffState>({ status: "idle" });
  const [confirmingSha, setConfirmingSha] = useState<string | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  // Load (or reload) the commit list whenever the article path changes.
  useEffect(() => {
    let cancelled = false;
    setCommitsState({ status: "loading" });
    setSelectedSha(null);
    setDiffState({ status: "idle" });
    setConfirmingSha(null);
    setRestoreError(null);
    fetchHistory(path)
      .then((res) => {
        if (cancelled) return;
        // fetchHistory already swallows transport errors into an empty list,
        // so an empty array here is "no history yet", not a failure.
        setCommitsState({ status: "ready", commits: res.commits ?? [] });
      })
      .catch(() => {
        if (cancelled) return;
        setCommitsState({ status: "error" });
      });
    return () => {
      cancelled = true;
    };
  }, [path]);

  // Fetch the diff for the selected commit. Distinct from the commit-list
  // effect so re-selecting does not re-list.
  useEffect(() => {
    if (!selectedSha) {
      setDiffState({ status: "idle" });
      return;
    }
    let cancelled = false;
    const sha = selectedSha;
    setDiffState({ status: "loading", sha });
    fetchWikiDiff(path, sha)
      .then((res) => {
        if (cancelled) return;
        setDiffState({ status: "ready", sha, diff: res.diff });
      })
      .catch(() => {
        if (cancelled) return;
        setDiffState({ status: "error", sha });
      });
    return () => {
      cancelled = true;
    };
  }, [path, selectedSha]);

  async function handleRestore(sha: string) {
    setRestoring(true);
    setRestoreError(null);
    try {
      const res = await restoreWikiVersion(path, sha);
      setConfirmingSha(null);
      onRestored?.(res.commit_sha);
    } catch (err: unknown) {
      setRestoreError(
        err instanceof Error ? err.message : "Restore failed. Please retry.",
      );
    } finally {
      setRestoring(false);
    }
  }

  return (
    <section
      className="wk-history"
      aria-label="Version history"
      data-testid="wk-version-history"
    >
      <CommitList
        state={commitsState}
        selectedSha={selectedSha}
        onSelect={(sha) => {
          setSelectedSha(sha);
          setConfirmingSha(null);
          setRestoreError(null);
        }}
      />
      <DiffPane
        path={path}
        selectedSha={selectedSha}
        diffState={diffState}
        confirming={confirmingSha === selectedSha}
        restoring={restoring}
        restoreError={restoreError}
        onAskRestore={(sha) => {
          setConfirmingSha(sha);
          setRestoreError(null);
        }}
        onCancelRestore={() => {
          setConfirmingSha(null);
          setRestoreError(null);
        }}
        onConfirmRestore={(sha) => {
          void handleRestore(sha);
        }}
      />
    </section>
  );
}

function CommitList({
  state,
  selectedSha,
  onSelect,
}: {
  state: CommitsState;
  selectedSha: string | null;
  onSelect: (sha: string) => void;
}) {
  if (state.status === "loading") {
    return (
      <div className="wk-history-list-status" role="status" aria-busy="true">
        Loading version history…
      </div>
    );
  }
  if (state.status === "error") {
    return (
      <div className="wk-history-list-status wk-history-error" role="alert">
        Could not load version history.
      </div>
    );
  }
  if (state.commits.length === 0) {
    return (
      <div className="wk-history-list-status" role="status">
        No version history yet.
      </div>
    );
  }
  return (
    <ul
      className="wk-history-list"
      // Each row is an independent aria-pressed toggle button in the natural
      // tab order (not a listbox/roving-tabindex pattern): the pressed button
      // is the commit whose diff is shown. Tab moves between commits; the
      // accessible name comes from the button's text content.
      aria-label="Commits, newest first"
    >
      {state.commits.map((commit) => {
        const isSelected = commit.sha === selectedSha;
        return (
          <li key={commit.sha} className="wk-history-row">
            <button
              type="button"
              className={`wk-history-commit${isSelected ? " is-selected" : ""}`}
              aria-pressed={isSelected}
              onClick={() => onSelect(commit.sha)}
            >
              <span className="wk-history-msg">
                {commit.msg || "(no message)"}
              </span>
              <span className="wk-history-meta">
                <span className="wk-history-author">
                  {formatAgentName(commit.author_slug)}
                </span>
                <span className="wk-history-sep" aria-hidden="true">
                  ·
                </span>
                <span className="wk-history-when">
                  {safeRelative(commit.date)}
                </span>
                <span className="wk-history-sep" aria-hidden="true">
                  ·
                </span>
                <span className="wk-history-sha">{shortSha(commit.sha)}</span>
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

function DiffPane({
  path,
  selectedSha,
  diffState,
  confirming,
  restoring,
  restoreError,
  onAskRestore,
  onCancelRestore,
  onConfirmRestore,
}: {
  path: string;
  selectedSha: string | null;
  diffState: DiffState;
  confirming: boolean;
  restoring: boolean;
  restoreError: string | null;
  onAskRestore: (sha: string) => void;
  onCancelRestore: () => void;
  onConfirmRestore: (sha: string) => void;
}) {
  if (!selectedSha) {
    return (
      <div className="wk-history-diff-empty" role="status">
        Select a version to see what changed.
      </div>
    );
  }
  return (
    <div className="wk-history-diff">
      <div className="wk-history-diff-bar">
        <span className="wk-history-sha">{shortSha(selectedSha)}</span>
        <RestoreControl
          path={path}
          sha={selectedSha}
          confirming={confirming}
          restoring={restoring}
          onAskRestore={onAskRestore}
          onCancelRestore={onCancelRestore}
          onConfirmRestore={onConfirmRestore}
        />
      </div>
      {restoreError ? (
        <p className="wk-history-restore-error" role="alert">
          {restoreError}
        </p>
      ) : null}
      <DiffBody diffState={diffState} />
    </div>
  );
}

function RestoreControl({
  path,
  sha,
  confirming,
  restoring,
  onAskRestore,
  onCancelRestore,
  onConfirmRestore,
}: {
  path: string;
  sha: string;
  confirming: boolean;
  restoring: boolean;
  onAskRestore: (sha: string) => void;
  onCancelRestore: () => void;
  onConfirmRestore: (sha: string) => void;
}) {
  void path;
  const triggerRef = useRef<HTMLButtonElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  // True only while we are leaving the confirm state, so focus returns to the
  // trigger after a cancel/complete (the user's prior anchor) but NOT on the
  // initial mount, where the trigger is just the resting affordance.
  const wasConfirming = useRef(false);
  const headingId = `wk-restore-confirm-${shortSha(sha)}`;

  // On entering the confirming state, move focus to Cancel (the safe default)
  // so the alertdialog is announced and the keyboard user starts on the
  // non-destructive choice. On leaving it (cancel or completed restore),
  // return focus to the trigger button so the keyboard anchor is not lost.
  useEffect(() => {
    if (confirming) {
      wasConfirming.current = true;
      cancelRef.current?.focus();
    } else if (wasConfirming.current) {
      wasConfirming.current = false;
      triggerRef.current?.focus();
    }
  }, [confirming]);

  if (!confirming) {
    return (
      <button
        ref={triggerRef}
        type="button"
        className="wk-history-restore-btn"
        onClick={() => onAskRestore(sha)}
        aria-label={`Restore version ${shortSha(sha)}`}
      >
        Restore this version
      </button>
    );
  }
  return (
    // Tell, don't ask: we state the recommendation + the consequence, then
    // give one click to proceed and one to back out. Not a multiple-choice
    // menu — the human only confirms a state-changing write. role=alertdialog
    // announces the prompt on entry and is labelled by the consequence text;
    // focus lands on Cancel (the safe default) via the effect above, and
    // Escape cancels.
    <span
      className="wk-history-confirm"
      role="alertdialog"
      aria-labelledby={headingId}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !restoring) {
          event.stopPropagation();
          onCancelRestore();
        }
      }}
    >
      <span id={headingId} className="wk-history-confirm-msg">
        Replace the current article with version {shortSha(sha)}? This writes a
        new commit.
      </span>
      <button
        type="button"
        className="wk-history-restore-btn wk-history-restore-confirm"
        onClick={() => onConfirmRestore(sha)}
        disabled={restoring}
        aria-label={`Confirm restore of version ${shortSha(sha)}`}
      >
        {restoring ? "Restoring…" : "Restore"}
      </button>
      <button
        ref={cancelRef}
        type="button"
        className="wk-history-restore-cancel"
        onClick={onCancelRestore}
        disabled={restoring}
      >
        Cancel
      </button>
    </span>
  );
}

function DiffBody({ diffState }: { diffState: DiffState }) {
  if (diffState.status === "loading") {
    return (
      <div className="wk-history-diff-status" role="status" aria-busy="true">
        Loading diff…
      </div>
    );
  }
  if (diffState.status === "error") {
    return (
      <div className="wk-history-diff-status wk-history-error" role="alert">
        Could not load this version's diff.
      </div>
    );
  }
  if (diffState.status !== "ready") return null;
  if (diffState.diff.trim() === "") {
    return (
      <div className="wk-history-diff-status" role="status">
        No changes recorded for this version.
      </div>
    );
  }
  const lines = diffState.diff.split("\n");
  return (
    // A named, keyboard-focusable scroll region for the diff. The section's
    // implicit region role + accessible name let screen-reader users reach it;
    // tabIndex={0} makes the overflow:auto region focusable so a keyboard-only
    // user can scroll it with the arrow keys (WCAG technique G202: a scrollable
    // region must be reachable by keyboard). biome's noNoninteractiveTabindex
    // has no carve-out for scrollable regions and would strip the tabIndex,
    // re-introducing the keyboard trap the review flagged; suppress it on the
    // attribute below rather than fake an interactive role the element lacks.
    <section
      className="wk-history-diff-pre"
      aria-label={`Diff for ${shortSha(diffState.sha)}`}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: WCAG G202 — the overflow:auto diff must be keyboard-scrollable; tabIndex is required and the rule has no scrollable-region exception.
      tabIndex={0}
    >
      {/* keyedByOccurrence keeps duplicate diff lines (blank lines, repeated
          context) on stable, unique keys without an index-based key. Empty
          lines render with no text node; .wk-diff-line keeps their height via
          a CSS min-height so a blank diff line still occupies a row. */}
      {keyedByOccurrence(lines, (line) => line).map(({ key, value: line }) => {
        const kind = classifyDiffLine(line);
        return (
          <span key={key} className={`wk-diff-line wk-diff-${kind}`}>
            {line}
          </span>
        );
      })}
    </section>
  );
}

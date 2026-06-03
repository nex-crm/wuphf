import { lazy, Suspense, useCallback, useMemo } from "react";

import type { WikiCatalogEntry } from "../../api/wiki";
import { useWikiEditorController } from "../../hooks/useWikiEditorController";

/**
 * The reference-clone rich editor lives in a lazy chunk (Tiptap + extensions +
 * lowlight + katex). It is the single, always-on editing surface for wiki
 * articles — there is no longer a plain-markdown source/preview fallback.
 */
const RefCloneEditor = lazy(() => import("./editor/refclone/RefCloneEditor"));

interface WikiEditorProps {
  /** Target article path, e.g. `team/people/nazz.md`. */
  path: string;
  /** Markdown the editor starts with (article.content when present). */
  initialContent: string;
  /** SHA the editor opened against; sent back as expected_sha on save. */
  expectedSha: string;
  /** Server's last-edited timestamp for the article, used to decide whether
   *  a cached localStorage draft is newer than what's on disk. */
  serverLastEditedTs?: string;
  /** Catalog the rich editor uses to resolve wikilinks (mark broken ones) and
   *  to power the mention picker. Pass the same list WikiArticle renders against. */
  catalog?: WikiCatalogEntry[];
  /** Called after a successful save so the parent can refetch. */
  onSaved: (newSha: string) => void;
  /** Called when the user cancels. */
  onCancel: () => void;
}

function formatAgo(isoOrMs: string): string {
  const t =
    typeof isoOrMs === "string" && isoOrMs.length > 0
      ? Date.parse(isoOrMs)
      : NaN;
  if (!Number.isFinite(t)) return "moments ago";
  const deltaSec = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (deltaSec < 5) return "just now";
  if (deltaSec < 60) return `${deltaSec}s ago`;
  const mins = Math.floor(deltaSec / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  return `${days}d ago`;
}

/**
 * WYSIWYG-first wiki article editor with autosaved drafts.
 *
 * The Tiptap rich editor is the only editing surface — the former
 * markdown-source textarea, the live ReactMarkdown preview pane, and the
 * source/rich + preview toggles have been removed. This component owns
 * presentation only (the rich editor body, draft/conflict/error banners, the
 * commit input, and Save/Cancel); the draft/save/conflict/SHA state machine
 * lives in `useWikiEditorController`.
 */
export default function WikiEditor({
  path,
  initialContent,
  expectedSha,
  serverLastEditedTs,
  catalog = [],
  onSaved,
  onCancel,
}: WikiEditorProps) {
  const {
    content,
    setContent,
    commitMessage,
    setCommitMessage,
    saving,
    error,
    conflict,
    draft,
    handleRestoreDraft,
    handleDiscardDraft,
    handleSave,
    handleReloadConflict,
  } = useWikiEditorController({
    path,
    initialContent,
    expectedSha,
    serverLastEditedTs,
    onSaved,
  });

  const catalogSlugs = useMemo(
    () => new Set(catalog.map((c) => c.path)),
    [catalog],
  );
  const resolver = useCallback(
    (slug: string) => catalogSlugs.has(slug),
    [catalogSlugs],
  );

  return (
    <div className="wk-editor" data-testid="wk-editor">
      {draft ? (
        <div
          className="wk-editor-banner wk-editor-banner--draft"
          role="alert"
          data-testid="wk-editor-draft-banner"
        >
          Unsaved draft from {formatAgo(draft.saved_at)}.
          <div className="wk-editor-banner-actions">
            <button
              type="button"
              onClick={handleRestoreDraft}
              data-testid="wk-editor-draft-restore"
            >
              Restore draft
            </button>
            <button
              type="button"
              onClick={handleDiscardDraft}
              data-testid="wk-editor-draft-discard"
            >
              Discard
            </button>
          </div>
        </div>
      ) : null}
      {conflict ? (
        <div
          className="wk-editor-banner wk-editor-banner--conflict"
          role="alert"
        >
          <strong>Someone else edited this article.</strong> Your save was
          rejected because the article changed since you opened it.
          <div className="wk-editor-banner-actions">
            <button type="button" onClick={handleReloadConflict}>
              Reload latest &amp; re-apply
            </button>
          </div>
        </div>
      ) : null}
      {error && !conflict && (
        <div className="wk-editor-banner wk-editor-banner--error" role="alert">
          {error}
        </div>
      )}
      <div className="wk-editor-panes">
        <div className="wk-editor-pane wk-editor-pane--rich">
          {/* Visible region label. Rendered as a span (not a <label>) because
              the editable control is the ProseMirror surface inside
              TiptapWikiEditor, which references this id via aria-labelledby
              (labelId) — a span avoids a control-less <label> while still
              naming the editor for assistive tech. */}
          <span id="wk-editor-source-label" className="wk-editor-label">
            Editing {path}
          </span>
          <Suspense
            fallback={
              <div
                className="wk-editor-rich-fallback"
                data-testid="wk-editor-rich-loading"
              >
                Loading rich editor…
              </div>
            }
          >
            <div className="wk-editor-rich" data-testid="wk-editor-rich">
              <RefCloneEditor
                content={content}
                onChange={setContent}
                resolver={resolver}
                catalog={catalog}
              />
            </div>
          </Suspense>
        </div>
      </div>
      <label className="wk-editor-label" htmlFor="wk-editor-commit-msg">
        Edit summary
      </label>
      <input
        id="wk-editor-commit-msg"
        className="wk-editor-commit"
        data-testid="wk-editor-commit"
        type="text"
        placeholder="human: short description of the edit"
        value={commitMessage}
        onChange={(e) => setCommitMessage(e.target.value)}
      />
      <div className="wk-editor-actions">
        <button
          type="button"
          className="wk-editor-save"
          data-testid="wk-editor-save"
          onClick={handleSave}
          disabled={saving}
        >
          {saving ? "Saving…" : "Save changes"}
        </button>
        <button
          type="button"
          className="wk-editor-cancel"
          data-testid="wk-editor-cancel"
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </button>
      </div>
      <p className="wk-editor-help">
        <code>[[slug]]</code> creates a wikilink. Saved as commit author{" "}
        <strong>Human &lt;human@wuphf.local&gt;</strong>. Type <code>/</code>{" "}
        for inserts; <code>@</code> opens the mention picker. Select text, then{" "}
        <code>Mod-e</code> adds a link and <code>Mod-Shift-h</code> toggles
        highlight.
      </p>
    </div>
  );
}

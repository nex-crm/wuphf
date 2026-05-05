// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import ReactMarkdown from "react-markdown";

import type { WikiCatalogEntry } from "../../api/wiki";
import { useWikiEditorController } from "../../hooks/useWikiEditorController";
import {
  buildMarkdownComponents,
  buildRehypePlugins,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";

/**
 * Milkdown lives in a lazy chunk (~100kB gzipped). Users who never toggle
 * Rich mode never download it.
 */
const RichWikiEditor = lazy(() => import("./editor/RichWikiEditor"));

type EditorMode = "source" | "rich";
const EDITOR_MODE_KEY_PREFIX = "wuphf:editor-mode:";

function readEditorMode(path: string): EditorMode {
  if (typeof window === "undefined") return "source";
  try {
    const raw = window.localStorage.getItem(EDITOR_MODE_KEY_PREFIX + path);
    return raw === "rich" ? "rich" : "source";
  } catch {
    return "source";
  }
}

function writeEditorMode(path: string, mode: EditorMode): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(EDITOR_MODE_KEY_PREFIX + path, mode);
  } catch {
    // Storage disabled / out of quota — toggle still works for the session.
  }
}

interface MobileTabsProps {
  mobileView: "source" | "preview";
  setMobileView: (next: "source" | "preview") => void;
}

function MobileTabs({ mobileView, setMobileView }: MobileTabsProps) {
  return (
    <div
      className="wk-editor-mobile-tabs"
      role="tablist"
      data-testid="wk-editor-mobile-tabs"
    >
      <button
        type="button"
        role="tab"
        aria-selected={mobileView === "source"}
        className={`wk-editor-mobile-tab${mobileView === "source" ? " is-active" : ""}`}
        onClick={() => setMobileView("source")}
        data-testid="wk-editor-mobile-source"
      >
        Source
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={mobileView === "preview"}
        className={
          "wk-editor-mobile-tab" +
          (mobileView === "preview" ? " is-active" : "")
        }
        onClick={() => setMobileView("preview")}
        data-testid="wk-editor-mobile-preview"
      >
        Preview
      </button>
    </div>
  );
}

interface SourcePaneProps {
  path: string;
  editorMode: EditorMode;
  content: string;
  setContent: (next: string) => void;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
}

/**
 * The left/source pane swaps between the textarea and the lazy Milkdown
 * surface based on `editorMode`. Pulled out of `WikiEditor` so the parent
 * stays under Biome's cognitive-complexity ceiling.
 */
function SourcePane({
  path,
  editorMode,
  content,
  setContent,
  textareaRef,
}: SourcePaneProps) {
  const labelText = `Article source (${path})`;
  return (
    <div className="wk-editor-pane wk-editor-pane--source">
      <label
        id="wk-editor-source-label"
        className="wk-editor-label"
        // The textarea only mounts in source mode, so `htmlFor` only points
        // to it when relevant. In rich mode the wrapper uses
        // `aria-labelledby` against this label's id so the visible text
        // both labels the editor *and* clicks through, instead of being a
        // detached caption with a duplicated `aria-label`.
        htmlFor={editorMode === "source" ? "wk-editor-textarea" : undefined}
      >
        {labelText}
      </label>
      {editorMode === "rich" ? (
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
          <div
            className="wk-editor-rich"
            data-testid="wk-editor-rich"
            aria-labelledby="wk-editor-source-label"
          >
            <RichWikiEditor content={content} onChange={setContent} />
          </div>
        </Suspense>
      ) : (
        <textarea
          id="wk-editor-textarea"
          ref={textareaRef}
          className="wk-editor-textarea"
          data-testid="wk-editor-textarea"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={true}
          rows={28}
        />
      )}
    </div>
  );
}

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
  /** Catalog used by the preview pane to resolve wikilinks and mark
   *  broken ones. Pass the same list WikiArticle renders against. */
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
 * Plain-markdown editor with autosaved drafts and a live preview pane.
 *
 * Editor state (draft restore/discard, autosave debounce, save, conflict
 * reload, mobile source/preview toggle) lives in `useWikiEditorController`
 * so the upcoming rich editor can share the same state machine. This
 * component owns presentation only: textarea, preview pane, banners.
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
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const {
    content,
    setContent,
    commitMessage,
    setCommitMessage,
    saving,
    error,
    conflict,
    draft,
    previewOn,
    setPreviewOn,
    mobileView,
    setMobileView,
    isMobile,
    showSource,
    showPreview,
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
  const remarkPlugins = useMemo(() => buildRemarkPlugins(resolver), [resolver]);
  const rehypePlugins = useMemo(() => buildRehypePlugins(), []);
  const markdownComponents = useMemo(
    () => buildMarkdownComponents({ resolver }),
    [resolver],
  );

  // Per-article editor mode — persists across sessions so a user who picks
  // Rich on a page they edit often gets it back next time without resetting.
  //
  // The mode is keyed to `path` *synchronously*: when the parent navigates
  // to a different article, the new article's stored mode must apply on
  // the very first render. A previous version held mode in `useState` and
  // reset it via `useEffect`, which left mode one render behind path —
  // a `rich -> source` navigation would mount Milkdown for one paint
  // before correcting itself. Storing `{ path, mode }` lets us detect the
  // stale snapshot and read storage inline when it doesn't match.
  const [storedMode, setStoredMode] = useState<{
    path: string;
    mode: EditorMode;
  }>(() => ({ path, mode: readEditorMode(path) }));
  const editorMode: EditorMode =
    storedMode.path === path ? storedMode.mode : readEditorMode(path);
  useEffect(() => {
    setStoredMode({ path, mode: readEditorMode(path) });
  }, [path]);
  const toggleEditorMode = useCallback(() => {
    setStoredMode((prev) => {
      const current: EditorMode =
        prev.path === path ? prev.mode : readEditorMode(path);
      const next: EditorMode = current === "rich" ? "source" : "rich";
      writeEditorMode(path, next);
      return { path, mode: next };
    });
  }, [path]);

  return (
    <div
      className={`wk-editor${previewOn ? " wk-editor--with-preview" : ""}`}
      data-testid="wk-editor"
    >
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
      {previewOn && isMobile ? (
        <MobileTabs mobileView={mobileView} setMobileView={setMobileView} />
      ) : null}
      <div className="wk-editor-panes">
        {showSource ? (
          <SourcePane
            path={path}
            editorMode={editorMode}
            content={content}
            setContent={setContent}
            textareaRef={textareaRef}
          />
        ) : null}
        {showPreview ? (
          <div
            className="wk-editor-pane wk-editor-pane--preview"
            data-testid="wk-editor-preview"
            aria-label="Live preview"
          >
            <div className="wk-editor-preview-body wk-article-body">
              <ReactMarkdown
                remarkPlugins={remarkPlugins}
                rehypePlugins={rehypePlugins}
                components={markdownComponents}
              >
                {content}
              </ReactMarkdown>
            </div>
          </div>
        ) : null}
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
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </button>
        <button
          type="button"
          className={`wk-editor-preview-toggle${previewOn ? " is-on" : ""}`}
          data-testid="wk-editor-preview-toggle"
          aria-pressed={previewOn}
          onClick={() => setPreviewOn((v) => !v)}
        >
          {previewOn ? "Hide preview" : "Preview"}
        </button>
        <button
          type="button"
          className={`wk-editor-mode-toggle${editorMode === "rich" ? " is-on" : ""}`}
          data-testid="wk-editor-mode-toggle"
          // Visible label names the *current* mode so it agrees with
          // `aria-pressed`. Screen readers announce e.g. "Rich, pressed"
          // when rich is active rather than the contradictory pairing of
          // "Source, pressed" the previous implementation produced.
          aria-pressed={editorMode === "rich"}
          onClick={toggleEditorMode}
        >
          {editorMode === "rich" ? "Rich" : "Source"}
        </button>
      </div>
      <p className="wk-editor-help">
        Plain markdown. <code>[[slug]]</code> creates a wikilink. Saved as
        commit author <strong>Human &lt;human@wuphf.local&gt;</strong>.
      </p>
    </div>
  );
}

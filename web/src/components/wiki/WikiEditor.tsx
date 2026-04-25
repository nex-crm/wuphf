import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";

import type { WikiCatalogEntry } from "../../api/wiki";
import { type WriteHumanConflict, writeHumanArticle } from "../../api/wiki";
import {
  buildMarkdownComponents,
  buildRehypePlugins,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";

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

/** Draft envelope persisted to localStorage. */
interface DraftPayload {
  content: string;
  summary: string;
  saved_at: string;
}

const DRAFT_KEY_PREFIX = "wuphf:draft:";
const AUTOSAVE_DEBOUNCE_MS = 750;
const MOBILE_BREAKPOINT_PX = 768;

function draftKey(path: string): string {
  return `${DRAFT_KEY_PREFIX}${path}`;
}

function readDraft(path: string): DraftPayload | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(draftKey(path));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<DraftPayload>;
    if (
      typeof parsed.content !== "string" ||
      typeof parsed.saved_at !== "string"
    ) {
      return null;
    }
    return {
      content: parsed.content,
      summary: typeof parsed.summary === "string" ? parsed.summary : "",
      saved_at: parsed.saved_at,
    };
  } catch {
    return null;
  }
}

function writeDraft(path: string, payload: DraftPayload): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(draftKey(path), JSON.stringify(payload));
  } catch {
    // Out of quota / disabled storage — silently skip, in-memory state
    // still protects the user for the session.
  }
}

function clearDraft(path: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(draftKey(path));
  } catch {
    // Ignore — storage unavailable.
  }
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

/** Narrow viewport detector — mobile layout collapses split view to tabs. */
function useIsMobileViewport(): boolean {
  const getMatch = (): boolean => {
    if (typeof window === "undefined" || !window.matchMedia) return false;
    return window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`)
      .matches;
  };
  const [isMobile, setIsMobile] = useState<boolean>(getMatch);
  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`);
    const update = () => setIsMobile(mq.matches);
    update();
    // Safari <14 only supports addListener.
    if (typeof mq.addEventListener === "function") {
      mq.addEventListener("change", update);
      return () => mq.removeEventListener("change", update);
    }
    mq.addListener(update);
    return () => mq.removeListener(update);
  }, []);
  return isMobile;
}

/**
 * Plain-markdown editor with autosaved drafts and a live preview pane.
 *
 * Autosave: debounced writes to localStorage keyed by article path. On
 * re-open, if the stored draft is newer than the server's last_edited_ts,
 * a yellow banner offers [Restore] / [Discard].
 *
 * Preview: "Preview" toggle flips into split view (desktop) or tab
 * switcher (<768px viewport). The preview uses the same remark/rehype
 * pipeline as `WikiArticle` so wikilinks, tables, and image embeds render
 * identically.
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
  const [content, setContent] = useState(initialContent);
  const [commitMessage, setCommitMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<WriteHumanConflict | null>(null);
  const [draft, setDraft] = useState<DraftPayload | null>(null);
  const [previewOn, setPreviewOn] = useState(false);
  const [mobileView, setMobileView] = useState<"source" | "preview">("source");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const isMobile = useIsMobileViewport();

  // On mount / when the article changes, reset editor state AND check
  // localStorage for a draft newer than server's last_edited_ts.
  useEffect(() => {
    setContent(initialContent);
    setCommitMessage("");
    setError(null);
    setConflict(null);
    const stored = readDraft(path);
    if (!stored) {
      setDraft(null);
      return;
    }
    // If the server has a newer edit than the draft, the draft is stale
    // (someone saved the article after the user left the editor); discard.
    const serverTs = serverLastEditedTs ? Date.parse(serverLastEditedTs) : NaN;
    const draftTs = Date.parse(stored.saved_at);
    if (
      Number.isFinite(serverTs) &&
      Number.isFinite(draftTs) &&
      serverTs >= draftTs
    ) {
      clearDraft(path);
      setDraft(null);
      return;
    }
    // Only surface the banner when the draft diverges from the fresh server
    // content; otherwise it's noise.
    if (stored.content === initialContent) {
      setDraft(null);
      return;
    }
    setDraft(stored);
  }, [path, initialContent, serverLastEditedTs]);

  // Debounced autosave. Anchors on `content` + `commitMessage` and writes
  // after AUTOSAVE_DEBOUNCE_MS of quiescence. Skip writing if nothing has
  // diverged from the server-supplied content — no point polluting storage.
  useEffect(() => {
    if (content === initialContent && commitMessage === "") return;
    const handle = window.setTimeout(() => {
      writeDraft(path, {
        content,
        summary: commitMessage,
        saved_at: new Date().toISOString(),
      });
    }, AUTOSAVE_DEBOUNCE_MS);
    return () => window.clearTimeout(handle);
  }, [path, content, commitMessage, initialContent]);

  const handleRestoreDraft = useCallback(() => {
    if (!draft) return;
    setContent(draft.content);
    setCommitMessage(draft.summary);
    setDraft(null);
  }, [draft]);

  const handleDiscardDraft = useCallback(() => {
    clearDraft(path);
    setDraft(null);
  }, [path]);

  async function handleSave() {
    if (saving) return;
    setError(null);
    setConflict(null);
    if (!content.trim()) {
      setError("Article content cannot be empty.");
      return;
    }
    setSaving(true);
    try {
      const result = await writeHumanArticle({
        path,
        content,
        commitMessage: commitMessage.trim() || `human: update ${path}`,
        expectedSha,
      });
      if ("conflict" in result) {
        // Keep the draft — user's work should survive a conflict round-trip.
        setConflict(result);
        return;
      }
      // Saved OK — the draft is now redundant.
      clearDraft(path);
      setDraft(null);
      onSaved(result.commit_sha);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Save failed.");
    } finally {
      setSaving(false);
    }
  }

  function handleReloadConflict() {
    if (!conflict) return;
    setContent(conflict.current_content);
    onSaved(conflict.current_sha);
  }

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

  const showSource = !(previewOn && isMobile) || mobileView === "source";
  const showPreview = previewOn && (!isMobile || mobileView === "preview");

  return (
    <div
      className={`wk-editor${previewOn ? " wk-editor--with-preview" : ""}`}
      data-testid="wk-editor"
    >
      {draft && (
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
      )}
      {conflict && (
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
      )}
      {error && !conflict && (
        <div className="wk-editor-banner wk-editor-banner--error" role="alert">
          {error}
        </div>
      )}
      {previewOn && isMobile && (
        <div
          className="wk-editor-mobile-tabs"
          role="tablist"
          data-testid="wk-editor-mobile-tabs"
        >
          <button
            type="button"
            role="tab"
            aria-selected={mobileView === "source"}
            className={
              "wk-editor-mobile-tab" +
              (mobileView === "source" ? " is-active" : "")
            }
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
      )}
      <div className="wk-editor-panes">
        {showSource && (
          <div className="wk-editor-pane wk-editor-pane--source">
            <label className="wk-editor-label" htmlFor="wk-editor-textarea">
              Article source ({path})
            </label>
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
          </div>
        )}
        {showPreview && (
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
        )}
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
      </div>
      <p className="wk-editor-help">
        Plain markdown. <code>[[slug]]</code> creates a wikilink. Saved as
        commit author <strong>Human &lt;human@wuphf.local&gt;</strong>.
      </p>
    </div>
  );
}

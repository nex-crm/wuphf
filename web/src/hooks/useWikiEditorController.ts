import { useCallback, useEffect, useState } from "react";

import {
  writeHumanArticle as defaultWriteHumanArticle,
  type WriteHumanConflict,
  type WriteHumanResult,
} from "../api/wiki";

/**
 * Draft envelope persisted to localStorage. Schema is intentionally minimal so
 * forward-compat reads can ignore unknown fields.
 */
export interface DraftPayload {
  content: string;
  summary: string;
  saved_at: string;
}

export type MobileView = "source" | "preview";

const DRAFT_KEY_PREFIX = "wuphf:draft:";
export const AUTOSAVE_DEBOUNCE_MS = 750;
export const MOBILE_BREAKPOINT_PX = 768;

export function draftKey(path: string): string {
  return `${DRAFT_KEY_PREFIX}${path}`;
}

export function readDraft(path: string): DraftPayload | null {
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

export function writeDraft(path: string, payload: DraftPayload): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(draftKey(path), JSON.stringify(payload));
  } catch {
    // Out of quota / disabled storage — silently skip; in-memory state still
    // protects the user for the session.
  }
}

export function clearDraft(path: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(draftKey(path));
  } catch {
    // Ignore — storage unavailable.
  }
}

/** Narrow viewport detector — mobile layout collapses split view to tabs. */
export function useIsMobileViewport(): boolean {
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

export interface UseWikiEditorControllerParams {
  path: string;
  initialContent: string;
  expectedSha: string;
  serverLastEditedTs?: string;
  onSaved: (newSha: string) => void;
  /** Override for tests — defaults to the real wiki API client. */
  writeHumanArticle?: (params: {
    path: string;
    content: string;
    commitMessage: string;
    expectedSha: string;
  }) => Promise<WriteHumanResult>;
}

export interface UseWikiEditorControllerResult {
  content: string;
  setContent: (next: string) => void;
  commitMessage: string;
  setCommitMessage: (next: string) => void;
  saving: boolean;
  error: string | null;
  conflict: WriteHumanConflict | null;
  draft: DraftPayload | null;
  previewOn: boolean;
  setPreviewOn: (next: boolean | ((v: boolean) => boolean)) => void;
  mobileView: MobileView;
  setMobileView: (next: MobileView) => void;
  isMobile: boolean;
  showSource: boolean;
  showPreview: boolean;
  handleRestoreDraft: () => void;
  handleDiscardDraft: () => void;
  handleSave: () => Promise<void>;
  handleReloadConflict: () => void;
}

/**
 * Editor state machine extracted from WikiEditor so the upcoming rich editor
 * can share the same draft/save/conflict/SHA mechanics. Pure logic — owns no
 * DOM or markdown rendering.
 *
 * Behavior is preserved 1:1 from the original textarea editor:
 *   - On path/initialContent change: reset state and surface a draft banner
 *     when the cached draft is newer than the server's last-edited timestamp
 *     AND diverges from server content.
 *   - Autosave: debounced localStorage write keyed by article path; skipped
 *     when content matches server and commit message is empty.
 *   - Save: posts via writeHumanArticle with expectedSha; surfaces conflict
 *     payloads without clearing the draft so the user's work survives a
 *     concurrent edit.
 *   - Reload conflict: replaces content with the server's current bytes and
 *     promotes the conflict's current_sha as the new opened SHA via onSaved.
 */
export function useWikiEditorController(
  params: UseWikiEditorControllerParams,
): UseWikiEditorControllerResult {
  const {
    path,
    initialContent,
    expectedSha,
    serverLastEditedTs,
    onSaved,
    writeHumanArticle = defaultWriteHumanArticle,
  } = params;

  const [content, setContent] = useState(initialContent);
  const [commitMessage, setCommitMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<WriteHumanConflict | null>(null);
  const [draft, setDraft] = useState<DraftPayload | null>(null);
  const [previewOn, setPreviewOn] = useState(false);
  const [mobileView, setMobileView] = useState<MobileView>("source");
  const isMobile = useIsMobileViewport();

  // Track the SHA we expect to overwrite separately from the prop. After a
  // successful save or a conflict reload we promote the new SHA locally so a
  // follow-up save from the same controller instance does not race the parent's
  // refetch and re-conflict against the version we just wrote.
  const [currentExpectedSha, setCurrentExpectedSha] = useState(expectedSha);

  // On mount / when the article changes, reset editor state AND check
  // localStorage for a draft newer than server's last_edited_ts.
  useEffect(() => {
    setContent(initialContent);
    setCommitMessage("");
    setError(null);
    setConflict(null);
    setCurrentExpectedSha(expectedSha);
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
  }, [path, initialContent, serverLastEditedTs, expectedSha]);

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

  const handleSave = useCallback(async () => {
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
        expectedSha: currentExpectedSha,
      });
      if ("conflict" in result) {
        // Persist the in-memory edit synchronously: a 409 can land before
        // the autosave debounce fires, and `handleReloadConflict` will
        // overwrite `content` with the server copy. Without this write,
        // the user's unsaved work is unrecoverable after reload.
        writeDraft(path, {
          content,
          summary: commitMessage,
          saved_at: new Date().toISOString(),
        });
        setConflict(result);
        return;
      }
      // Saved OK — the draft is now redundant.
      clearDraft(path);
      setDraft(null);
      // Promote the new SHA locally so an immediate follow-up save uses it
      // without waiting on the parent's refetch + rerender.
      setCurrentExpectedSha(result.commit_sha);
      onSaved(result.commit_sha);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Save failed.");
    } finally {
      setSaving(false);
    }
  }, [
    saving,
    content,
    commitMessage,
    path,
    currentExpectedSha,
    onSaved,
    writeHumanArticle,
  ]);

  const handleReloadConflict = useCallback(() => {
    if (!conflict) return;
    // Don't clear the conflict here — onSaved triggers a parent refetch which
    // changes initialContent, and the path/initialContent effect resets the
    // conflict back to null. Clearing locally would race with that reset.
    setContent(conflict.current_content);
    // Promote locally so an immediate follow-up save uses the new SHA.
    setCurrentExpectedSha(conflict.current_sha);
    onSaved(conflict.current_sha);
  }, [conflict, onSaved]);

  const showSource = !(previewOn && isMobile) || mobileView === "source";
  const showPreview = previewOn && (!isMobile || mobileView === "preview");

  return {
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
  };
}

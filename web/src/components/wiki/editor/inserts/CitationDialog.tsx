/**
 * Modal form for inserting a footnote-style citation.
 *
 * The user supplies a URL and optional title; the dialog computes the
 * next available footnote id from the current document so identifiers
 * never collide. Submitting calls `onConfirm` with both the inline
 * reference and the block definition; the controller is responsible
 * for inserting the reference at the caret and appending the
 * definition to the document tail.
 */

import { useEffect, useRef, useState } from "react";

import {
  type BuiltCitation,
  buildCitation,
  nextFootnoteId,
} from "./markdownShapes";

export interface CitationDialogProps {
  currentMarkdown: string;
  onConfirm: (built: BuiltCitation) => void;
  onCancel: () => void;
}

export function CitationDialog({
  currentMarkdown,
  onConfirm,
  onCancel,
}: CitationDialogProps): React.ReactElement {
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  const urlRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    urlRef.current?.focus();
  }, []);

  function handleSubmit(e?: React.FormEvent): void {
    e?.preventDefault();
    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      setError("URL is required.");
      return;
    }
    if (!/^https?:\/\//.test(trimmedUrl)) {
      setError("URL must start with http:// or https://.");
      return;
    }
    const id = nextFootnoteId(currentMarkdown);
    const built = buildCitation({ id, title, url: trimmedUrl });
    onConfirm(built);
  }

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-citation-dialog-backdrop"
      role="dialog"
      aria-modal="true"
    >
      <form
        className="wk-modal wk-insert-dialog"
        data-testid="wk-citation-dialog"
        onSubmit={handleSubmit}
      >
        <h2>Cite source</h2>
        <label htmlFor="wk-citation-url" className="wk-editor-label">
          URL
        </label>
        <input
          id="wk-citation-url"
          ref={urlRef}
          type="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://example.com/article"
          data-testid="wk-citation-url"
        />
        <label htmlFor="wk-citation-title" className="wk-editor-label">
          Title (optional)
        </label>
        <input
          id="wk-citation-title"
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Source headline"
          data-testid="wk-citation-title"
        />
        {error ? (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
            data-testid="wk-citation-error"
          >
            {error}
          </div>
        ) : null}
        <div className="wk-insert-dialog__actions">
          <button
            type="button"
            data-testid="wk-citation-confirm"
            className="wk-editor-save"
            onClick={() => handleSubmit()}
          >
            Insert citation
          </button>
          <button
            type="button"
            onClick={onCancel}
            data-testid="wk-citation-cancel"
            className="wk-editor-cancel"
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
}

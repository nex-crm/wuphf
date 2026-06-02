/**
 * Small, token-styled image-URL popover for the Tiptap wiki editor.
 *
 * The `/image` slash action opens this so a user can paste an image URL with
 * optional alt text. Uploads are out of scope for the wiki editor (articles
 * reference assets by URL), so this is a URL + alt form, not a file picker.
 */

import { useEffect, useRef, useState } from "react";

export interface EditorImagePopoverProps {
  /** Viewport-relative top-left corner where the popover should float. */
  position: { top: number; left: number };
  onInsert: (payload: { src: string; alt: string }) => void;
  onCancel: () => void;
}

export function EditorImagePopover({
  position,
  onInsert,
  onCancel,
}: EditorImagePopoverProps): React.ReactElement {
  const [src, setSrc] = useState("");
  const [alt, setAlt] = useState("");
  const [error, setError] = useState<string | null>(null);
  const srcRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    srcRef.current?.focus();
  }, []);

  function handleInsert(): void {
    const trimmed = src.trim();
    if (!trimmed) {
      setError("Image URL is required.");
      return;
    }
    if (!/^https?:\/\//.test(trimmed)) {
      setError("URL must start with http:// or https://.");
      return;
    }
    onInsert({ src: trimmed, alt: alt.trim() });
  }

  return (
    <div
      className="wk-editor-popover"
      data-testid="wk-editor-image-popover"
      role="dialog"
      aria-label="Insert image"
      style={{
        position: "fixed",
        top: `${position.top}px`,
        left: `${position.left}px`,
        zIndex: 60,
      }}
      onMouseDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
    >
      <label htmlFor="wk-editor-image-src" className="wk-editor-label">
        Image URL
      </label>
      <input
        id="wk-editor-image-src"
        ref={srcRef}
        type="url"
        className="wk-editor-popover__input"
        value={src}
        placeholder="https://cdn.example.com/image.png"
        onChange={(e) => setSrc(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            handleInsert();
          }
        }}
        data-testid="wk-editor-image-src"
      />
      <label htmlFor="wk-editor-image-alt" className="wk-editor-label">
        Alt text (optional)
      </label>
      <input
        id="wk-editor-image-alt"
        type="text"
        className="wk-editor-popover__input"
        value={alt}
        placeholder="Describe the image"
        onChange={(e) => setAlt(e.target.value)}
        data-testid="wk-editor-image-alt"
      />
      {error ? (
        <div
          className="wk-editor-banner wk-editor-banner--error"
          role="alert"
          data-testid="wk-editor-image-error"
        >
          {error}
        </div>
      ) : null}
      <div className="wk-editor-popover__actions">
        <button
          type="button"
          className="wk-editor-save"
          onClick={handleInsert}
          data-testid="wk-editor-image-insert"
        >
          Insert image
        </button>
        <button
          type="button"
          className="wk-editor-cancel"
          onClick={onCancel}
          data-testid="wk-editor-image-cancel"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

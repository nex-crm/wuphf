/**
 * Small, token-styled link-entry popover for the Tiptap wiki editor.
 *
 * Replaces cabinet's blocking `window.prompt` link flow (banned by the repo's
 * no-blocking-UI rule) with an inline form. The bubble menu opens it over the
 * current selection; Mod-e (bound in `tiptap/extensions.ts`) routes here too.
 *
 * The popover owns no editor state — it reports a URL via `onApply`, a removal
 * via `onRemove`, or a dismissal via `onCancel`. The editor component restores
 * the captured selection and runs the actual `setLink` / `unsetLink` command.
 */

import { useEffect, useRef, useState } from "react";

export interface EditorLinkPopoverProps {
  /** Viewport-relative top-left corner where the popover should float. */
  position: { top: number; left: number };
  /** Existing href when editing a link; empty when adding a new one. */
  initialUrl?: string;
  onApply: (url: string) => void;
  onRemove?: () => void;
  onCancel: () => void;
}

export function EditorLinkPopover({
  position,
  initialUrl = "",
  onApply,
  onRemove,
  onCancel,
}: EditorLinkPopoverProps): React.ReactElement {
  const [url, setUrl] = useState(initialUrl);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  function handleApply(): void {
    const trimmed = url.trim();
    if (!trimmed) {
      onCancel();
      return;
    }
    onApply(trimmed);
  }

  return (
    <div
      className="wk-editor-popover"
      data-testid="wk-editor-link-popover"
      role="dialog"
      aria-label={initialUrl ? "Edit link" : "Add link"}
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
      <div className="wk-editor-popover__row">
        <input
          ref={inputRef}
          type="url"
          className="wk-editor-popover__input"
          value={url}
          placeholder="https://example.com"
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              handleApply();
            }
          }}
          data-testid="wk-editor-link-input"
        />
      </div>
      <div className="wk-editor-popover__actions">
        <button
          type="button"
          className="wk-editor-save"
          disabled={url.trim().length === 0}
          onClick={handleApply}
          data-testid="wk-editor-link-apply"
        >
          {initialUrl ? "Update" : "Add link"}
        </button>
        {onRemove ? (
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onRemove}
            data-testid="wk-editor-link-remove"
          >
            Remove
          </button>
        ) : null}
        <button
          type="button"
          className="wk-editor-cancel"
          onClick={onCancel}
          data-testid="wk-editor-link-cancel"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

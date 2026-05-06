/**
 * Modal form for the fact / triple insert.
 *
 * Critical contract: this dialog NEVER silently writes a fact to the
 * fact log. The two-step flow is intentional:
 *   1. Editing — user fills out subject / predicate / object plus the
 *      optional confidence field.
 *   2. Preview — same dialog flips to a read-only render of the fact
 *      block exactly as it will land in the document. The confirm
 *      button is the only path that triggers `onConfirm`.
 *
 * The preview is the contract the phase doc requires ("Fact/triple
 * insert never silently writes facts — always shows a reviewable
 * preview"). Closing the dialog at any point before confirm leaves
 * the document untouched.
 */

import { useEffect, useRef, useState } from "react";

import { buildFactBlock, type FactDraft } from "./markdownShapes";

export interface FactDialogProps {
  /** Optional default source citation reference (e.g. `[^1]`) — used
   *  when the user opens the fact dialog after inserting a citation. */
  defaultSource?: string;
  onConfirm: (block: string) => void;
  onCancel: () => void;
}

type Stage = "edit" | "preview";

export function FactDialog({
  defaultSource,
  onConfirm,
  onCancel,
}: FactDialogProps): React.ReactElement {
  const [draft, setDraft] = useState<FactDraft>({
    subject: "",
    predicate: "",
    object: "",
    confidence: 0.9,
    source: defaultSource ?? "",
  });
  const [stage, setStage] = useState<Stage>("edit");
  const [error, setError] = useState<string | null>(null);
  const subjectRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (stage === "edit") subjectRef.current?.focus();
  }, [stage]);

  function handleProceedToPreview(e?: React.FormEvent): void {
    e?.preventDefault();
    if (!draft.subject.trim()) {
      setError("Subject is required.");
      return;
    }
    if (!draft.predicate.trim()) {
      setError("Predicate is required.");
      return;
    }
    if (!draft.object.trim()) {
      setError("Object is required.");
      return;
    }
    setError(null);
    setStage("preview");
  }

  const previewBlock =
    stage === "preview"
      ? buildFactBlock({
          ...draft,
          source: draft.source?.trim() || undefined,
        })
      : "";

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-fact-dialog-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-fact-dialog-title"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
    >
      <form
        className="wk-modal wk-insert-dialog"
        data-testid="wk-fact-dialog"
        onSubmit={handleProceedToPreview}
      >
        <h2 id="wk-fact-dialog-title">
          {stage === "edit" ? "Add fact" : "Review fact"}
        </h2>

        {stage === "edit" ? (
          <>
            <p className="wk-insert-dialog__hint">
              Captures a subject + predicate + object claim. Nothing is written
              until you confirm on the next screen.
            </p>
            <label htmlFor="wk-fact-subject" className="wk-editor-label">
              Subject
            </label>
            <input
              id="wk-fact-subject"
              ref={subjectRef}
              value={draft.subject}
              onChange={(e) => setDraft({ ...draft, subject: e.target.value })}
              data-testid="wk-fact-subject"
            />
            <label htmlFor="wk-fact-predicate" className="wk-editor-label">
              Predicate
            </label>
            <input
              id="wk-fact-predicate"
              value={draft.predicate}
              onChange={(e) =>
                setDraft({ ...draft, predicate: e.target.value })
              }
              data-testid="wk-fact-predicate"
            />
            <label htmlFor="wk-fact-object" className="wk-editor-label">
              Object
            </label>
            <input
              id="wk-fact-object"
              value={draft.object}
              onChange={(e) => setDraft({ ...draft, object: e.target.value })}
              data-testid="wk-fact-object"
            />
            <label htmlFor="wk-fact-confidence" className="wk-editor-label">
              Confidence (0..1)
            </label>
            <input
              id="wk-fact-confidence"
              type="number"
              min={0}
              max={1}
              step={0.1}
              value={draft.confidence ?? ""}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  confidence:
                    e.target.value === ""
                      ? undefined
                      : Number.parseFloat(e.target.value),
                })
              }
              data-testid="wk-fact-confidence"
            />
            <label htmlFor="wk-fact-source" className="wk-editor-label">
              Source (optional)
            </label>
            <input
              id="wk-fact-source"
              value={draft.source ?? ""}
              onChange={(e) => setDraft({ ...draft, source: e.target.value })}
              placeholder="[^1]"
              data-testid="wk-fact-source"
            />
            {error ? (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
                data-testid="wk-fact-error"
              >
                {error}
              </div>
            ) : null}
            <div className="wk-insert-dialog__actions">
              <button
                type="button"
                data-testid="wk-fact-preview"
                className="wk-editor-save"
                onClick={() => handleProceedToPreview()}
              >
                Preview
              </button>
              <button
                type="button"
                onClick={onCancel}
                className="wk-editor-cancel"
                data-testid="wk-fact-cancel"
              >
                Cancel
              </button>
            </div>
          </>
        ) : (
          <>
            <p className="wk-insert-dialog__hint">
              The block below will be inserted at your cursor. Nothing else is
              written until you click Confirm insert.
            </p>
            <pre
              className="wk-insert-dialog__preview"
              data-testid="wk-fact-preview-block"
            >
              {previewBlock}
            </pre>
            <div className="wk-insert-dialog__actions">
              <button
                type="button"
                className="wk-editor-save"
                data-testid="wk-fact-confirm"
                onClick={() => onConfirm(previewBlock)}
              >
                Confirm insert
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                data-testid="wk-fact-back"
                onClick={() => setStage("edit")}
              >
                Back
              </button>
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onCancel}
                data-testid="wk-fact-cancel-2"
              >
                Cancel
              </button>
            </div>
          </>
        )}
      </form>
    </div>
  );
}

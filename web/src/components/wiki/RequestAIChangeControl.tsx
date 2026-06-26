import { useState } from "react";

import { createTasks } from "../../api/tasks";
import { useFocusTrap } from "./editor/inserts/useFocusTrap";

/**
 * RequestAIChangeControl — the article-toolbar "Request changes via AI"
 * affordance. Opens a small modal with one instruction field; submitting
 * creates a task owned by the librarian (Pam) to update the article AND
 * the related items the change affects, then confirms with a link to the
 * created task. Existing wk- tokens only; markup mirrors the article
 * delete control so the parallel wiki-shell restyle can move both at once.
 */

export const LIBRARIAN_SLUG = "librarian";

/** Builds the librarian task payload for a wiki AI-change request. */
export function buildWikiChangeTask(
  title: string,
  path: string,
  instruction: string,
): { title: string; assignee: string; details: string } {
  return {
    title: `Update wiki article: ${title}`,
    assignee: LIBRARIAN_SLUG,
    details:
      `${instruction.trim()}\n\nWiki article: ${path}\n` +
      "Also update related items (linked articles, the index) that this change affects.",
  };
}

interface RequestAIChangeControlProps {
  title: string;
  path: string;
}

export default function RequestAIChangeControl({
  title,
  path,
}: RequestAIChangeControlProps) {
  const [open, setOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createdTaskId, setCreatedTaskId] = useState<string | null>(null);

  const close = () => {
    setOpen(false);
    setInstruction("");
    setError(null);
    setCreatedTaskId(null);
  };

  const submit = async () => {
    if (pending) return;
    const trimmed = instruction.trim();
    if (!trimmed) {
      setError("Say what should change — Pam needs the instruction.");
      return;
    }
    setPending(true);
    setError(null);
    try {
      const res = await createTasks(
        [buildWikiChangeTask(title, path, trimmed)],
        {
          channel: "general",
          createdBy: "human",
        },
      );
      const task = res.tasks?.[0];
      if (!task?.id) {
        throw new Error("Task was not created — the broker returned no task.");
      }
      setCreatedTaskId(task.id);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Task creation failed.");
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <div className="wk-article-toolbar">
        <button
          type="button"
          className="wk-article-delete-btn wk-article-ai-change-btn"
          data-testid="wk-article-ai-change"
          onClick={() => {
            setError(null);
            setOpen(true);
          }}
        >
          Request changes via AI
        </button>
      </div>
      {open ? (
        <RequestAIChangeModal
          title={title}
          path={path}
          instruction={instruction}
          pending={pending}
          error={error}
          createdTaskId={createdTaskId}
          onInstructionChange={(value) => {
            setInstruction(value);
            if (error) setError(null);
          }}
          onCancel={close}
          onSubmit={() => {
            void submit();
          }}
        />
      ) : null}
    </>
  );
}

function RequestAIChangeModal({
  title,
  path,
  instruction,
  pending,
  error,
  createdTaskId,
  onInstructionChange,
  onCancel,
  onSubmit,
}: {
  title: string;
  path: string;
  instruction: string;
  pending: boolean;
  error: string | null;
  createdTaskId: string | null;
  onInstructionChange: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
}) {
  const trapRef = useFocusTrap<HTMLDivElement>();
  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-article-ai-change-title"
      data-testid="wk-article-ai-change-modal"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-article-ai-change-title">Request changes via AI</h2>
        {createdTaskId ? (
          <>
            <p className="wk-editor-help" data-testid="wk-ai-change-confirm">
              Task created — Pam the librarian will update “{title}” and the
              related items this change affects.{" "}
              <a href={`#/tasks/${encodeURIComponent(createdTaskId)}`}>
                Open task {createdTaskId}
              </a>
            </p>
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-save"
                onClick={onCancel}
              >
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <p className="wk-editor-help">
              Tell Pam what should change in <code>{path}</code>. She will
              update this article and the related items (linked articles, the
              index) the change affects.
            </p>
            <label className="wk-editor-help" htmlFor="wk-ai-change-input">
              What should change? (required)
            </label>
            <textarea
              id="wk-ai-change-input"
              data-testid="wk-ai-change-input"
              rows={4}
              value={instruction}
              placeholder="e.g. Add the new renewal terms from the June call and refresh the pricing table."
              onChange={(e) => onInstructionChange(e.target.value)}
            />
            {error ? (
              <div
                className="wk-editor-banner wk-editor-banner--error"
                role="alert"
              >
                {error}
              </div>
            ) : null}
            <div className="wk-editor-actions">
              <button
                type="button"
                className="wk-editor-cancel"
                onClick={onCancel}
                disabled={pending}
              >
                Cancel
              </button>
              <button
                type="button"
                className="wk-editor-save"
                data-testid="wk-ai-change-submit"
                onClick={onSubmit}
                disabled={pending}
              >
                {pending ? "Creating task…" : "Create task for Pam"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { reopenTask } from "../../api/tasks";

// ── Reopen button (rejected / approved Tasks) ────────────────────────

interface ReopenTaskButtonProps {
  taskId: string;
  channel: string;
  onReopened: () => void;
}

export function ReopenTaskButton({
  taskId,
  channel,
  onReopened,
}: ReopenTaskButtonProps) {
  const [error, setError] = useState<string | null>(null);

  const reopenMutation = useMutation({
    mutationFn: () => reopenTask(taskId, channel),
    onSuccess: () => {
      setError(null);
      onReopened();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not reopen.");
    },
  });

  return (
    <div className="issue-doc-reopen">
      <button
        type="button"
        className="issue-doc-reopen-button"
        onClick={() => {
          // Clear the previous error before retrying so the alert text
          // does not linger next to a pending request.
          setError(null);
          reopenMutation.mutate();
        }}
        disabled={reopenMutation.isPending}
        data-testid="reopen-issue-button"
      >
        {reopenMutation.isPending ? "Reopening…" : "Reopen task"}
      </button>
      {error ? (
        <span className="issue-doc-reopen-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

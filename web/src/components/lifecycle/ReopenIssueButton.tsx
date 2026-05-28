import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { reopenIssue } from "../../api/tasks";

// ── Reopen button (rejected / approved Issues) ───────────────────────

interface ReopenIssueButtonProps {
  taskId: string;
  channel: string;
  onReopened: () => void;
}

export function ReopenIssueButton({
  taskId,
  channel,
  onReopened,
}: ReopenIssueButtonProps) {
  const [error, setError] = useState<string | null>(null);

  const reopenMutation = useMutation({
    mutationFn: () => reopenIssue(taskId, channel),
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
        onClick={() => reopenMutation.mutate()}
        disabled={reopenMutation.isPending}
        data-testid="reopen-issue-button"
      >
        {reopenMutation.isPending ? "Reopening…" : "Reopen issue"}
      </button>
      {error ? (
        <span className="issue-doc-reopen-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

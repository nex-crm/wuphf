import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { reassignTask } from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";

// ── Owner picker (Issue header) ──────────────────────────────────────

interface OwnerPickerProps {
  taskId: string;
  channel: string;
  currentOwner: string | undefined;
  onChanged: () => void;
}

export function OwnerPicker({
  taskId,
  channel,
  currentOwner,
  onChanged,
}: OwnerPickerProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { data: members = [] } = useOfficeMembers();
  const assignableAgents = members.filter(
    (m) => m.slug && m.slug !== "human" && m.slug !== "you",
  );

  const reassignMutation = useMutation({
    mutationFn: (newOwner: string) => reassignTask(taskId, newOwner, channel),
    onSuccess: () => {
      setIsEditing(false);
      setError(null);
      onChanged();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not reassign.");
    },
  });

  if (!isEditing) {
    return (
      <button
        type="button"
        className="issue-doc-owner-pill"
        onClick={() => setIsEditing(true)}
        data-testid="issue-owner-pill"
        aria-label="Change owner"
      >
        <span className="issue-doc-owner-pill-label">Owner</span>
        <span className="issue-doc-owner-pill-value">
          {currentOwner ? `@${currentOwner}` : "unassigned"}
        </span>
        <span className="issue-doc-owner-pill-edit" aria-hidden="true">
          ✎
        </span>
      </button>
    );
  }

  return (
    <div className="issue-doc-owner-editor">
      <label className="issue-doc-owner-editor-label" htmlFor="owner-select">
        Owner
      </label>
      <select
        id="owner-select"
        className="issue-doc-owner-editor-select"
        defaultValue={currentOwner ?? ""}
        onChange={(event) => {
          const next = event.target.value;
          if (next === (currentOwner ?? "")) {
            setIsEditing(false);
            return;
          }
          // Clear any stale error from a prior attempt so the alert
          // banner does not linger while the new request is pending.
          setError(null);
          reassignMutation.mutate(next);
        }}
        disabled={reassignMutation.isPending}
        autoFocus
        data-testid="issue-owner-select"
      >
        <option value="">— unassigned —</option>
        {assignableAgents.map((agent) => (
          <option key={agent.slug} value={agent.slug}>
            @{agent.slug}
            {agent.name && agent.name !== agent.slug
              ? ` (${agent.name})`
              : ""}
          </option>
        ))}
      </select>
      <button
        type="button"
        className="issue-doc-owner-editor-cancel"
        onClick={() => {
          setIsEditing(false);
          setError(null);
        }}
        disabled={reassignMutation.isPending}
      >
        Cancel
      </button>
      {error ? (
        <span className="issue-doc-owner-editor-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

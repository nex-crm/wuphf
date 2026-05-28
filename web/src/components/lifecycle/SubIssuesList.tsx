import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createSubIssue,
  getSubIssues,
} from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";
import { router } from "../../lib/router";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { formatIssueTitleForDisplay } from "../../lib/issueTitle";
import { IssueStatusDot } from "./IssueActivityStream";

interface SubIssuesListProps {
  taskId: string;
  channel: string;
}

export function SubIssuesList({ taskId, channel }: SubIssuesListProps) {
  const queryClient = useQueryClient();
  const [isAdding, setIsAdding] = useState(false);
  const [draftTitle, setDraftTitle] = useState("");
  const [draftOwner, setDraftOwner] = useState("");
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const { data: members = [] } = useOfficeMembers();

  const childQuery = useQuery({
    queryKey: ["issue-children", taskId],
    queryFn: () => getSubIssues(taskId),
    staleTime: 5_000,
  });

  const addMutation = useMutation({
    mutationFn: (input: { title: string; owner: string }) =>
      createSubIssue({
        parentIssueId: taskId,
        title: input.title,
        channel,
        owner: input.owner || undefined,
      }),
    onSuccess: () => {
      setDraftTitle("");
      setDraftOwner("");
      setIsAdding(false);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["issue-children", taskId] });
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not add sub-issue.");
    },
  });

  useEffect(() => {
    if (isAdding) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isAdding]);

  const children = childQuery.data?.tasks ?? [];
  // Sub-issues + Issues should share owner-pick UX. Exclude `human` and
  // self-loop entries that aren't real agent slugs.
  const assignableAgents = members.filter(
    (m) => m.slug && m.slug !== "human" && m.slug !== "you",
  );

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const title = draftTitle.trim();
    if (!title || addMutation.isPending) return;
    addMutation.mutate({ title, owner: draftOwner.trim() });
  }

  function openSub(childId: string) {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: childId },
    });
  }

  return (
    <section
      className="issue-doc-sub-issues"
      aria-label="Sub-issues"
      data-testid="issue-sub-issues"
    >
      <header className="issue-doc-sub-issues-header">
        <h3 className="issue-doc-sub-issues-heading">
          Sub-issues
          {children.length > 0 ? (
            <span className="issue-doc-sub-issues-count">
              {" "}
              · {children.length}
            </span>
          ) : null}
        </h3>
        {!isAdding ? (
          <button
            type="button"
            className="issue-doc-sub-issues-add"
            onClick={() => setIsAdding(true)}
            data-testid="add-sub-issue-button"
          >
            + Add sub-issue
          </button>
        ) : null}
      </header>

      {children.length === 0 && !isAdding ? (
        <p className="issue-doc-sub-issues-empty">
          No sub-issues. Break this down with the + button above.
        </p>
      ) : null}

      {children.length > 0 ? (
        <ul className="issue-doc-sub-issues-list">
          {children.map((child) => {
            const lifecycle = (child.lifecycle_state ||
              child.status ||
              "drafting") as LifecycleState;
            return (
              <li key={child.id} className="issue-doc-sub-issue-row">
                <button
                  type="button"
                  className="issue-doc-sub-issue-link"
                  onClick={() => openSub(child.id)}
                  data-testid="sub-issue-link"
                  data-task-id={child.id}
                >
                  <IssueStatusDot lifecycleState={lifecycle} />
                  <span className="issue-doc-sub-issue-id">{child.id}</span>
                  <span className="issue-doc-sub-issue-title">
                    {formatIssueTitleForDisplay(child.title) || "(untitled)"}
                  </span>
                  <span className="issue-doc-sub-issue-state">
                    {child.lifecycle_state || child.status}
                  </span>
                  {child.owner ? (
                    <span className="issue-doc-sub-issue-owner">
                      @{child.owner}
                    </span>
                  ) : null}
                </button>
              </li>
            );
          })}
        </ul>
      ) : null}

      {isAdding ? (
        <form
          className="issue-doc-sub-issues-form"
          onSubmit={handleSubmit}
          data-testid="add-sub-issue-form"
        >
          <input
            ref={inputRef}
            className="issue-doc-sub-issues-input"
            value={draftTitle}
            onChange={(event) => {
              setDraftTitle(event.target.value);
              if (error) setError(null);
            }}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                setIsAdding(false);
                setDraftTitle("");
                setDraftOwner("");
              }
            }}
            placeholder="Sub-issue title (Enter to add, Esc to cancel)"
            disabled={addMutation.isPending}
            data-testid="sub-issue-title-input"
          />
          <label className="issue-doc-sub-issues-owner-label">
            Owner
            <select
              className="issue-doc-sub-issues-owner-select"
              value={draftOwner}
              onChange={(event) => setDraftOwner(event.target.value)}
              disabled={addMutation.isPending}
              data-testid="sub-issue-owner-select"
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
          </label>
          <div className="issue-doc-sub-issues-form-actions">
            <button
              type="submit"
              className="issue-doc-sub-issues-submit"
              disabled={!draftTitle.trim() || addMutation.isPending}
            >
              {addMutation.isPending ? "Adding…" : "Add"}
            </button>
            <button
              type="button"
              className="issue-doc-sub-issues-cancel"
              onClick={() => {
                setIsAdding(false);
                setDraftTitle("");
                setDraftOwner("");
                setError(null);
              }}
              disabled={addMutation.isPending}
            >
              Cancel
            </button>
          </div>
          {error ? (
            <p className="issue-doc-sub-issues-error" role="alert">
              {error}
            </p>
          ) : null}
        </form>
      ) : null}
    </section>
  );
}

import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks } from "../../api/tasks";
import { router } from "../../lib/router";

// ── Parent issue breadcrumb (shown on sub-issues) ────────────────────

interface ParentIssueBreadcrumbProps {
  parentIssueId: string;
}

export function ParentIssueBreadcrumb({ parentIssueId }: ParentIssueBreadcrumbProps) {
  // Fetch the parent's title so the breadcrumb shows it inline. The
  // /tasks list is already cached for the kanban; we read the same
  // cache key (["issues","list"]) for a free hit when available, and
  // fall back to the task id when the parent is filtered out (e.g.
  // it lives in a channel the viewer can't see).
  const tasksQuery = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    staleTime: 5_000,
  });
  const parent = tasksQuery.data?.tasks.find((t) => t.id === parentIssueId);
  const label = parent?.title ?? parentIssueId;

  function openParent() {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: parentIssueId },
    });
  }

  return (
    <button
      type="button"
      className="issue-doc-parent-breadcrumb"
      onClick={openParent}
      data-testid="issue-parent-breadcrumb"
      aria-label={`Open parent issue ${parentIssueId}`}
    >
      <span className="issue-doc-parent-breadcrumb-icon" aria-hidden="true">
        ↑
      </span>
      <span className="issue-doc-parent-breadcrumb-label">Parent</span>
      <span className="issue-doc-parent-breadcrumb-id">{parentIssueId}</span>
      <span className="issue-doc-parent-breadcrumb-title">{label}</span>
    </button>
  );
}

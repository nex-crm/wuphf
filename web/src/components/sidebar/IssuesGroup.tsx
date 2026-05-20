/**
 * IssuesGroup — sidebar Issues section for Phase 3.
 *
 * Renders a collapsible group header with "+ New issue" affordance plus a
 * list of open issues when expanded. The Issues group sits between
 * Channels and Tools per the spec Surface 2 layout.
 *
 * Design decisions:
 * - Extends the existing SectionToggle + sidebar-item pattern rather than
 *   building a new primitive — consistent with ChannelList and AgentList.
 * - "+ New issue" wires to /issues/new (Phase 4 stub that 501s for now).
 * - Issue rows navigate to /issues/$issueId.
 * - Empty state: a faint placeholder label in --text-tertiary so the user
 *   understands what the section IS, per spec Surface 2: "Issues sidebar
 *   groups render as empty states with a faint placeholder, not hidden."
 *
 * No sub-issues, no filters, no wiki mirror. Phase 6 scope.
 */

import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { SidebarItem } from "./SidebarItem";

// ── Helpers ────────────────────────────────────────────────────────────

const KNOWN_LIFECYCLE_STATES: ReadonlySet<LifecycleState> =
  new Set<LifecycleState>([
    "drafting",
    "intake",
    "ready",
    "running",
    "review",
    "decision",
    "blocked_on_pr_merge",
    "changes_requested",
    "approved",
    "rejected",
  ]);

function taskToState(task: Task): LifecycleState {
  if (task.pipeline_stage === "draft") return "drafting";
  const raw = task.lifecycle_state;
  if (
    typeof raw === "string" &&
    KNOWN_LIFECYCLE_STATES.has(raw as LifecycleState)
  ) {
    return raw as LifecycleState;
  }
  switch (task.status) {
    case "open":
      return "intake";
    case "in_progress":
      return "running";
    case "done":
      return "approved";
    case "blocked":
      return "blocked_on_pr_merge";
    case "review":
      return "review";
    default:
      return "intake";
  }
}

const TERMINAL_STATES: ReadonlySet<LifecycleState> = new Set([
  "approved",
  "rejected",
]);

function isOpenIssue(task: Task): boolean {
  return !TERMINAL_STATES.has(taskToState(task));
}

// ── Main component ─────────────────────────────────────────────────────

interface IssuesGroupProps {
  open: boolean;
}

/**
 * Renders the Issues sidebar list body. The section header lives in
 * Sidebar.tsx alongside the other section headers so all four sections
 * (Agents, Channels, Issues, Tools) share the same structure.
 */
export function IssuesGroup({ open }: IssuesGroupProps) {
  const route = useCurrentRoute();
  const activeIssueId = route.kind === "issue-detail" ? route.issueId : null;

  const query = useQuery({
    queryKey: ["issues", "sidebar"],
    queryFn: () => getOfficeTasks({ includeDone: false }),
    staleTime: 10_000,
    // Only fetch once the section is opened to avoid extra requests on
    // first paint. Pre-warm on second mount once the user has seen it.
    enabled: open,
  });

  const openIssues = (query.data?.tasks ?? []).filter(isOpenIssue);

  // Always render the body — the parent .sidebar-collapsible owns the
  // open/close animation via grid-template-rows + overflow:hidden. If we
  // returned null when closed, the collapse would snap instantly because
  // there'd be no content to animate from.
  return (
    <div className="sidebar-scroll-wrap is-issues">
      <div className="sidebar-issues" data-testid="issues-group-list">
        {openIssues.length === 0 ? (
          <p
            className="sidebar-empty-hint"
            style={{
              color: "var(--text-tertiary)",
              padding: "4px 10px",
              fontSize: 12,
            }}
            data-testid="issues-sidebar-empty"
          >
            No issues yet.
          </p>
        ) : (
          openIssues.slice(0, 20).map((task) => (
            <SidebarItem
              key={task.id}
              icon="#"
              label={task.title}
              active={activeIssueId === task.id}
              onClick={() =>
                void router.navigate({
                  to: "/issues/$issueId",
                  params: { issueId: task.id },
                })
              }
              aria-label={task.title}
              title={task.title}
              data-testid="issues-sidebar-row"
            />
          ))
        )}
        <SidebarItem
          variant="add"
          icon="+"
          label="New issue"
          onClick={() => void router.navigate({ to: "/issues/new" })}
          title="New issue"
          data-testid="issues-sidebar-new-btn"
        />
      </div>
    </div>
  );
}

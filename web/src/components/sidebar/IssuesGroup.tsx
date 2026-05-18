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
import { SidebarItemLabel } from "./SidebarItemLabel";

// ── Helpers ────────────────────────────────────────────────────────────

function taskToState(task: Task): LifecycleState {
  if (task.pipeline_stage === "draft") return "drafting";
  const raw = (task as unknown as Record<string, unknown>).lifecycle_state;
  if (typeof raw === "string" && raw) return raw as LifecycleState;
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

// ── Sub-components ─────────────────────────────────────────────────────

function SectionToggle({
  label,
  open,
  onToggle,
}: {
  label: string;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className="sidebar-section-title sidebar-section-toggle"
      onClick={onToggle}
      aria-expanded={open}
    >
      <span>{label}</span>
      <svg
        aria-hidden="true"
        focusable="false"
        style={{
          width: 10,
          height: 10,
          transform: open ? "rotate(90deg)" : "rotate(0deg)",
          transition: "transform 0.15s",
        }}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="m9 18 6-6-6-6" />
      </svg>
    </button>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface IssuesGroupProps {
  open: boolean;
  onToggle: () => void;
}

/**
 * Renders the Issues sidebar section header + optional issue list.
 *
 * The header row always renders (toggle + New issue button). The list
 * only renders when `open === true`. Using this pattern instead of the
 * separate sidebar-collapsible div in Sidebar.tsx allows the "+New" button
 * to remain visible even when collapsed — same UX as ChannelList's "+ New
 * Channel" button inside the scroll area.
 */
export function IssuesGroup({ open, onToggle }: IssuesGroupProps) {
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

  return (
    <>
      {/* Section header — always visible */}
      <div
        className="sidebar-section-title-row issues-group-header"
        data-testid="issues-group-header"
      >
        <SectionToggle label="Issues" open={open} onToggle={onToggle} />
        <button
          type="button"
          className="sidebar-icon-btn issues-new-icon-btn"
          title="New issue"
          aria-label="New issue"
          onClick={() => void router.navigate({ to: "/issues/new" })}
          data-testid="issues-sidebar-new-btn"
        >
          +
        </button>
      </div>

      {/* Collapsible list */}
      {open && (
        <div
          className="sidebar-collapsible is-open is-issues"
          data-testid="issues-group-list"
        >
          {openIssues.length === 0 ? (
            <p
              className="sidebar-empty-hint"
              style={{
                color: "var(--text-tertiary)",
                padding: "4px 12px",
                fontSize: 12,
              }}
              data-testid="issues-sidebar-empty"
            >
              No issues yet.
            </p>
          ) : (
            openIssues.slice(0, 20).map((task) => (
              <button
                key={task.id}
                type="button"
                className={`sidebar-item${activeIssueId === task.id ? " active" : ""}`}
                onClick={() =>
                  void router.navigate({
                    to: "/issues/$issueId",
                    params: { issueId: task.id },
                  })
                }
                aria-label={task.title}
                title={task.title}
                data-testid="issues-sidebar-row"
              >
                <span
                  style={{
                    color: "currentColor",
                    width: 18,
                    textAlign: "center",
                    flexShrink: 0,
                    display: "inline-block",
                    fontSize: 11,
                  }}
                >
                  #
                </span>
                <SidebarItemLabel>{task.title}</SidebarItemLabel>
              </button>
            ))
          )}
          {/* View all issues link */}
          <button
            type="button"
            className={`sidebar-item sidebar-add-btn${route.kind === "issues-list" ? " active" : ""}`}
            onClick={() => void router.navigate({ to: "/issues" })}
            title="View all issues"
            data-testid="issues-sidebar-view-all"
          >
            <span
              style={{
                width: 18,
                textAlign: "center",
                flexShrink: 0,
                display: "inline-block",
              }}
            />
            <span style={{ color: "var(--text-tertiary)" }}>View all</span>
          </button>
        </div>
      )}
    </>
  );
}

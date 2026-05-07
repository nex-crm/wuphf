import type { Task } from "../../../api/tasks";
import { normalizeStatus, taskMeta } from "../../../lib/officeStatus";

interface ActiveTasksPanelProps {
  tasks: Task[];
  /** Badge CSS class for the status pill. Defaults to accent. */
  badgeClass?: string;
  /** Max tasks to render. Defaults to 10. */
  limit?: number;
  /** Called when a task card is clicked. */
  onTaskClick?: (taskId: string) => void;
  emptyLabel?: string;
}

/**
 * Renders a compact list of task cards — status badge, title, description
 * excerpt, and channel/owner/time meta. Used by OfficeOverviewApp (active
 * and blocked sections) and ArtifactsApp (active lanes).
 */
export function ActiveTasksPanel({
  tasks,
  badgeClass = "badge badge-accent",
  limit = 10,
  onTaskClick,
  emptyLabel = "No tasks right now.",
}: ActiveTasksPanelProps) {
  if (tasks.length === 0) {
    return (
      <div
        style={{
          padding: "20px 0",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 13,
        }}
      >
        {emptyLabel}
      </div>
    );
  }

  return (
    <>
      {tasks.slice(0, limit).map((t) => {
        const status = normalizeStatus(t.status);
        const meta = taskMeta(t);
        const clickable = Boolean(onTaskClick);

        function handleKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
          if (!onTaskClick) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onTaskClick(t.id);
          }
        }

        return (
          <div
            key={t.id}
            className="app-card"
            style={{ marginBottom: 6, cursor: clickable ? "pointer" : "default" }}
            onClick={onTaskClick ? () => onTaskClick(t.id) : undefined}
            onKeyDown={clickable ? handleKeyDown : undefined}
            role={clickable ? "button" : undefined}
            tabIndex={clickable ? 0 : undefined}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 6,
                marginBottom: t.description || meta ? 4 : 0,
              }}
            >
              <span className={badgeClass}>{status.replace(/_/g, " ")}</span>
              <span
                style={{
                  fontWeight: 600,
                  fontSize: 13,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {t.title || t.id}
              </span>
            </div>
            {t.description ? (
              <div
                style={{
                  fontSize: 12,
                  color: "var(--text-secondary)",
                  marginBottom: meta ? 4 : 0,
                  lineHeight: 1.45,
                }}
              >
                {t.description.slice(0, 100)}
              </div>
            ) : null}
            {meta ? <div className="app-card-meta">{meta}</div> : null}
          </div>
        );
      })}
    </>
  );
}

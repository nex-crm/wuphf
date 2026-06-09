/**
 * TaskDocumentRoute — /tasks/$taskId route container.
 *
 * Thin wrapper that passes the taskId URL param to TaskDocument.
 * Route-level error boundary catches fetch failures that escape the
 * component's own error state.
 */

import { TaskDocument } from "./TaskDocument";

interface TaskDocumentRouteProps {
  taskId: string;
}

export function TaskDocumentRoute({ taskId }: TaskDocumentRouteProps) {
  return (
    <div
      className="app-panel active issue-document-panel"
      data-testid="issue-document-panel"
    >
      <TaskDocument taskId={taskId} />
    </div>
  );
}

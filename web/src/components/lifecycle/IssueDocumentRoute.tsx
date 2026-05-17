/**
 * IssueDocumentRoute — /issues/$issueId route container.
 *
 * Thin wrapper that passes the issueId URL param to IssueDocument.
 * Route-level error boundary catches fetch failures that escape the
 * component's own error state. Phase 3 is read-only.
 */

import { IssueDocument } from "./IssueDocument";

interface IssueDocumentRouteProps {
  issueId: string;
}

export function IssueDocumentRoute({ issueId }: IssueDocumentRouteProps) {
  return (
    <div
      className="app-panel active issue-document-panel"
      data-testid="issue-document-panel"
    >
      <IssueDocument taskId={issueId} />
    </div>
  );
}

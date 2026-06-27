// Human approval card — the shared gate for external mutations (CQ1).
//
// Both at build-explore time and at execution, any action that mutates an
// outside system (posting to Slack, writing to the CRM) surfaces this same
// card before it runs. Mock/presentational: the buttons just resolve locally.

import { Check, X } from "lucide-react";

interface ApprovalCardProps {
  title: string;
  detail: string;
  integration: string;
  onApprove: () => void;
  onSkip: () => void;
}

export function ApprovalCard({
  title,
  detail,
  integration,
  onApprove,
  onSkip,
}: ApprovalCardProps) {
  return (
    <div className="opr-approval" role="group" aria-label="Approval needed">
      <span className="opr-approval-icon" aria-hidden>
        !
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="opr-eyebrow">Approve · {integration}</div>
        <div style={{ fontWeight: 600, margin: "2px 0 1px" }}>{title}</div>
        <div className="opr-step-detail">{detail}</div>
        <div className="opr-approval-actions">
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={onApprove}
          >
            <Check size={13} strokeWidth={1.9} aria-hidden />
            Approve &amp; send
          </button>
          <button
            type="button"
            className="opr-btn opr-btn-sm"
            onClick={onSkip}
          >
            <X size={13} strokeWidth={1.9} aria-hidden />
            Skip
          </button>
        </div>
      </div>
    </div>
  );
}

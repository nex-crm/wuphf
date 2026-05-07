import {
  type ApprovalContext,
  approvalContextIsEmpty,
} from "../../lib/parseApprovalContext";

interface ApprovalContextViewProps {
  parsed: ApprovalContext;
}

export function ApprovalContextView({ parsed }: ApprovalContextViewProps) {
  const empty = approvalContextIsEmpty(parsed);

  return (
    <div className="approval-context">
      {parsed.why ? <p className="approval-why">{parsed.why}</p> : null}

      {parsed.details.length > 0 ? (
        <dl className="approval-details">
          {parsed.details.map((detail) => (
            <div key={detail.label} className="approval-details-row">
              <dt className="approval-details-label">{detail.label}</dt>
              <dd className="approval-details-value">
                <span
                  className={isMonoLabel(detail.label) ? "mono" : undefined}
                >
                  {detail.value}
                </span>
                {detail.truncated ? (
                  <span className="approval-truncated">(truncated)</span>
                ) : null}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}

      {empty ? (
        <p className="approval-empty">
          No additional details available. Approve based on the action ID below.
        </p>
      ) : null}

      <div className="approval-footer">
        {parsed.footer.action ? (
          <span className="approval-footer-row">
            Action: <span className="mono">{parsed.footer.action}</span>
          </span>
        ) : null}
        {parsed.footer.account ? (
          <span className="approval-footer-row">
            Account: <span className="mono">{parsed.footer.account}</span>
          </span>
        ) : null}
        {parsed.footer.channel ? (
          <span className="approval-footer-row">
            Channel: {parsed.footer.channel}
          </span>
        ) : null}
      </div>
    </div>
  );
}

const monoLabels = new Set([
  "To",
  "From",
  "CC",
  "BCC",
  "Email",
  "URL",
  "Event",
]);

function isMonoLabel(label: string): boolean {
  return monoLabels.has(label);
}

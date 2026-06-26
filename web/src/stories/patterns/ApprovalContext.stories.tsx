import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Patterns/Approval context",
  parameters: { layout: "padded" },
};

export default meta;

export const Default: StoryObj = {
  render: () => (
    <div className="approval-context" style={{ maxWidth: 520 }}>
      <p className="approval-why">
        Atlas wants to merge <code>fix/auth-token-rotation</code>. CI passed;
        2/2 reviewers approved.
      </p>
      <dl className="approval-details">
        <div className="approval-details-row">
          <dt className="approval-details-label">Repo</dt>
          <dd className="approval-details-value">
            <span className="mono">nex-crm/wuphf</span>
          </dd>
        </div>
        <div className="approval-details-row">
          <dt className="approval-details-label">Branch</dt>
          <dd className="approval-details-value">
            <span className="mono">fix/auth-token-rotation</span>
          </dd>
        </div>
        <div className="approval-details-row">
          <dt className="approval-details-label">Files</dt>
          <dd className="approval-details-value">
            12 files · +312 / −84
          </dd>
        </div>
      </dl>
      <div className="approval-footer">
        <span className="approval-footer-row">
          PR <span className="mono">#1208</span>
        </span>
        <span className="approval-footer-row">
          Last activity <span className="mono">14m ago</span>
        </span>
      </div>
    </div>
  ),
};

export const WithTruncation: StoryObj = {
  render: () => (
    <div className="approval-context" style={{ maxWidth: 520 }}>
      <p className="approval-why">
        Long payload — values truncated for the inline view.
      </p>
      <dl className="approval-details">
        <div className="approval-details-row">
          <dt className="approval-details-label">Token</dt>
          <dd className="approval-details-value">
            <span className="mono">sk-proj-abc…xyz</span>
            <span className="approval-truncated">truncated</span>
          </dd>
        </div>
        <div className="approval-details-row">
          <dt className="approval-details-label">Trace</dt>
          <dd className="approval-details-value">
            <span className="mono">61ab…7f2c</span>
            <span className="approval-truncated">truncated</span>
          </dd>
        </div>
      </dl>
    </div>
  ),
};

export const Empty: StoryObj = {
  render: () => (
    <div className="approval-context" style={{ maxWidth: 520 }}>
      <p className="approval-empty">No structured context — see the message thread.</p>
    </div>
  ),
};

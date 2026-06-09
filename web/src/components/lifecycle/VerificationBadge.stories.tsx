import type { Meta, StoryObj } from "@storybook/react-vite";

import type { TaskVerification, TaskVerificationResult } from "../../api/tasks";
import { VerificationBadge } from "./VerificationBadge";

const meta: Meta<typeof VerificationBadge> = {
  title: "Lifecycle/VerificationBadge",
  component: VerificationBadge,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof VerificationBadge>;

const COMMAND_CHECK: TaskVerification = {
  kind: "command",
  spec: "bash scripts/test-go.sh ./internal/team",
  required: true,
};

const PASS_RESULT: TaskVerificationResult = {
  pass: true,
  kind: "command",
  detail: "ok  \twuphf/internal/team\t41.20s\nPASS",
  checked_at: "2026-06-10T09:14:00Z",
};

const FAIL_RESULT: TaskVerificationResult = {
  pass: false,
  kind: "command",
  detail:
    "--- FAIL: TestTaskVerificationGate (0.02s)\n    task_verification_test.go:157: packet must carry the verification spec\nFAIL\nexit status 1",
  checked_at: "2026-06-10T09:14:00Z",
};

/** Last check ran and passed — click the badge to see the proof. */
export const Verified: Story = {
  args: { verification: COMMAND_CHECK, result: PASS_RESULT },
};

/** Last check ran and failed — the failure output is one click away. */
export const CheckFailing: Story = {
  args: { verification: COMMAND_CHECK, result: FAIL_RESULT },
};

/** A check is defined but has not run yet. */
export const CheckPending: Story = {
  args: { verification: COMMAND_CHECK },
};

/** No machine check attached — muted, honest, not alarming. */
export const Unverified: Story = {
  args: {},
};

/** Kind "none" is an explicit no-check and renders as Unverified (U1.1). */
export const KindNone: Story = {
  args: { verification: { kind: "none" } },
};

/** All four states side by side, as they appear next to the state pill. */
export const AllStates: Story = {
  render: () => (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
      <VerificationBadge verification={COMMAND_CHECK} result={PASS_RESULT} />
      <VerificationBadge verification={COMMAND_CHECK} result={FAIL_RESULT} />
      <VerificationBadge verification={COMMAND_CHECK} />
      <VerificationBadge />
    </div>
  ),
};

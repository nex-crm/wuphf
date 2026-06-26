import type { Meta, StoryObj } from "@storybook/react-vite";

import type { TaskDefinition } from "../../api/tasks";
import { TaskDefinitionView } from "./TaskDefinitionView";

const meta: Meta<typeof TaskDefinitionView> = {
  title: "Lifecycle/TaskDefinitionView",
  component: TaskDefinitionView,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 320 }}>
        <Story />
      </div>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof TaskDefinitionView>;

const FULL_DEFINITION: TaskDefinition = {
  goal: "Ship the first partner newsletter to the approved partner list this week",
  deliverables: [
    { name: "newsletter draft", format: "markdown in the wiki" },
    { name: "send report", format: "CSV" },
  ],
  success_criteria: [
    "Draft approved by the human before sending",
    "newsletter.md exists in the task worktree",
    "Send report shows zero bounces from the partner list",
  ],
  access_needed: ["mailing-list account", "partner CRM read access"],
  defined_at: "2026-06-10T09:14:00Z",
};

/** The full intake contract: goal, deliverables with format chips,
 *  checklist-style success criteria, and access chips. */
export const FullContract: Story = {
  args: { definition: FULL_DEFINITION },
};

/** Minimal definition — only the required goal was set. */
export const GoalOnly: Story = {
  args: {
    definition: { goal: "Stabilize the flaky auth test before the release" },
  },
};

/** No access requirements; a deliverable without a stated format renders
 *  without a chip. */
export const NoAccessNeeded: Story = {
  args: {
    definition: {
      goal: "Compare competitor pricing tiers for the Q3 pricing call",
      deliverables: [{ name: "pricing comparison brief" }],
      success_criteria: ["Brief covers the top 3 competitors"],
    },
  },
};

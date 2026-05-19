import type { Meta, StoryObj } from "@storybook/react-vite";

import type { NotebookEntryStatus } from "../../api/notebook";

import ByLineStrip from "./ByLineStrip";

const meta: Meta<typeof ByLineStrip> = {
  title: "Features/Notebook/ByLineStrip",
  component: ByLineStrip,
  parameters: { layout: "padded" },
  args: {
    authorSlug: "atlas",
    status: "draft" satisfies NotebookEntryStatus,
    lastEditedTs: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
    revisions: 3,
  },
  argTypes: {
    status: {
      control: "select",
      options: [
        "draft",
        "in-review",
        "changes-requested",
        "promoted",
        "discarded",
      ] satisfies NotebookEntryStatus[],
    },
  },
};

export default meta;
type Story = StoryObj<typeof ByLineStrip>;

export const Draft: Story = {};

export const InReview: Story = {
  args: { status: "in-review", reviewerSlug: "lina" },
};

export const ChangesRequested: Story = {
  args: { status: "changes-requested", reviewerSlug: "lina", revisions: 5 },
};

export const Promoted: Story = {
  args: { status: "promoted", revisions: 7 },
};

export const Discarded: Story = {
  args: { status: "discarded" },
};

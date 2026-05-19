import type { Meta, StoryObj } from "@storybook/react-vite";

import Infobox from "./Infobox";

const meta: Meta<typeof Infobox> = {
  title: "Features/Wiki/Infobox",
  component: Infobox,
  parameters: { layout: "padded" },
};

export default meta;
type Story = StoryObj<typeof Infobox>;

export const Person: Story = {
  args: {
    title: "Lina Park",
    fields: [
      { dt: "Role", dd: "Engineering lead" },
      { dt: "Team", dd: "Storage" },
      { dt: "Tenure", dd: "3.2 years" },
      { dt: "Pronouns", dd: "she / her" },
    ],
  },
};

export const Project: Story = {
  args: {
    title: "Sharded sessions migration",
    fields: [
      { dt: "Status", dd: "In progress" },
      { dt: "Started", dd: "2026-04-12" },
      { dt: "Owner", dd: "Atlas" },
    ],
    sections: [
      {
        fields: [
          { dt: "Spec", dd: "docs/specs/sharded-sessions.md" },
          { dt: "RFC", dd: "RFC-014" },
        ],
      },
      {
        fields: [
          { dt: "Risk", dd: "Medium — touches the auth path" },
          { dt: "Rollback", dd: "Feature-flagged" },
        ],
      },
    ],
  },
};

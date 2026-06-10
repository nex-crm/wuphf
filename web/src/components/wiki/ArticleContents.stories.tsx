import type { Meta, StoryObj } from "@storybook/react-vite";

import "../../styles/wiki.css";
import ArticleContents from "./ArticleContents";

/** Sticky left "Contents" panel with numbered H2/H3 entries (Vector 2022). */
const meta: Meta<typeof ArticleContents> = {
  title: "Features/Wiki/ArticleContents",
  component: ArticleContents,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div className="wiki-root" style={{ padding: 24, maxWidth: 260 }}>
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ArticleContents>;

export const EntityArticleSections: Story = {
  args: {
    entries: [
      { level: 1, num: "1", anchor: "work-history", title: "Work history" },
      { level: 1, num: "2", anchor: "observations", title: "Observations" },
      {
        level: 2,
        num: "2.1",
        anchor: "billing-preferences",
        title: "Billing preferences",
      },
      { level: 1, num: "3", anchor: "associated", title: "Associated" },
      { level: 1, num: "4", anchor: "references", title: "References" },
    ],
  },
};

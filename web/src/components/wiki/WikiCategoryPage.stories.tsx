import type { Meta, StoryObj } from "@storybook/react-vite";

import "../../styles/wiki.css";
import type { WikiCatalogEntry } from "../../api/wiki";
import WikiCategoryPage from "./WikiCategoryPage";

const CATALOG: WikiCatalogEntry[] = [
  ...["Ana", "Arturo", "Eng", "Elena", "Nazz", "Zoe"].map((name) => ({
    path: `team/people/${name.toLowerCase()}.md`,
    title: name,
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T12:00:00Z",
    group: "people",
  })),
  {
    path: "team/companies/acme-corp.md",
    title: "Acme Corp",
    author_slug: "eng",
    last_edited_ts: "2026-06-10T12:00:00Z",
    group: "companies",
  },
];

/** Auto-generated alphabetical category index (Wikipedia category page). */
const meta: Meta<typeof WikiCategoryPage> = {
  title: "Features/Wiki/WikiCategoryPage",
  component: WikiCategoryPage,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <div className="wiki-root" style={{ minHeight: "100vh" }}>
        <Story />
      </div>
    ),
  ],
  args: {
    catalog: CATALOG,
    onNavigate: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof WikiCategoryPage>;

export const People: Story = {
  args: { slug: "people" },
};

export const EmptyCategory: Story = {
  args: { slug: "decisions" },
};

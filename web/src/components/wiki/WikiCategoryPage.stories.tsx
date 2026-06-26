import type { Meta, StoryObj } from "@storybook/react-vite";

import "../../styles/wiki.css";
import type { DiscoveredCategory, WikiCatalogEntry } from "../../api/wiki";
import WikiCategoryPage from "./WikiCategoryPage";

// The subcategory tree: People is a child of the Org category and the parent
// of Engineering. Drives the "Part of:" + "Subcategories" sections.
const CATEGORIES: DiscoveredCategory[] = [
  { slug: "org", title: "Org", article_count: 0, parents: [] },
  { slug: "people", title: "People", article_count: 7, parents: ["org"] },
  {
    slug: "engineering",
    title: "Engineering",
    article_count: 0,
    parents: ["people"],
  },
];

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
  // A playbook filed into People via its `categories:` frontmatter — appears
  // in the People category page even though it lives in a different folder.
  {
    path: "team/playbooks/hiring-loop.md",
    title: "Hiring Loop",
    author_slug: "ceo",
    last_edited_ts: "2026-06-11T12:00:00Z",
    group: "playbooks",
    categories: ["people"],
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
    categories: CATEGORIES,
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

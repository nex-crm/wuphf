import type { Meta, StoryObj } from "@storybook/react-vite";

import "../../styles/wiki.css";

import { ENTITY_ARTICLE_FIXTURE } from "./__fixtures__/entityArticleFixture";
import ArticleReadView from "./ArticleReadView";
import { makeWikilinkResolver } from "./articleContent";

/**
 * The Wikipedia-parity read view rendered against B2's REAL generated
 * entity-article shape: hatnote, right-floating infobox from the
 * `## Summary` definition list, [n] footnote citations with a hover
 * popover, blue/red wikilinks with page-preview cards, per-section
 * [ edit ] affordances, and the References footnote block.
 */
const meta: Meta<typeof ArticleReadView> = {
  title: "Features/Wiki/ArticleReadView",
  component: ArticleReadView,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div className="wiki-root" style={{ padding: 32 }}>
        <div className="wk-article-page">
          <Story />
        </div>
      </div>
    ),
  ],
  args: {
    title: "Acme Corp",
    articlePath: "team/companies/acme-corp.md",
    resolver: makeWikilinkResolver([
      "team/people/eng.md",
      "team/companies/acme-corp.md",
    ]),
    fetchPreview: async (slug: string) => ({
      title: slug.split("/").pop() ?? slug,
      body: "Eng is a person in the team knowledge graph, with 2 recorded facts from 1 completed task…",
    }),
    onNavigate: () => {},
    onEditSection: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof ArticleReadView>;

/** B2 entity article: infobox + footnote citations + wikilinks. */
export const EntityArticle: Story = {
  args: { content: ENTITY_ARTICLE_FIXTURE },
};

/** A body with a redlink (missing target) next to a blue link. */
export const Redlinks: Story = {
  args: {
    content:
      "**Acme Corp** works with [[people/eng]] and the not-yet-documented [[companies/globex]].\n\n## Notes\n\nHover the blue link for a page preview.\n",
  },
};

/** A plain human-written article: no hatnote, no infobox. */
export const PlainArticle: Story = {
  args: {
    content:
      "Some prose the team wrote by hand.\n\n## Summary\n\nA prose summary that stays in the body because it is not a definition list.\n\n## Details\n\nMore prose.\n",
  },
};

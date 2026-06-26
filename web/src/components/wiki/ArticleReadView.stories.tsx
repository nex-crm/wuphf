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
      body: "Eng is a person. Owns the broker and has led the renewal motion since spring…",
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

/**
 * A compiled article (Karpathy-style): YAML frontmatter is stripped, the lead
 * H1 is dropped, `^[source-id]` markers render as citation badges, a fenced
 * mermaid block renders as a diagram, and the warm-paper `.wiki-reader`
 * measure applies. Hover a citation pill to see its source popover.
 */
export const CompiledArticle: Story = {
  args: {
    title: "Reciprocal Rank Fusion",
    articlePath: "team/concepts/reciprocal-rank-fusion.md",
    onViewSource: () => {},
    content: `---
title: Reciprocal Rank Fusion
kind: concept
categories:
  - retrieval
sources:
  - decision-rrf-1
  - task-wup-12
compiled: true
updated_at: 2026-06-25T12:00:00Z
---

# Reciprocal Rank Fusion

Reciprocal Rank Fusion (RRF) combines several ranked result lists into one by summing reciprocal ranks.^[decision-rrf-1] The team adopted it to fuse BM25 and semantic retrieval.^[task-wup-12]

## How it works

Each document's score is the sum of \`1 / (k + rank)\` across every list it appears in.^[decision-rrf-1]

\`\`\`mermaid
graph LR
  BM25 --> Fuse
  Semantic --> Fuse
  Fuse --> Results
\`\`\`

## Why the team uses it

Hybrid search beats pure semantic retrieval on the team's eval set.^[task-wup-12]
`,
    fetchPreview: async () => null,
  },
};

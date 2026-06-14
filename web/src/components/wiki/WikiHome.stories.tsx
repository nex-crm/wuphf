import type { Meta, StoryObj } from "@storybook/react-vite";

import "../../styles/wiki.css";
import type { WikiCatalogEntry } from "../../api/wiki";
import WikiHome from "./WikiHome";

const CATALOG: WikiCatalogEntry[] = [
  {
    path: "team/companies/acme-corp.md",
    title: "Acme Corp",
    author_slug: "eng",
    last_edited_ts: "2026-06-10T12:00:00Z",
    group: "companies",
  },
  {
    path: "team/people/eng.md",
    title: "Eng",
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T15:30:00Z",
    group: "people",
  },
  {
    path: "team/people/nazz.md",
    title: "Nazz",
    author_slug: "pm",
    last_edited_ts: "2026-06-08T09:00:00Z",
    group: "people",
  },
  {
    path: "team/playbooks/acme-renewal.md",
    title: "Acme Renewal Playbook",
    author_slug: "eng",
    last_edited_ts: "2026-06-07T18:00:00Z",
    group: "playbooks",
  },
];

/** The search-first wiki main page: big search + categories + recent changes. */
const meta: Meta<typeof WikiHome> = {
  title: "Features/Wiki/WikiHome",
  component: WikiHome,
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
type Story = StoryObj<typeof WikiHome>;

/** Audit-log entries drive Recent changes. */
export const WithRecentChanges: Story = {
  args: {
    recentChanges: [
      {
        sha: "a1b2c3d",
        author_slug: "eng",
        timestamp: "2026-06-10T13:00:00Z",
        message: "wiki: regenerate acme-corp entity article",
        paths: ["team/companies/acme-corp.md"],
      },
      {
        sha: "d4e5f6a",
        author_slug: "ceo",
        timestamp: "2026-06-10T09:00:00Z",
        message: "wiki: record renewal playbook execution",
        paths: ["team/playbooks/acme-renewal.md"],
      },
    ],
  },
};

/** Empty audit log falls back to recently edited articles. */
export const CatalogFallback: Story = {
  args: { recentChanges: [] },
};

/** Fresh install: nothing written yet. */
export const EmptyWiki: Story = {
  args: { catalog: [], recentChanges: [] },
};

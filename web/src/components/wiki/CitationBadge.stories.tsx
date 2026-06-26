import type { Meta, StoryObj } from "@storybook/react-vite";

import { ApiError } from "../../api/client";
import type { SourceKind, SourceRecord } from "../../api/sources";
import CitationBadge, { CitationNumberContext } from "./CitationBadge";
import "../../styles/wiki.css";
import "../../styles/wiki-reader.css";

/**
 * Inline citation badge for a compiled-article `^[source-id]` marker. Hover
 * or focus the pill to reveal the source popover (title + kind + origin +
 * "View source"). The three stories inject different fetch behaviors:
 * resolved, perpetually loading, and not-found.
 */
const meta: Meta<typeof CitationBadge> = {
  title: "Features/Wiki/CitationBadge",
  component: CitationBadge,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <CitationNumberContext.Provider value={new Map([["task-wup-12", 1]])}>
        <div className="wiki-root" style={{ padding: 32 }}>
          <p
            className="wk-article-body wiki-reader"
            style={{ fontSize: 15, lineHeight: 1.8 }}
          >
            The renewal motion shipped in Q2 after the pilot converted.{" "}
            <Story /> Hover the pill to see the cited source.
          </p>
        </div>
      </CitationNumberContext.Provider>
    ),
  ],
  args: {
    sourceId: "task-wup-12",
    onViewSource: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof CitationBadge>;

const RESOLVED: SourceRecord = {
  id: "task-wup-12",
  kind: "task",
  title: "Ship the Brex × Nex referral pilot",
  origin: "task/wup-12",
  captured_at: "2026-06-20T12:00:00Z",
  content_hash: "abc123",
  content: "Full task deliverables…",
};

/** A resolved source: popover shows title, kind, origin, and View source. */
export const Resolved: Story = {
  args: {
    fetchSource: async (_kind: SourceKind, _id: string) => RESOLVED,
  },
};

/** A source still loading: popover shows the loading state. */
export const Loading: Story = {
  args: {
    fetchSource: () => new Promise<SourceRecord>(() => {}),
  },
};

/** A missing source: popover degrades to "Source not found". */
export const NotFound: Story = {
  args: {
    fetchSource: async (_kind: SourceKind, _id: string) => {
      throw new ApiError({
        status: 404,
        statusText: "Not Found",
        bodyText: '{"error":"source not found"}',
      });
    },
  },
};

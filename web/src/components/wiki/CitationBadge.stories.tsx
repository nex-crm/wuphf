import type { Meta, StoryObj } from "@storybook/react-vite";

import CitationBadge, { CitationNumberContext } from "./CitationBadge";
import "../../styles/wiki.css";
import "../../styles/wiki-reader.css";

/**
 * Inline citation badge for a compiled-article `^[source-id]` marker. Hover or
 * focus the pill to reveal a lightweight popover with the raw citation id. The
 * WUPHF source store has been retired (gbrain owns the knowledge backend), so
 * the badge no longer fetches a source record — it degrades to showing the id.
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
            <Story /> Hover the pill to see the cited source id.
          </p>
        </div>
      </CitationNumberContext.Provider>
    ),
  ],
  args: {
    sourceId: "task-wup-12",
  },
};

export default meta;
type Story = StoryObj<typeof CitationBadge>;

/** A numbered citation: the pill renders `[1]` and reveals its id on hover. */
export const Numbered: Story = {};

/** An unnumbered citation (not in the numbering context): renders `[cite]`. */
export const Unnumbered: Story = {
  decorators: [
    (Story) => (
      <CitationNumberContext.Provider value={new Map()}>
        <div className="wiki-root" style={{ padding: 32 }}>
          <p
            className="wk-article-body wiki-reader"
            style={{ fontSize: 15, lineHeight: 1.8 }}
          >
            A claim with an unnumbered source <Story /> renders a generic
            marker.
          </p>
        </div>
      </CitationNumberContext.Provider>
    ),
  ],
};

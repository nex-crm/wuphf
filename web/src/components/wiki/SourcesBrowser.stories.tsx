import type { Meta, StoryObj } from "@storybook/react-vite";

import type {
  SourceKind,
  SourceMetadata,
  SourceRecord,
} from "../../api/sources";
import SourcesBrowser from "./SourcesBrowser";
import "../../styles/wiki.css";
import "../../styles/wiki-reader.css";

/**
 * The Sources browser — the immutable source layer the wiki compiles from.
 * Records group by kind; selecting a row opens the full record as markdown.
 */
const meta: Meta<typeof SourcesBrowser> = {
  title: "Features/Wiki/SourcesBrowser",
  component: SourcesBrowser,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <div className="wiki-root" style={{ height: "100vh" }}>
        <div className="wiki-layout" data-view="sources">
          <Story />
        </div>
      </div>
    ),
  ],
  args: {
    onSelect: () => {},
    onBack: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof SourcesBrowser>;

const SAMPLE: SourceMetadata[] = [
  {
    id: "task-wup-12",
    kind: "task",
    title: "Ship the Brex × Nex referral pilot",
    origin: "task/wup-12",
    captured_at: "2026-06-20T12:00:00Z",
    content_hash: "a1",
  },
  {
    id: "task-wup-7",
    kind: "task",
    title: "Draft the onboarding wizard",
    origin: "task/wup-7",
    captured_at: "2026-06-18T09:30:00Z",
    content_hash: "a2",
  },
  {
    id: "decision-rrf-1",
    kind: "decision",
    title: "Adopt reciprocal rank fusion for hybrid search",
    origin: "decisions/2026-06-15",
    captured_at: "2026-06-15T16:00:00Z",
    content_hash: "b1",
  },
  {
    id: "chat-general-2026-06-25",
    kind: "chat",
    title: "Standup digest — general",
    origin: "#general",
    captured_at: "2026-06-25T08:00:00Z",
    content_hash: "c1",
  },
  {
    id: "note-pricing-thoughts",
    kind: "note",
    title: "Pricing thoughts",
    captured_at: "2026-06-22T14:00:00Z",
    content_hash: "d1",
  },
];

const RECORD: SourceRecord = {
  ...SAMPLE[0],
  content:
    "# Brex × Nex referral pilot\n\nThe pilot converted **3 of 5** referred accounts.\n\n## Outcome\n\nRevenue impact landed in Q2.\n",
};

/** The grouped list across several kinds. */
export const List: Story = {
  args: {
    selection: null,
    listSourcesFn: async () => SAMPLE,
  },
};

/** Empty state when no sources have been captured yet. */
export const Empty: Story = {
  args: {
    selection: null,
    listSourcesFn: async () => [],
  },
};

/** A single record opened from the list, rendered as markdown. */
export const RecordDetail: Story = {
  args: {
    selection: { kind: "task", id: "task-wup-12" },
    listSourcesFn: async () => SAMPLE,
    readSourceFn: async (_kind: SourceKind, _id: string) => RECORD,
  },
};

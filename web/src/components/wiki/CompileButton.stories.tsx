import type { Meta, StoryObj } from "@storybook/react-vite";

import type { CompileResult } from "../../api/sources";
import CompileButton from "./CompileButton";
import "../../styles/wiki.css";
import "../../styles/wiki-reader.css";

/**
 * Compile trigger — runs the deterministic compile engine over the source
 * layer and surfaces the result tally inline. Click "Compile wiki" in each
 * story to see the corresponding outcome.
 */
const meta: Meta<typeof CompileButton> = {
  title: "Features/Wiki/CompileButton",
  component: CompileButton,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div className="wiki-root" style={{ padding: 32 }}>
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof CompileButton>;

const RESULT: CompileResult = {
  pages_written: 4,
  concepts: 6,
  sources_read: 11,
};

/** A clean compile run: pages written, no warnings. */
export const Success: Story = {
  args: {
    compile: async () => RESULT,
  },
};

/** A compile run that wrote pages but reported non-fatal warnings. */
export const WithWarnings: Story = {
  args: {
    compile: async () => ({
      ...RESULT,
      pages_written: 3,
      errors: [
        "source note-foo-1a2b: empty content, skipped",
        "page rrf: runner returned empty body",
      ],
    }),
  },
};

/** A failed compile: the button surfaces an error. */
export const Failure: Story = {
  args: {
    compile: async () => {
      throw new Error("wiki backend is not active");
    },
  },
};

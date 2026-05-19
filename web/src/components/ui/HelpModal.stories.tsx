import { useState } from "react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { HelpModal } from "./HelpModal";

const meta: Meta<typeof HelpModal> = {
  title: "Design System/Organisms/HelpModal",
  component: HelpModal,
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof HelpModal>;

export const Default: Story = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(true);
      return (
        <div style={{ minHeight: 500, padding: 24 }}>
          <button
            type="button"
            className="btn"
            onClick={() => setOpen(true)}
            disabled={open}
          >
            Open help
          </button>
          <HelpModal open={open} onClose={() => setOpen(false)} />
        </div>
      );
    }
    return <Wrapper />;
  },
};

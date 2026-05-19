import { useState } from "react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { SidePanel } from "./SidePanel";

const meta: Meta<typeof SidePanel> = {
  title: "Design System/Organisms/SidePanel",
  component: SidePanel,
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof SidePanel>;

export const Default: Story = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(true);
      return (
        <div style={{ minHeight: 400, padding: 24 }}>
          <button
            type="button"
            className="btn"
            onClick={() => setOpen(true)}
            disabled={open}
          >
            Open panel
          </button>
          <SidePanel
            open={open}
            onClose={() => setOpen(false)}
            title="Skill — recap-thread"
            subtitle="recap-thread"
          >
            <div
              style={{ padding: 20, color: "var(--text)", lineHeight: 1.55 }}
            >
              <p style={{ marginBottom: 12 }}>
                A side panel keeps the main view in context while the user
                explores a secondary surface — skill detail, thread view, agent
                profile.
              </p>
              <p style={{ marginBottom: 12, color: "var(--text-secondary)" }}>
                Press <kbd className="kbd kbd-md">Esc</kbd>, click the
                backdrop, or hit the × to close. Focus is trapped while open
                and restored to the trigger on close.
              </p>
              <ul style={{ paddingLeft: 18, color: "var(--text-secondary)" }}>
                <li>480px desktop</li>
                <li>60vw tablet</li>
                <li>fullscreen mobile</li>
              </ul>
            </div>
          </SidePanel>
        </div>
      );
    }
    return <Wrapper />;
  },
};

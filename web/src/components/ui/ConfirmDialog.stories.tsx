import type { Meta, StoryObj } from "@storybook/react-vite";

import { confirm, ConfirmHost } from "./ConfirmDialog";
import { showNotice, ToastContainer } from "./Toast";

const meta: Meta = {
  title: "Design System/Organisms/ConfirmDialog",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Playground: StoryObj = {
  render: () => (
    <div
      style={{
        minHeight: 360,
        padding: 24,
        display: "flex",
        flexDirection: "column",
        gap: 12,
        alignItems: "flex-start",
      }}
    >
      <button
        type="button"
        className="btn"
        onClick={() =>
          confirm({
            title: "Archive this channel?",
            message: "Members keep history; new posts get blocked.",
            confirmLabel: "Archive",
            onConfirm: () => showNotice("Archived", "success"),
          })
        }
      >
        Confirm — neutral
      </button>
      <button
        type="button"
        className="btn"
        onClick={() =>
          confirm({
            title: "Delete workspace?",
            message: "This permanently deletes the workspace and its history.",
            details: (
              <ul style={{ marginTop: 8, paddingLeft: 18 }}>
                <li>All channels and messages</li>
                <li>All wiki entries</li>
                <li>All agent state</li>
              </ul>
            ),
            confirmLabel: "Delete forever",
            danger: true,
            onConfirm: () => showNotice("Deleted", "error"),
          })
        }
      >
        Confirm — danger
      </button>
      <ConfirmHost />
      <ToastContainer />
    </div>
  ),
};

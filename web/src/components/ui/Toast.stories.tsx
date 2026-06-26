import type { Meta, StoryObj } from "@storybook/react-vite";

import { showNotice, showUndoToast, ToastContainer } from "./Toast";

const meta: Meta = {
  title: "Design System/Molecules/Toast",
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
        onClick={() => showNotice("Saved successfully", "success")}
      >
        Show success
      </button>
      <button
        type="button"
        className="btn"
        onClick={() => showNotice("Something went wrong", "error")}
      >
        Show error
      </button>
      <button
        type="button"
        className="btn"
        onClick={() => showNotice("Heads up — provider switched", "info")}
      >
        Show info
      </button>
      <button
        type="button"
        className="btn"
        onClick={() =>
          showUndoToast("Channel archived", () =>
            showNotice("Restored", "success"),
          )
        }
      >
        Show undo toast
      </button>
      <ToastContainer />
    </div>
  ),
};

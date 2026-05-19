import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  ShredCardSubtitle,
  ShredDeletionsList,
  ShredWarningCopy,
} from "./ShredWarning";

const meta: Meta = {
  title: "Patterns/Shred warning",
  parameters: { layout: "padded" },
};

export default meta;

export const ModalCopy: StoryObj = {
  render: () => (
    <div
      style={{
        maxWidth: 480,
        padding: 16,
        color: "var(--text)",
        lineHeight: 1.55,
        fontSize: 13,
      }}
    >
      <ShredWarningCopy />
    </div>
  ),
};

export const CardSubtitle: StoryObj = {
  render: () => (
    <div
      style={{
        maxWidth: 480,
        padding: 16,
        color: "var(--text-secondary)",
        lineHeight: 1.55,
        fontSize: 13,
      }}
    >
      <ShredCardSubtitle />
    </div>
  ),
};

export const DeletionsList: StoryObj = {
  render: () => (
    <div style={{ maxWidth: 480, padding: 16, color: "var(--text)" }}>
      <ShredDeletionsList />
    </div>
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/Status dot",
  parameters: { layout: "padded" },
};

export default meta;

function Row({ cls, label }: { cls: string; label: string }) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "8px 0",
        fontSize: 13,
        color: "var(--text)",
        borderBottom: "1px solid var(--border-light)",
      }}
    >
      <span className={cls} />
      <code
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--text-secondary)",
          width: 220,
        }}
      >
        {cls}
      </code>
      <span>{label}</span>
    </div>
  );
}

export const Variants: StoryObj = {
  render: () => (
    <div style={{ maxWidth: 520 }}>
      <Row cls="status-dot active" label="Online / healthy" />
      <Row cls="status-dot active pulse" label="Live — actively transmitting" />
      <Row cls="status-dot shipping" label="Shipping update" />
      <Row cls="status-dot plotting" label="Plotting / thinking" />
      <Row cls="status-dot lurking" label="Idle — quietly present" />
    </div>
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/Input",
  parameters: { layout: "padded" },
};

export default meta;

export const Default: StoryObj = {
  render: () => (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 12,
        maxWidth: 360,
      }}
    >
      <input className="input" placeholder="Your name" />
      <input className="input" placeholder="Email" type="email" />
      <input className="input" defaultValue="atlas" />
    </div>
  ),
};

export const States: StoryObj = {
  render: () => (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 16,
        maxWidth: 360,
      }}
    >
      <Labeled label="Default">
        <input className="input" placeholder="Type something" />
      </Labeled>
      <Labeled label="Disabled">
        <input className="input" defaultValue="atlas" disabled />
      </Labeled>
      <Labeled label="Error">
        <input
          className="input has-error"
          defaultValue="not-an-email"
          aria-invalid
        />
      </Labeled>
    </div>
  ),
};

export const WithLabel: StoryObj = {
  render: () => (
    <label
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 6,
        maxWidth: 360,
        color: "var(--text)",
        fontSize: 13,
      }}
    >
      <span
        style={{
          fontSize: 11,
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          color: "var(--text-tertiary)",
          fontWeight: 600,
        }}
      >
        Workspace name
      </span>
      <input
        className="input"
        defaultValue="wuphf-prod"
        style={{ width: "100%" }}
      />
    </label>
  ),
};

export const Textarea: StoryObj = {
  render: () => (
    <textarea
      className="input"
      style={{ width: 360, height: 120, padding: 12 }}
      placeholder="What did you ship today?"
    />
  ),
};

function Labeled({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <span
        style={{
          fontSize: 11,
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          color: "var(--text-tertiary)",
          fontWeight: 600,
        }}
      >
        {label}
      </span>
      {children}
    </div>
  );
}

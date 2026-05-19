import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Patterns/Focus ring",
  parameters: { layout: "padded" },
};

export default meta;

export const Examples: StoryObj = {
  render: () => (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 16,
        maxWidth: 480,
      }}
    >
      <p style={{ color: "var(--text-secondary)", fontSize: 13 }}>
        Tab through these to see the system focus ring. It uses
        <code style={{ margin: "0 4px" }}>--focus-ring-color</code> and
        <code style={{ margin: "0 4px" }}>--focus-ring-width</code>, with
        <code style={{ margin: "0 4px" }}>:focus-visible</code> so mouse clicks
        don't trigger it.
      </p>
      <button type="button" className="btn btn-primary" style={{ width: 120 }}>
        Primary
      </button>
      <button type="button" className="btn btn-secondary" style={{ width: 120 }}>
        Secondary
      </button>
      <input className="input" placeholder="Tab to focus" />
      <a
        href="#"
        style={{
          color: "var(--accent)",
          fontSize: 13,
          textDecoration: "none",
          width: "fit-content",
        }}
      >
        A focusable link
      </a>
    </div>
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";

type Variant = "primary" | "ghost" | "danger";
type Size = "sm" | "md" | "lg";

interface ButtonArgs {
  label: string;
  variant: Variant;
  size: Size;
  disabled: boolean;
}

function classFor(variant: Variant, size: Size) {
  const base = "btn";
  const variantCls = `btn-${variant}`;
  const sizeCls = size === "sm" ? "btn-sm" : size === "lg" ? "btn-lg" : "";
  return [base, variantCls, sizeCls].filter(Boolean).join(" ");
}

const meta: Meta<ButtonArgs> = {
  title: "Design System/Atoms/Button",
  parameters: { layout: "centered" },
  argTypes: {
    variant: {
      control: "inline-radio",
      options: ["primary", "ghost", "danger"],
    },
    size: { control: "inline-radio", options: ["sm", "md", "lg"] },
    disabled: { control: "boolean" },
  },
  args: { label: "Button", variant: "primary", size: "md", disabled: false },
  render: ({ label, variant, size, disabled }) => (
    <button
      type="button"
      className={classFor(variant, size)}
      disabled={disabled}
    >
      {label}
    </button>
  ),
};

export default meta;
type Story = StoryObj<ButtonArgs>;

export const Default: Story = {};

const VARIANTS: Variant[] = ["primary", "ghost", "danger"];
const SIZES: Size[] = ["sm", "md", "lg"];

export const Variants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${VARIANTS.length}, auto)`,
        gap: 12,
        alignItems: "center",
      }}
    >
      {VARIANTS.map((variant) => (
        <button
          key={variant}
          type="button"
          className={classFor(variant, "md")}
        >
          {variant}
        </button>
      ))}
    </div>
  ),
};

export const Sizes: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
      {SIZES.map((size) => (
        <button key={size} type="button" className={classFor("primary", size)}>
          {size}
        </button>
      ))}
    </div>
  ),
};

export const States: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
      <button type="button" className="btn btn-primary">
        Default
      </button>
      <button type="button" className="btn btn-primary" disabled>
        Disabled
      </button>
      <button type="button" className="btn-text">
        Text
      </button>
      <button type="button" className="btn-text btn-text--danger">
        Text danger
      </button>
    </div>
  ),
};

export const InContext: Story = {
  name: "In context",
  parameters: { controls: { disable: true } },
  render: () => (
    <div
      style={{
        display: "flex",
        gap: 8,
        padding: 24,
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-md)",
        background: "var(--bg-card)",
        maxWidth: 520,
        justifyContent: "flex-end",
      }}
    >
      <button type="button" className="btn-text">
        Cancel
      </button>
      <button type="button" className="btn btn-ghost">
        Save draft
      </button>
      <button type="button" className="btn btn-primary">
        Publish
      </button>
    </div>
  ),
};

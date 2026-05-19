import type { Meta, StoryObj } from "@storybook/react-vite";
import type { CSSProperties } from "react";
import { BookStack, HomeSimple, Terminal } from "iconoir-react";

import { Kbd, MOD_KEY } from "../ui/Kbd";
import { SidebarItem } from "./SidebarItem";

// Brand canvas — components render directly on the page, not in a faux sidebar.
// Sidebar-item tokens are remapped here so labels read on the saturated brand.
const brandCanvas: CSSProperties = {
  background: "#612a92",
  minHeight: "100vh",
  padding: "32px",
  boxSizing: "border-box",
  ["--text" as string]: "#ffffff",
  ["--text-secondary" as string]: "rgba(255, 255, 255, 0.88)",
  ["--text-tertiary" as string]: "rgba(255, 255, 255, 0.6)",
  ["--accent-bg" as string]: "rgba(255, 255, 255, 0.14)",
};
const itemStack: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 2,
  width: 280,
};

const meta = {
  title: "Sidebar/Item",
  component: SidebarItem,
  parameters: { layout: "fullscreen" },
  tags: ["autodocs"],
  args: { label: "architecture", icon: "#" },
  decorators: [
    (Story) => (
      <div style={brandCanvas}>
        <div style={itemStack}>
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof SidebarItem>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

const shortcut = (n: number) => (
  <span className="sidebar-shortcut" aria-hidden="true">
    <Kbd size="sm">{`${MOD_KEY}${n}`}</Kbd>
  </span>
);
const badge = (n: number) => <span className="sidebar-badge">{n}</span>;

export const Variants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <>
      <SidebarItem icon="#" label="default row" />
      <SidebarItem icon="#" label="with badge" badge={badge(3)} />
      <SidebarItem icon="#" label="with shortcut" shortcut={shortcut(1)} />
      <SidebarItem
        icon="#"
        label="badge + shortcut"
        badge={badge(12)}
        shortcut={shortcut(2)}
      />
      <SidebarItem
        icon={<HomeSimple className="sidebar-item-icon" />}
        label="SVG icon"
      />
      <SidebarItem
        icon={<BookStack className="sidebar-item-icon" />}
        label="SVG + badge"
        badge={badge(2)}
      />
      <SidebarItem variant="add" icon="+" label="New issue" />
    </>
  ),
};

export const States: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <>
      <SidebarItem icon="#" label="default" />
      <SidebarItem icon="#" label="active" active />
      <SidebarItem icon="#" label="active + unread" active badge={badge(3)} />
      <SidebarItem
        icon={<Terminal className="sidebar-item-icon" />}
        label="active SVG"
        active
      />
      <SidebarItem variant="add" icon="+" label="add idle" />
    </>
  ),
};

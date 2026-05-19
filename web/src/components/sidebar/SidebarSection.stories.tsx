import type { Meta, StoryObj } from "@storybook/react-vite";
import type { CSSProperties } from "react";
import { useState } from "react";
import {
  BookStack,
  CheckCircle,
  HomeSimple,
  Page,
  Settings as SettingsIcon,
  Terminal,
  User,
} from "iconoir-react";

import { Kbd, MOD_KEY } from "../ui/Kbd";
import { SidebarItem } from "./SidebarItem";
import { SidebarSection } from "./SidebarSection";

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
const sectionStack: CSSProperties = { width: 280 };

// Story-only: collapsible body uses `flex: 1 1 0` to fill the live sidebar's
// height. In an isolated story it resolves to 0, so we force natural height.
const collapsibleOverride = (
  <style>{`.sidebar-collapsible.is-open { flex: none; display: block; }`}</style>
);

const meta = {
  title: "Sidebar/Section",
  component: SidebarSection,
  parameters: { layout: "fullscreen" },
  tags: ["autodocs"],
  args: {
    label: "Channels",
    open: true,
    onToggle: () => {},
    children: null,
  },
  decorators: [
    (Story) => (
      <div style={brandCanvas}>
        {collapsibleOverride}
        <div style={sectionStack}>
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof SidebarSection>;

export default meta;
type Story = StoryObj<typeof meta>;

const shortcut = (n: number) => (
  <span className="sidebar-shortcut" aria-hidden="true">
    <Kbd size="sm">{`${MOD_KEY}${n}`}</Kbd>
  </span>
);
const badge = (n: number) => <span className="sidebar-badge">{n}</span>;

function ChannelsBody() {
  return (
    <div className="sidebar-channels">
      <SidebarItem icon="#" label="architecture" active shortcut={shortcut(1)} />
      <SidebarItem icon="#" label="deploys" badge={badge(2)} shortcut={shortcut(2)} />
      <SidebarItem icon="#" label="wiki" shortcut={shortcut(3)} />
      <SidebarItem variant="add" icon="+" label="New Channel" />
    </div>
  );
}

function IssuesBody() {
  return (
    <div className="sidebar-issues">
      <SidebarItem icon="#" label="Auth token rotation" />
      <SidebarItem icon="#" label="Calendar sync drift" active />
      <SidebarItem variant="add" icon="+" label="New issue" />
    </div>
  );
}

function ToolsBody() {
  return (
    <div className="sidebar-apps">
      <SidebarItem icon={<HomeSimple className="sidebar-item-icon" />} label="Overview" active />
      <SidebarItem
        icon={<BookStack className="sidebar-item-icon" />}
        label="Wiki"
        badge={badge(2)}
      />
      <SidebarItem icon={<Terminal className="sidebar-item-icon" />} label="Console" />
    </div>
  );
}

function RecentBody() {
  return (
    <div className="sidebar-scroll-wrap is-recent">
      <div className="sidebar-recent">
        <SidebarItem
          icon={<CheckCircle className="sidebar-item-icon" />}
          label="bookkeeping-invoicing-service-3"
        />
        <SidebarItem
          icon={<CheckCircle className="sidebar-item-icon" />}
          label="bookkeeping-invoicing-service-4"
        />
        <SidebarItem
          icon={<Page className="sidebar-item-icon" />}
          label="people/nazz"
        />
        <SidebarItem
          icon={<User className="sidebar-item-icon" />}
          label="atlas"
        />
        <SidebarItem
          icon={<SettingsIcon className="sidebar-item-icon" />}
          label="Workspace"
        />
      </div>
    </div>
  );
}

const viewAllAction = (
  <button type="button" className="sidebar-section-action">
    View all
  </button>
);

export const Default: Story = {
  render: () => {
    const [open, setOpen] = useState(true);
    return (
      <SidebarSection
        label="Channels"
        open={open}
        onToggle={() => setOpen((v) => !v)}
      >
        <ChannelsBody />
      </SidebarSection>
    );
  },
};

export const Collapsed: Story = {
  render: () => (
    <SidebarSection label="Channels" open={false} onToggle={() => {}}>
      <ChannelsBody />
    </SidebarSection>
  ),
};

export const WithHeaderAction: Story = {
  render: () => (
    <SidebarSection
      label="Issues"
      open
      onToggle={() => {}}
      headerActions={viewAllAction}
    >
      <IssuesBody />
    </SidebarSection>
  ),
};

function StatefulSection({
  label,
  headerActions,
  children,
}: {
  label: string;
  headerActions?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <SidebarSection
      label={label}
      open={open}
      onToggle={() => setOpen((v) => !v)}
      headerActions={headerActions}
    >
      {children}
    </SidebarSection>
  );
}

export const Variants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div>
      <StatefulSection label="Channels">
        <ChannelsBody />
      </StatefulSection>
      <StatefulSection label="Issues" headerActions={viewAllAction}>
        <IssuesBody />
      </StatefulSection>
      <StatefulSection label="Tools">
        <ToolsBody />
      </StatefulSection>
      <StatefulSection label="Recent">
        <RecentBody />
      </StatefulSection>
    </div>
  ),
};

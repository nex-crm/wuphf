import type { Meta, StoryObj } from "@storybook/react-vite";
import { useEffect } from "react";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import type { Workspace } from "../../api/workspaces";
import { WorkspaceRail } from "./WorkspaceRail";

/**
 * Left-edge 56px-wide rail.
 *
 * Top: a single tile for the active workspace. Clicking it opens a
 * centered "Switch workspace" modal listing the other workspaces (each
 * with a kebab → Pause / Resume / Settings / Shred…) plus a `+ New
 * workspace` row.
 *
 * Tools group (below the workspace tile, ordered): Inbox → Overview →
 * Wiki → Issues → Calendar → Skills → `[⋮⋮⋮] More tools` popover
 * (Console, Graph, Policies, Activity, Receipts, Access & Health).
 *
 * Footer: Settings, pinned to the bottom of the rail.
 *
 * Each rail icon is a `RailIconButton` (36×36, 10px radius, cyan-400
 * active fill). Hover hints portal to the right of the rail.
 */
const meta: Meta = {
  title: "Design System/Organisms/WorkspaceRail",
  parameters: { layout: "fullscreen" },
};

export default meta;

function RailFrame({
  children,
  description,
}: {
  children: React.ReactNode;
  description?: React.ReactNode;
}) {
  return (
    <div
      style={{ display: "flex", minHeight: "100vh", background: "var(--bg)" }}
    >
      {children}
      <div
        style={{
          flex: 1,
          padding: 32,
          color: "var(--text)",
          maxWidth: 720,
        }}
      >
        <h2 style={{ margin: 0, marginBottom: 8 }}>App canvas</h2>
        <p style={{ color: "var(--text-secondary)" }}>
          {description ?? (
            <>
              The rail on the left renders the active workspace, the Tools
              column (Inbox / Overview / Wiki / Issues / Calendar / Skills +
              "More tools"), and a pinned Settings shortcut at the bottom.
            </>
          )}
        </p>
      </div>
    </div>
  );
}

/* 1 — Canonical default ------------------------------------------------ */
export const Default: StoryObj = {
  render: () => (
    <SidebarContext initialUrl="/apps/overview">
      <RailFrame>
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

/* 2 — Tool-active variants (cyan-400 highlight on the selected tool) ---- */
export const InboxActive: StoryObj = {
  name: "Active tool — Inbox (with badge)",
  render: () => (
    <SidebarContext initialUrl="/apps/inbox">
      <RailFrame description="Inbox sits at the top of the tools column. When it has unread attention items the red badge appears in the top-right corner of the icon.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

export const WikiActive: StoryObj = {
  name: "Active tool — Wiki",
  render: () => (
    <SidebarContext initialUrl="/apps/wiki">
      <RailFrame description="The active tool inherits the same cyan-400 fill as the active workspace highlight — the rail keeps a single 'where am I' colour system.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

export const IssuesActive: StoryObj = {
  name: "Active tool — Issues",
  render: () => (
    <SidebarContext initialUrl="/issues">
      <RailFrame description="Issues has its own /issues route (not an app panel). The rail icon lights up on any issues-list, issue-detail, or issue-new surface.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

export const SettingsActive: StoryObj = {
  name: "Active tool — Settings (footer)",
  render: () => (
    <SidebarContext initialUrl="/apps/settings">
      <RailFrame description="Settings is parked at the bottom of the rail with a divider above it. Its active state mirrors the other tools: cyan-400 fill + dark ink.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

/* 3 — Inbox badge variants -------------------------------------------- */
const INBOX_MANY = Array.from({ length: 12 }).map((_, i) => ({
  kind: "request",
  id: `r${i}`,
}));

export const InboxBadge9Plus: StoryObj = {
  name: "Inbox badge — 9+",
  render: () => (
    <SidebarContext initialUrl="/apps/overview" inboxItems={INBOX_MANY}>
      <RailFrame description="When attention items exceed 9 the badge clamps to '9+' so it stays a fixed width.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

export const InboxBadgeNone: StoryObj = {
  name: "Inbox badge — empty",
  render: () => (
    <SidebarContext initialUrl="/apps/overview" inboxItems={[]}>
      <RailFrame description="No attention items → the Inbox icon renders without a badge.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

/* 4 — Workspace switcher modal --------------------------------------- */
function ClickWhenMounted({
  selector,
  children,
}: {
  selector: string;
  children: React.ReactNode;
}) {
  // Fire a click after mount so the popover/modal renders into the
  // story snapshot without manual interaction.
  useEffect(() => {
    const id = window.setTimeout(() => {
      document.querySelector<HTMLButtonElement>(selector)?.click();
    }, 80);
    return () => window.clearTimeout(id);
  }, [selector]);
  return <>{children}</>;
}

export const SwitcherModalOpen: StoryObj = {
  name: "Switch workspace modal",
  parameters: {
    docs: {
      description: {
        story:
          "Clicking the active workspace tile opens a centered modal (360px, 20px corners) listing other workspaces. Each row has a kebab (`⋯`) anchored to the right; clicking it surfaces the Pause / Resume / Settings / Shred menu over the modal without dismissing it.",
      },
    },
  },
  render: () => (
    <SidebarContext initialUrl="/apps/overview">
      <RailFrame>
        <ClickWhenMounted selector='[data-testid="workspace-icon-main"]'>
          <WorkspaceRail navigate={() => {}} />
        </ClickWhenMounted>
      </RailFrame>
    </SidebarContext>
  ),
};

/* 5 — More-tools popover --------------------------------------------- */
export const MoreToolsPopoverOpen: StoryObj = {
  name: "More tools popover",
  parameters: {
    docs: {
      description: {
        story:
          "The 3×3 dots trigger opens a popover anchored to the right of the icon (bottom-edge aligned to the trigger's bottom). Contains the secondary tools — Console, Graph, Policies, Activity, Receipts, Access & Health.",
      },
    },
  },
  render: () => (
    <SidebarContext initialUrl="/apps/overview">
      <RailFrame>
        <ClickWhenMounted selector='[data-rail-more-trigger="true"]'>
          <WorkspaceRail navigate={() => {}} />
        </ClickWhenMounted>
      </RailFrame>
    </SidebarContext>
  ),
};

/* 6 — Workspace lifecycle states ------------------------------------- */
const PAUSED_ACTIVE: Workspace[] = [
  {
    name: "atlas",
    runtime_home: "/tmp/atlas",
    broker_port: 7890,
    web_port: 7891,
    state: "paused",
    company_name: "Atlas Group",
    is_active: true,
    last_used_at: new Date(Date.now() - 3_600_000).toISOString(),
  },
  {
    name: "spare",
    runtime_home: "/tmp/spare",
    broker_port: 7900,
    web_port: 7901,
    state: "running",
    is_active: false,
  },
];

export const PausedActive: StoryObj = {
  name: "Active workspace is paused",
  render: () => (
    <SidebarContext
      initialUrl="/apps/overview"
      workspaces={PAUSED_ACTIVE}
      activeWorkspace="atlas"
    >
      <RailFrame description="Paused active workspace — tile dims, state dot turns grey; the cyan ring still anchors the eye.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

const SOLO: Workspace[] = [
  {
    name: "solo",
    runtime_home: "/tmp/solo",
    broker_port: 7890,
    web_port: 7891,
    state: "running",
    company_name: "Solo Studio",
    is_active: true,
    last_used_at: new Date().toISOString(),
  },
];

export const SingleWorkspace: StoryObj = {
  name: "Only one workspace",
  render: () => (
    <SidebarContext initialUrl="/apps/overview" workspaces={SOLO}>
      <RailFrame description="Edge case: a single workspace. Opening the switcher shows 'No other workspaces.' above the New workspace row.">
        <WorkspaceRail navigate={() => {}} />
      </RailFrame>
    </SidebarContext>
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";
import { useEffect } from "react";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import { CollapsedSidebar } from "./CollapsedSidebar";

/**
 * 56px-wide collapsed rail surface.
 *
 * Layout (top → bottom):
 *
 *   1. SidebarExpand (re-expand the sidebar)
 *   2. Section rail — Agents (Group), Channels (MultiBubble),
 *      Issues (ChatBubbleWarning), Recent (ClockRotateRight). Hovering
 *      any icon opens a popover anchored to the right with the matching
 *      list.
 *   3. Usage rail — pinned to the bottom via `margin-top: auto`.
 *
 * All popovers share the `.sidebar-rail-popover` shell (260px wide,
 * 12/14 padded title, 6/10 padded body, consistent hover/active token
 * usage on items) so the four sections read as a single system.
 */
const meta: Meta = {
  title: "Design System/Organisms/CollapsedSidebar",
  parameters: { layout: "fullscreen" },
};

export default meta;

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        display: "flex",
        minHeight: "100vh",
        background: "var(--bg-card)",
      }}
    >
      <aside
        className="sidebar sidebar-collapsed"
        style={{ height: "100vh", overflow: "hidden" }}
      >
        {children}
      </aside>
      <div
        style={{
          flex: 1,
          padding: 32,
          color: "var(--text)",
          background: "var(--bg)",
        }}
      >
        <h2 style={{ margin: 0, marginBottom: 8 }}>App canvas</h2>
        <p style={{ color: "var(--text-secondary)" }}>
          Hover an icon on the rail to open its popover. Switch themes from the
          toolbar to verify the popover chrome stays consistent.
        </p>
      </div>
    </div>
  );
}

function HoverWhenMounted({
  selector,
  children,
}: {
  selector: string;
  children: React.ReactNode;
}) {
  useEffect(() => {
    const id = window.setTimeout(() => {
      const target = document.querySelector<HTMLElement>(selector);
      target?.dispatchEvent(
        new MouseEvent("mouseenter", { bubbles: true }),
      );
      target?.focus?.();
    }, 80);
    return () => window.clearTimeout(id);
  }, [selector]);
  return <>{children}</>;
}

export const Default: StoryObj = {
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <Frame>
        <CollapsedSidebar />
      </Frame>
    </SidebarContext>
  ),
};

export const AgentsPopover: StoryObj = {
  name: "Popover — Agents",
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <Frame>
        <HoverWhenMounted selector='aside.sidebar-collapsed button[aria-label="Agents"]'>
          <CollapsedSidebar />
        </HoverWhenMounted>
      </Frame>
    </SidebarContext>
  ),
};

export const ChannelsPopover: StoryObj = {
  name: "Popover — Channels",
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <Frame>
        <HoverWhenMounted selector='aside.sidebar-collapsed button[aria-label="Channels"]'>
          <CollapsedSidebar />
        </HoverWhenMounted>
      </Frame>
    </SidebarContext>
  ),
};

export const IssuesPopover: StoryObj = {
  name: "Popover — Issues",
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <Frame>
        <HoverWhenMounted selector='aside.sidebar-collapsed button[aria-label="Issues"]'>
          <CollapsedSidebar />
        </HoverWhenMounted>
      </Frame>
    </SidebarContext>
  ),
};

export const RecentPopover: StoryObj = {
  name: "Popover — Recent (empty state)",
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <Frame>
        <HoverWhenMounted selector='aside.sidebar-collapsed button[aria-label="Recent"]'>
          <CollapsedSidebar />
        </HoverWhenMounted>
      </Frame>
    </SidebarContext>
  ),
};

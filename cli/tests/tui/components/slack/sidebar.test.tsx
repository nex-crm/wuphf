import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  SlackSidebar,
  WorkspaceHeader,
  SidebarSection,
  SidebarItem,
} from "../../../../src/tui/components/slack/sidebar.js";
import type { SidebarItemData } from "../../../../src/tui/components/slack/sidebar-types.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Test data ───────────────────────────────────────────────────────

function makeDM(
  id: string,
  name: string,
  online: boolean,
  unread = 0,
): SidebarItemData {
  return {
    id,
    name,
    type: "dm",
    online,
    unread,
    hasMention: false,
    muted: false,
    lastActivity: Date.now(),
  };
}

function makeChannel(
  id: string,
  name: string,
  unread = 0,
): SidebarItemData {
  return {
    id,
    name,
    type: "channel",
    unread,
    hasMention: false,
    muted: false,
    lastActivity: Date.now(),
  };
}

const dmItems: SidebarItemData[] = [
  makeDM("dm-founding", "Founding Agent", true),
  makeDM("dm-seo", "SEO Analyst", true),
  makeDM("dm-lead", "Lead Gen", false),
];

const channelItems: SidebarItemData[] = [
  makeChannel("ch-general", "general"),
  makeChannel("ch-leads", "leads", 3),
  makeChannel("ch-seo", "seo"),
  makeChannel("ch-support", "support"),
];

const sections = [
  { title: "Direct Messages", items: dmItems },
  { title: "Channels", items: channelItems },
];

// ── WorkspaceHeader ─────────────────────────────────────────────────

describe("WorkspaceHeader", () => {
  it("renders workspace name", () => {
    const { lastFrame } = render(
      <WorkspaceHeader name="Nex Workspace" width={24} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Nex Workspace"), "should show workspace name");
  });

  it("renders a divider line", () => {
    const { lastFrame } = render(
      <WorkspaceHeader name="Test" width={24} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("─"), "should show divider");
  });
});

// ── SidebarItem ─────────────────────────────────────────────────────

describe("SidebarItem", () => {
  it("shows green dot for online DM", () => {
    const item = makeDM("dm-1", "Agent One", true);
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={false} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("●"), "should show filled dot for online");
    assert.ok(frame.includes("Agent One"), "should show agent name");
  });

  it("shows hollow dot for offline DM", () => {
    const item = makeDM("dm-2", "Agent Two", false);
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={false} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("○"), "should show hollow dot for offline");
  });

  it("shows # prefix for channel", () => {
    const item = makeChannel("ch-1", "general");
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={false} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("# general"), "should show # prefix");
  });

  it("shows unread count badge", () => {
    const item = makeChannel("ch-2", "leads", 3);
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={false} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(3)"), "should show unread count");
  });

  it("shows > indicator when active", () => {
    const item = makeDM("dm-3", "Active Agent", true);
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={true} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("▎"), "should show ▎ for active item");
  });

  it("does not show badge when unread is 0", () => {
    const item = makeChannel("ch-3", "quiet");
    const { lastFrame } = render(
      <SidebarItem item={item} isActive={false} isCursor={false} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("("), "should not show badge when no unreads");
  });
});

// ── SidebarSection ──────────────────────────────────────────────────

describe("SidebarSection", () => {
  it("shows expanded triangle and items when not collapsed", () => {
    const { lastFrame } = render(
      <SidebarSection
        title="Direct Messages"
        collapsed={false}
        items={dmItems}
        activeChannelId=""
        cursorIndex={-1}
        startIndex={0}
        onToggle={() => {}}
        onSelect={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("▾"), "should show expanded triangle");
    assert.ok(frame.includes("Direct Messages"), "should show section title");
    assert.ok(frame.includes("Founding Agent"), "should show first item");
    assert.ok(frame.includes("SEO Analyst"), "should show second item");
    assert.ok(frame.includes("Lead Gen"), "should show third item");
  });

  it("shows collapsed triangle and hides items when collapsed", () => {
    const { lastFrame } = render(
      <SidebarSection
        title="Direct Messages"
        collapsed={true}
        items={dmItems}
        activeChannelId=""
        cursorIndex={-1}
        startIndex={0}
        onToggle={() => {}}
        onSelect={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("▸"), "should show collapsed triangle");
    assert.ok(frame.includes("Direct Messages"), "should still show title");
    assert.ok(!frame.includes("Founding Agent"), "should hide items");
    assert.ok(!frame.includes("SEO Analyst"), "should hide items");
  });

  it("marks active item correctly", () => {
    const { lastFrame } = render(
      <SidebarSection
        title="Direct Messages"
        collapsed={false}
        items={dmItems}
        activeChannelId="dm-founding"
        cursorIndex={-1}
        startIndex={0}
        onToggle={() => {}}
        onSelect={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // Active item should have > indicator
    assert.ok(frame.includes("▎"), "should show ▎ for active item");
  });
});

// ── SlackSidebar (full) ─────────────────────────────────────────────

describe("SlackSidebar", () => {
  it("renders workspace name", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex Workspace"
        sections={sections}
        collapsedSections={[]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Nex Workspace"), "should show workspace name");
  });

  it("shows DM section with agents and online dots", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={[]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Direct Messages"), "should show DM section");
    assert.ok(frame.includes("●"), "should show online dot");
    assert.ok(frame.includes("○"), "should show offline dot");
    assert.ok(frame.includes("Founding Agent"), "should show agent name");
  });

  it("shows channel section with # prefix and unread counts", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={28}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={[]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Channels"), "should show Channels section");
    assert.ok(frame.includes("# general"), "should show # prefix");
    assert.ok(frame.includes("(3)"), "should show unread count for leads");
  });

  it("highlights active item", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={[]}
        activeChannelId="dm-founding"
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("▎"),
      "should show ▎ indicator for active item",
    );
  });

  it("hides items in collapsed section", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={["Direct Messages"]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("▸"), "collapsed section should show ▸");
    assert.ok(
      !frame.includes("Founding Agent"),
      "should hide DM items when collapsed",
    );
    // Channels should still be visible
    assert.ok(frame.includes("# general"), "channels should still show");
  });

  it("shows collapsed triangle for collapsed section", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={["Channels"]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // DM section should be expanded
    assert.ok(frame.includes("▾"), "expanded section should show ▾");
    // Channel section should be collapsed
    assert.ok(frame.includes("▸"), "collapsed section should show ▸");
    assert.ok(
      !frame.includes("# general"),
      "should hide channel items when collapsed",
    );
    // DM items should still be visible
    assert.ok(
      frame.includes("Founding Agent"),
      "DM items should still show",
    );
  });

  it("shows + Add channel footer", () => {
    const { lastFrame } = render(
      <SlackSidebar
        width={24}
        focused={false}
        workspaceName="Nex"
        sections={sections}
        collapsedSections={[]}
        activeChannelId=""
        cursor={-1}
        onToggleSection={() => {}}
        onSelectItem={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("+ Add channel"), "should show add channel link");
  });
});

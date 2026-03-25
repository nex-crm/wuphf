/**
 * Slack-style sidebar component.
 *
 * Contains workspace header, collapsible DM/channel sections,
 * and individual sidebar items with online status, unread badges,
 * and active/cursor indicators.
 */

import React from "react";
import { Box, Text } from "ink";
import type { SidebarItemData } from "./sidebar-types.js";

// ── Re-export types ─────────────────────────────────────────────────
export type {
  SidebarItemData,
  SidebarSectionData,
  SidebarProps,
  SidebarSectionProps,
  SidebarItemProps,
  WorkspaceHeaderProps,
} from "./sidebar-types.js";

// ── WorkspaceHeader ─────────────────────────────────────────────────

export interface WorkspaceHeaderComponentProps {
  name: string;
  width: number;
  focused?: boolean;
}

export function WorkspaceHeader({
  name,
  width,
  focused,
}: WorkspaceHeaderComponentProps): React.JSX.Element {
  return (
    <Box flexDirection="column" width={width}>
      <Box paddingLeft={1}>
        {focused && <Text color="cyan">{"◆ "}</Text>}
        <Text bold color={focused ? "cyan" : "white"}>
          {name}
        </Text>
      </Box>
      <Box paddingLeft={1}>
        <Text dimColor>{"─".repeat(Math.max(width - 2, 1))}</Text>
      </Box>
    </Box>
  );
}

// ── SidebarItem ─────────────────────────────────────────────────────

export function SidebarItem({
  item,
  isActive,
  isCursor,
}: {
  item: SidebarItemData;
  isActive: boolean;
  isCursor: boolean;
}): React.JSX.Element {
  const isUnread = item.unread > 0;
  const isBold = isActive || isUnread;

  // Active indicator: ▎ left border for active item (spec §2.3)
  const prefix = isActive ? "▎ " : "  ";

  // Build the status dot / hash prefix
  let indicator: React.JSX.Element;
  if (item.type === "dm" || item.type === "group-dm") {
    const dotColor = item.online ? "green" : "gray";
    const dot = item.online ? "●" : "○";
    indicator = <Text color={dotColor}>{dot} </Text>;
  } else {
    indicator = <Text color="gray"># </Text>;
  }

  // Name color: active=cyan, unread=white bold, normal=white, muted=gray
  const nameColor = isActive ? "cyan" : item.muted ? "gray" : "white";

  // Unread badge
  const badge =
    item.unread > 0 ? (
      <Text color="white" dimColor={!isActive}>
        {" "}
        ({item.unread})
      </Text>
    ) : null;

  return (
    <Box>
      <Text color={isActive ? "cyan" : isCursor ? "white" : "gray"}>
        {prefix}
      </Text>
      {indicator}
      <Text color={nameColor} bold={isBold}>
        {item.name}
      </Text>
      {badge}
    </Box>
  );
}

// ── SidebarSection ──────────────────────────────────────────────────

export function SidebarSection({
  title,
  collapsed,
  items,
  activeChannelId,
  cursorIndex,
  startIndex,
  onToggle,
  onSelect,
}: {
  title: string;
  collapsed: boolean;
  items: SidebarItemData[];
  activeChannelId: string;
  cursorIndex: number;
  startIndex: number;
  onToggle: () => void;
  onSelect: (id: string) => void;
}): React.JSX.Element {
  const triangle = collapsed ? "▸" : "▾";

  return (
    <Box flexDirection="column">
      <Box paddingLeft={1}>
        <Text color="gray">
          {triangle} {title}
        </Text>
      </Box>
      {!collapsed &&
        items.map((item, idx) => {
          const flatIdx = startIndex + idx;
          return (
            <SidebarItem
              key={item.id}
              item={item}
              isActive={item.id === activeChannelId}
              isCursor={cursorIndex === flatIdx}
            />
          );
        })}
    </Box>
  );
}

// ── SlackSidebar (root) ─────────────────────────────────────────────

export function SlackSidebar({
  width,
  focused,
  workspaceName,
  sections,
  collapsedSections,
  activeChannelId,
  cursor,
  onToggleSection,
  onSelectItem,
}: {
  width: number;
  focused: boolean;
  workspaceName: string;
  sections: Array<{ title: string; items: SidebarItemData[] }>;
  collapsedSections: string[];
  activeChannelId: string;
  cursor: number;
  onToggleSection: (title: string) => void;
  onSelectItem: (id: string) => void;
}): React.JSX.Element {
  // Compute flat index offsets for each section
  let runningIndex = 0;
  const sectionOffsets: number[] = [];
  for (const section of sections) {
    sectionOffsets.push(runningIndex);
    const isCollapsed = collapsedSections.includes(section.title);
    if (!isCollapsed) {
      runningIndex += section.items.length;
    }
  }

  return (
    <Box
      flexDirection="column"
      width={width}
    >
      <WorkspaceHeader name={workspaceName} width={width} focused={focused} />
      <Box height={1} />
      {sections.map((section, sIdx) => (
        <SidebarSection
          key={section.title}
          title={section.title}
          collapsed={collapsedSections.includes(section.title)}
          items={section.items}
          activeChannelId={activeChannelId}
          cursorIndex={cursor}
          startIndex={sectionOffsets[sIdx]}
          onToggle={() => onToggleSection(section.title)}
          onSelect={onSelectItem}
        />
      ))}
      <Box height={1} />
      <Box paddingLeft={1} gap={2}>
        <Text color="gray">[n] channel</Text>
        <Text color="gray">[a] agent</Text>
      </Box>
    </Box>
  );
}

export default SlackSidebar;

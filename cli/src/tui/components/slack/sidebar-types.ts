/**
 * Type definitions for the Slack sidebar components.
 *
 * Matches the interfaces defined in docs/slack-tui-architecture.md §2.3.
 */

/** A sidebar item (channel or DM). */
export interface SidebarItemData {
  id: string;
  name: string;
  type: "channel" | "dm" | "group-dm";
  /** For channels: public or private */
  visibility?: "public" | "private";
  /** For DMs: the agent/user slugs */
  members?: string[];
  /** Online status (DMs only) */
  online?: boolean;
  /** Unread message count */
  unread: number;
  /** Has @mention in unread */
  hasMention: boolean;
  /** Muted by user */
  muted: boolean;
  /** Last message timestamp for frecency sort */
  lastActivity: number;
}

/** Section grouping for the sidebar. */
export interface SidebarSectionData {
  title: string;
  items: SidebarItemData[];
}

/** Props for the root SlackSidebar component. */
export interface SidebarProps {
  width: number;
  focused: boolean;
  workspaceName: string;
  sections: SidebarSectionData[];
  collapsedSections: string[];
  activeChannelId: string;
  cursor: number;
  onToggleSection: (title: string) => void;
  onSelectItem: (id: string) => void;
}

/** Props for SidebarSection. */
export interface SidebarSectionProps {
  title: string;
  collapsed: boolean;
  items: SidebarItemData[];
  activeChannelId: string;
  /** Index of cursor within the flattened sidebar list */
  cursorIndex: number;
  /** Starting index of this section in the flat list */
  startIndex: number;
  onToggle: () => void;
  onSelect: (id: string) => void;
}

/** Props for SidebarItem. */
export interface SidebarItemProps {
  item: SidebarItemData;
  isActive: boolean;
  isCursor: boolean;
}

/** Props for WorkspaceHeader. */
export interface WorkspaceHeaderProps {
  name: string;
}

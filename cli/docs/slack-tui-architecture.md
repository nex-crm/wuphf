# Slack TUI Component Architecture

> Complete Ink component architecture for the Slack-style TUI.
> Every component, interface, state flow, layout calculation, and keyboard binding.

---

## 1. Component Tree

```
<App>                              ← existing app.tsx (modified)
  <ThemeProvider>                   ← existing theme.tsx (extended)
    <TuiContext.Provider>           ← existing tui-context.tsx (extended)
      <SlackLayout>                ← NEW: root 3-panel layout
        <Sidebar>                  ← NEW: left panel
          <WorkspaceHeader />
          <SearchTrigger />        ← Ctrl+K hint, not a real input
          <SidebarSection title="Direct messages" defaultExpanded>
            <SidebarItem />        ← repeated: DM items (agents/humans)
          </SidebarSection>
          <SidebarSection title="Channels" defaultExpanded>
            <SidebarItem />        ← repeated: channel items
          </SidebarSection>
        </Sidebar>

        <MainPanel>                ← NEW: center panel
          <ChannelHeader />
          <MessageList>            ← NEW: replaces flat message area
            <DateSeparator />
            <UnreadMarker />
            <MessageGroup>         ← NEW: avatar + name + grouped msgs
              <Message />          ← first in group
              <ContinuationMessage /> ← compact follow-ups
            </MessageGroup>
            <SystemMessage />
          </MessageList>
          <TypingIndicator />
          <ComposeArea>            ← NEW: replaces bare TextInput
            <HintBar />           ← "Ctrl+B bold · @ mention · / cmd"
            <ComposeInput />      ← TextInput with placeholder
          </ComposeArea>
        </MainPanel>

        <ThreadPanel>              ← NEW: right panel (conditional)
          <ThreadHeader />
          <ParentMessage />
          <ReplyDivider />
          <ThreadReplyList />
          <ThreadCompose />
        </ThreadPanel>
      </SlackLayout>

      {/* Overlays (rendered outside layout, absolute positioning) */}
      <QuickSwitcher />            ← NEW: Ctrl+K modal
      <SlashAutocomplete />        ← existing (reused)
      <MentionAutocomplete />      ← existing (reused)
    </TuiContext.Provider>
  </ThemeProvider>
</App>
```

---

## 2. TypeScript Interfaces

### 2.1 Store Extensions

```typescript
// ── New state slices added to TuiState ──

/** Which panel has keyboard focus */
type FocusSection = 'sidebar' | 'messages' | 'compose' | 'thread';

/** A sidebar item (channel or DM) */
interface SidebarItemData {
  id: string;
  name: string;
  type: 'channel' | 'dm' | 'group-dm';
  /** For channels: public or private */
  visibility?: 'public' | 'private';
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

/** Thread state */
interface ThreadState {
  /** Whether thread panel is visible */
  open: boolean;
  /** The parent message ID */
  parentMessageId: string | null;
  /** Source channel/DM ID */
  sourceChannelId: string | null;
  /** Compose buffer for thread replies */
  composeValue: string;
  /** "Also send to channel" checkbox state */
  alsoSendToChannel: boolean;
}

/** Quick switcher state */
interface QuickSwitcherState {
  open: boolean;
  query: string;
  selectedIndex: number;
}

/** Extended TuiState — additions only (existing fields preserved) */
interface SlackStateExtensions {
  /** Currently active channel/DM ID */
  activeChannelId: string;
  /** Which panel has keyboard focus */
  focusSection: FocusSection;
  /** Sidebar cursor position (for keyboard nav) */
  sidebarCursor: number;
  /** Which sections are collapsed (by section title) */
  collapsedSections: string[];
  /** Message cursor in message list (-1 = none, compose focused) */
  messageCursor: number;
  /** Thread panel state */
  thread: ThreadState;
  /** Quick switcher state */
  quickSwitcher: QuickSwitcherState;
  /** Typing users per channel: channelId → sender names */
  typingUsers: Record<string, string[]>;
}
```

### 2.2 New Actions

```typescript
type SlackAction =
  // Focus
  | { type: 'SET_FOCUS'; section: FocusSection }
  // Sidebar
  | { type: 'SET_ACTIVE_CHANNEL'; channelId: string }
  | { type: 'SET_SIDEBAR_CURSOR'; cursor: number }
  | { type: 'TOGGLE_SECTION'; title: string }
  // Messages
  | { type: 'SET_MESSAGE_CURSOR'; cursor: number }
  // Thread
  | { type: 'OPEN_THREAD'; parentMessageId: string; sourceChannelId: string }
  | { type: 'CLOSE_THREAD' }
  | { type: 'SET_THREAD_COMPOSE'; value: string }
  | { type: 'TOGGLE_ALSO_SEND' }
  // Quick switcher
  | { type: 'OPEN_QUICK_SWITCHER' }
  | { type: 'CLOSE_QUICK_SWITCHER' }
  | { type: 'SET_QUICK_SWITCHER_QUERY'; query: string }
  | { type: 'SET_QUICK_SWITCHER_INDEX'; index: number }
  // Typing
  | { type: 'SET_TYPING'; channelId: string; users: string[] };
```

### 2.3 Component Props Interfaces

```typescript
// ── SlackLayout ──

interface SlackLayoutProps {
  /** Terminal columns (from useStdout) */
  cols: number;
  /** Terminal rows (from useStdout) */
  rows: number;
  /** Which section has focus */
  focusSection: FocusSection;
  /** Whether thread panel is open */
  threadOpen: boolean;
}

// ── Sidebar ──

interface SidebarProps {
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

interface SidebarSectionData {
  title: string;
  items: SidebarItemData[];
}

interface SidebarSectionProps {
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

interface SidebarItemProps {
  item: SidebarItemData;
  isActive: boolean;
  isCursor: boolean;
}

interface WorkspaceHeaderProps {
  name: string;
}

interface SearchTriggerProps {
  focused: boolean;
}

// ── MainPanel ──

interface MainPanelProps {
  channelId: string;
  channelName: string;
  channelType: 'channel' | 'dm' | 'group-dm';
  channelTopic?: string;
  memberCount?: number;
  /** Online status for DM partner */
  online?: boolean;
  messages: GroupedMessage[];
  unreadMarkerId?: string;
  messageCursor: number;
  focused: boolean;
  focusSection: FocusSection;
  typingUsers: string[];
  isLoading: boolean;
  loadingHint: string;
  /** Compose state */
  composeValue: string;
  onComposeChange: (value: string) => void;
  onSend: (content: string) => void;
  onOpenThread: (messageId: string) => void;
  /** Slash and mention autocomplete */
  slashCommands: SlashCommandEntry[];
  agents: AgentEntry[];
  /** Inline widgets from slash commands */
  picker: PickerState | null;
  confirm: ConfirmState | null;
}

interface ChannelHeaderProps {
  name: string;
  type: 'channel' | 'dm' | 'group-dm';
  topic?: string;
  memberCount?: number;
  online?: boolean;
  /** Whether the header's panel is focused */
  focused: boolean;
}

// ── Messages ──

/** A message with grouping metadata pre-computed */
interface GroupedMessage {
  id: string;
  sender: string;
  senderType: 'agent' | 'human' | 'system';
  /** 2-letter initials for avatar */
  initials: string;
  content: string;
  timestamp: number;
  /** First message in a group: shows avatar + name */
  isFirstInGroup: boolean;
  /** Thread reply info */
  threadReplyCount?: number;
  threadParticipants?: string[];
  threadLastReply?: number;
  /** Reactions */
  reactions?: ReactionData[];
  /** Edit indicator */
  edited?: boolean;
  /** System message (centered, dim) */
  isSystem?: boolean;
  /** Date separator to render BEFORE this message */
  dateSeparator?: string;
  /** Whether this is the unread marker position */
  isUnreadMarker?: boolean;
  /** Error message */
  isError?: boolean;
}

interface ReactionData {
  emoji: string;
  count: number;
  /** Current user reacted */
  reacted: boolean;
}

interface MessageGroupProps {
  messages: GroupedMessage[];
  /** The sender name (shown once at top) */
  senderName: string;
  senderInitials: string;
  senderColor: string;
  /** Whether any message in group is cursor-focused */
  focusedIndex: number;
}

interface MessageProps {
  message: GroupedMessage;
  /** Whether this message has keyboard focus */
  focused: boolean;
  onOpenThread?: () => void;
}

interface ContinuationMessageProps {
  message: GroupedMessage;
  focused: boolean;
}

interface DateSeparatorProps {
  label: string;
  width: number;
}

interface UnreadMarkerProps {
  width: number;
}

interface SystemMessageProps {
  content: string;
  timestamp: number;
}

interface TypingIndicatorProps {
  users: string[];
}

// ── ComposeArea ──

interface ComposeAreaProps {
  channelName: string;
  channelType: 'channel' | 'dm' | 'group-dm';
  /** DM recipient name for placeholder */
  recipientName?: string;
  focused: boolean;
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: string) => void;
  slashCommands: SlashCommandEntry[];
  agents: AgentEntry[];
  picker: PickerState | null;
  confirm: ConfirmState | null;
}

interface HintBarProps {
  visible: boolean;
}

// ── ThreadPanel ──

interface ThreadPanelProps {
  width: number;
  focused: boolean;
  parentMessage: GroupedMessage;
  replies: GroupedMessage[];
  sourceChannelName: string;
  sourceChannelType: 'channel' | 'dm' | 'group-dm';
  composeValue: string;
  alsoSendToChannel: boolean;
  onComposeChange: (value: string) => void;
  onSendReply: (content: string) => void;
  onToggleAlsoSend: () => void;
  onClose: () => void;
}

interface ThreadHeaderProps {
  channelName: string;
  channelType: 'channel' | 'dm' | 'group-dm';
  onClose: () => void;
}

interface ReplyDividerProps {
  count: number;
  width: number;
}

// ── QuickSwitcher ──

interface QuickSwitcherProps {
  open: boolean;
  query: string;
  selectedIndex: number;
  results: QuickSwitcherResult[];
  onQueryChange: (query: string) => void;
  onSelect: (id: string) => void;
  onClose: () => void;
}

interface QuickSwitcherResult {
  id: string;
  name: string;
  type: 'channel' | 'dm' | 'group-dm';
  online?: boolean;
  unread: number;
  /** Frecency score for ranking */
  score: number;
}
```

---

## 3. State Management Design

### 3.1 What Goes Where

```
┌─────────────────────────────────────────────────────────┐
│  STORE (TuiState — global, persists across re-renders)  │
│                                                          │
│  • activeChannelId          (which DM/channel is open)   │
│  • focusSection             (sidebar | messages | ...)   │
│  • sidebarCursor            (keyboard nav position)      │
│  • collapsedSections        (which sections folded)      │
│  • messageCursor            (focused message index)      │
│  • thread { open, parentMessageId, sourceChannelId,      │
│             composeValue, alsoSendToChannel }             │
│  • quickSwitcher { open, query, selectedIndex }          │
│  • typingUsers              (per-channel typing state)    │
│  • viewStack                (existing — kept for non-     │
│                               slack views like help)      │
│  • mode, loading, session   (existing — preserved)       │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  SERVICES (singletons — data layer, subscribe/notify)    │
│                                                          │
│  ChatService:                                            │
│  • channels list + metadata (name, type, members)        │
│  • messages per channel (the source of truth)            │
│  • unread tracking per channel                           │
│  • send/receive message routing                          │
│                                                          │
│  AgentService:                                           │
│  • managed agents (config, state, loop)                  │
│  • agent online/offline status                           │
│  • steer() and followUp() for DM routing                 │
│                                                          │
│  CalendarService:  (existing — unchanged)                │
│  OrchestrationService:  (existing — unchanged)           │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  LOCAL STATE (component-level React.useState)             │
│                                                          │
│  • composeValue (main)    — local to ComposeArea         │
│  • submitKey counter      — TextInput remount trick      │
│  • slash autocomplete     — useSlashAutocomplete hook    │
│  • mention autocomplete   — useMentionAutocomplete hook  │
│  • showBanner             — initial banner dismiss       │
│  • chatRevision counter   — triggers re-render on svc    │
│  • message grouping cache — useMemo in MessageList       │
│  • quick switcher results — derived from query + data    │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Why This Split

- **Store** holds cross-component coordination state (focus, cursor, thread). Multiple components need to react to these (sidebar highlights, main panel focus border, thread panel visibility).
- **Services** hold domain data. They already exist and use subscribe/notify. No reason to duplicate messages or channels into the store — components derive from services on each revision bump.
- **Local state** holds ephemeral UI state that only one component cares about (compose buffer, autocomplete, animation frames).

### 3.3 Initial State

```typescript
// Home starts on DM with Founding Agent (not #general)
const INITIAL_STATE_EXTENSIONS: SlackStateExtensions = {
  activeChannelId: '', // resolved at mount: first DM, or founding-agent DM
  focusSection: 'compose',  // start with compose focused (ready to type)
  sidebarCursor: 0,
  collapsedSections: [],
  messageCursor: -1,  // -1 = no message focused
  thread: {
    open: false,
    parentMessageId: null,
    sourceChannelId: null,
    composeValue: '',
    alsoSendToChannel: false,
  },
  quickSwitcher: {
    open: false,
    query: '',
    selectedIndex: 0,
  },
  typingUsers: {},
};
```

---

## 4. Layout Calculations

### 4.1 Responsive Breakpoints

```typescript
function computeLayout(cols: number, threadOpen: boolean) {
  // ≥ 120 cols: full layout with thread possible
  // 80–119: sidebar + messages, thread replaces messages
  // 60–79: narrow sidebar + messages
  // < 60: messages only (no sidebar)

  const SIDEBAR_WIDE = 28;
  const SIDEBAR_NORMAL = 24;
  const SIDEBAR_NARROW = 20;
  const THREAD_WIDTH = 45;
  const SIDEBAR_BORDER = 1; // │ separator

  if (cols < 60) {
    // Ultra-narrow: no sidebar
    return {
      sidebarWidth: 0,
      showSidebar: false,
      mainWidth: cols,
      threadWidth: 0,
      showThread: false,
    };
  }

  if (cols < 80) {
    // Narrow: skinny sidebar, no room for thread
    return {
      sidebarWidth: SIDEBAR_NARROW,
      showSidebar: true,
      mainWidth: cols - SIDEBAR_NARROW - SIDEBAR_BORDER,
      threadWidth: 0,
      showThread: false,
    };
  }

  if (cols < 120) {
    // Medium: normal sidebar. Thread replaces main (STATE C).
    if (threadOpen) {
      return {
        sidebarWidth: SIDEBAR_NORMAL,
        showSidebar: true,
        mainWidth: 0,
        threadWidth: cols - SIDEBAR_NORMAL - SIDEBAR_BORDER,
        showThread: true,
      };
    }
    return {
      sidebarWidth: SIDEBAR_NORMAL,
      showSidebar: true,
      mainWidth: cols - SIDEBAR_NORMAL - SIDEBAR_BORDER,
      threadWidth: 0,
      showThread: false,
    };
  }

  // Wide: full 3-panel layout
  if (threadOpen) {
    const sidebarW = SIDEBAR_WIDE;
    const threadW = THREAD_WIDTH;
    const mainW = cols - sidebarW - threadW - SIDEBAR_BORDER - 1; // 1 for thread border
    return {
      sidebarWidth: sidebarW,
      showSidebar: true,
      mainWidth: Math.max(mainW, 30),
      threadWidth: threadW,
      showThread: true,
    };
  }

  return {
    sidebarWidth: SIDEBAR_WIDE,
    showSidebar: true,
    mainWidth: cols - SIDEBAR_WIDE - SIDEBAR_BORDER,
    threadWidth: 0,
    showThread: false,
  };
}
```

### 4.2 Vertical Layout (rows)

```
┌─ Terminal ─────────────────────────────────┐
│ Row 1:     ChannelHeader (1 row)            │
│ Row 2:     ─── divider ───                  │
│ Row 3..N-6: MessageList (flexGrow=1)        │
│ Row N-5:   TypingIndicator (0-1 row)        │
│ Row N-4:   ─── divider ───                  │
│ Row N-3:   HintBar (1 row)                  │
│ Row N-2:   ComposeInput (1-3 rows)          │
│ Row N-1:   (status bar — if kept)           │
└─────────────────────────────────────────────┘

messageListHeight = rows - headerRows(2) - typingRow(1) - composeRows(3) - statusBar(1)
                  = rows - 7
                  = minimum 4 rows
```

### 4.3 Sidebar Vertical Layout

```
┌─ Sidebar ──────────────┐
│ WorkspaceHeader (1 row) │
│ SearchTrigger (1 row)   │
│ ─── divider ───         │
│ Section: DMs            │  ← flexGrow=1, scrollable
│   item                  │
│   item                  │
│ Section: Channels       │
│   item                  │
│   item                  │
└─────────────────────────┘

visibleSidebarItems = rows - 3 (header + search + divider)
```

---

## 5. Keyboard Navigation Map

### 5.1 Focus Section Cycling

```
Tab / F6:        compose → sidebar → messages → thread (if open) → compose
Shift+Tab:       reverse direction
```

### 5.2 Global (any focus section)

```
Ctrl+K:          Open quick switcher overlay
Ctrl+C (×1):     Cancel loading / show exit hint
Ctrl+C (×2):     Exit app
Esc:             Context-dependent (see per-section below)
```

### 5.3 Sidebar Focused

```
↑ / k:           Move cursor up
↓ / j:           Move cursor down
Enter:           Select item → switch activeChannelId, focus compose
Esc:             Focus → compose (go back to typing)
Space:           Toggle section collapse (when cursor on header)
```

### 5.4 Messages Focused

```
↑ / k:           Move message cursor up
↓ / j:           Move message cursor down
T / →:           Open thread for focused message
R:               Add reaction (future — noop initially)
Esc:             Clear cursor → focus compose
Home / gg:       Jump to top
End / G:         Jump to bottom
```

### 5.5 Compose Focused

```
Enter:           Send message (slash command or natural language)
↑ (empty input): Edit last sent message (future — noop initially)
Tab:             Cycle autocomplete / cycle focus section
Shift+Tab:       Reverse cycle autocomplete / focus section
Esc:             Dismiss autocomplete. If none visible, focus messages.
/ (at start):   Trigger slash autocomplete
@ :             Trigger mention autocomplete
```

### 5.6 Thread Panel Focused

```
↑ / k:           Scroll thread replies up
↓ / j:           Scroll thread replies down
Esc:             Close thread panel → focus compose
Tab:             Focus thread compose input
C:               Toggle "Also send to channel" checkbox
```

### 5.7 Quick Switcher (overlay — captures all input)

```
Type:            Filter results
↑ / ↓:           Navigate results
Enter:           Select → switch channel, close switcher
Esc:             Close switcher
```

### 5.8 Integration with app.tsx useInput

The existing `useInput` in `app.tsx` will be extended:

```typescript
// In app.tsx onInput callback:

// 1. Quick switcher overlay captures ALL input when open
if (state.quickSwitcher.open) {
  handleQuickSwitcherKey(input, key, state, dispatch);
  return;
}

// 2. Ctrl+K: open quick switcher (any focus section)
if (key.ctrl && key.name === 'k') {
  dispatch({ type: 'OPEN_QUICK_SWITCHER' });
  return;
}

// 3. Tab: cycle focus sections (unless autocomplete consumes it)
if (key.name === 'tab') {
  // Try autocomplete first (existing globalThis bridge)
  const tabCompleteFn = globalThis.__nexHomeTabComplete;
  if (tabCompleteFn?.(key.shift ? -1 : 1)) return;

  // Cycle focus
  cycleFocus(state, dispatch, key.shift ? -1 : 1);
  return;
}

// 4. Route to section-specific handler
switch (state.focusSection) {
  case 'sidebar':  handleSidebarKey(input, key, state, dispatch); break;
  case 'messages':  handleMessagesKey(input, key, state, dispatch); break;
  case 'compose':   // Pass through to TextInput (existing behavior)
    if (key.escape) {
      // Dismiss autocomplete first, then focus messages
      dispatch({ type: 'SET_FOCUS', section: 'messages' });
    }
    break;
  case 'thread':   handleThreadKey(input, key, state, dispatch); break;
}
```

---

## 6. Color Mapping

### 6.1 Slack Theme Tokens (extend existing theme.tsx)

```typescript
const slackTheme = {
  // Preserve existing theme tokens...
  ...theme,

  // Slack-specific additions
  slack: {
    // Sidebar
    sidebarSectionHeader: 'gray' as const,      // section titles
    sidebarItemNormal: 'white' as const,         // channel/DM name
    sidebarItemUnread: 'white' as const,         // bold applied via <Text bold>
    sidebarItemActive: 'cyan' as const,          // selected item
    sidebarItemMuted: 'gray' as const,           // muted channel
    sidebarActiveBorder: 'cyan' as const,        // ▎ left indicator
    sidebarOnlineDot: 'green' as const,          // ● online
    sidebarOfflineDot: 'gray' as const,          // ● offline
    sidebarUnreadBadge: 'white' as const,        // (N) count

    // Header
    headerChannelName: 'white' as const,
    headerHashPrefix: 'gray' as const,
    headerTopic: 'gray' as const,
    headerMemberCount: 'gray' as const,

    // Messages
    messageSender: 'white' as const,             // bold applied
    messageTimestamp: 'gray' as const,
    messageBody: 'white' as const,
    messageCode: 'yellow' as const,
    messageBlockquoteBorder: 'gray' as const,
    messageLink: 'cyan' as const,
    messageMention: 'cyan' as const,             // bg applied
    messageSystem: 'gray' as const,

    // Thread
    threadIndicator: 'cyan' as const,
    threadHeaderChannel: 'cyan' as const,
    threadClose: 'gray' as const,
    threadReplyDivider: 'gray' as const,

    // Reactions
    reactionBorder: 'gray' as const,
    reactionOwnBorder: 'cyan' as const,

    // Markers
    unreadMarker: 'red' as const,
    dateSeparator: 'gray' as const,

    // Compose
    composePlaceholder: 'gray' as const,
    composeHint: 'gray' as const,
    composeSendReady: 'green' as const,
    composeSendEmpty: 'gray' as const,

    // Quick switcher
    switcherBorder: 'gray' as const,
    switcherHighlight: 'cyan' as const,
    switcherSectionHeader: 'gray' as const,

    // Focus indicators
    focusBorder: 'cyan' as const,
    unfocusBorder: 'gray' as const,

    // Typing
    typingIndicator: 'gray' as const,
  },
} as const;
```

### 6.2 Avatar Colors

Reuse existing `agent-colors.ts` palette directly. The `getAgentColor(slug)` function already provides stable per-sender colors. For human users, use the same system — all senders get a color from the cycling palette.

### 6.3 Channel Colors

Replace the current `channel-colors.ts` usage. In Slack, channels don't have distinct colors — the `#` prefix and item text are white/gray. The active indicator is cyan. Channel colors from the existing system are retired for sidebar rendering, but may still be used for message left-borders in multi-channel views.

---

## 7. Data Flow

### 7.1 User Types → Message Sent

```
User types in ComposeArea
  ↓
ComposeArea.onChange → local state (composeValue)
  ↓ (on Enter)
ComposeArea.onSubmit(value)
  ↓
register-views.tsx handleSubmit(input):
  ├── starts with "/" → parseSlashInput → getSlashCommand → execute
  │     └── existing flow preserved (init, help, search, etc.)
  ├── init flow active → handleInitInput (existing)
  └── natural language:
        ├── if activeChannel is a DM:
        │     chatService.send(channelId, input, 'human')
        │     agentService.steer(agentSlug, input)  ← NEW: direct routing
        │     └── agent loop processes, replies via chatService
        └── if activeChannel is a channel:
              chatService.send(channelId, input, 'human')
              └── visible to all agents; agents reply in-thread
                  based on @mention or relevance detection
```

### 7.2 Agent Reply Flow

```
AgentLoop.tick()
  ↓ (agent generates response)
Agent calls tool → produces output
  ↓
chatService.route(channelId, agentSlug, content)
  ↓
chatService.notify() → all subscribers re-render
  ↓
register-views.tsx subscription:
  setChatRevision(r => r + 1)
  ↓
Messages re-derived from chatService.getMessages(channelId)
  ↓
MessageList re-renders with new message
```

### 7.3 Thread Reply Flow

```
User types in ThreadCompose
  ↓
ThreadPanel.onSendReply(content)
  ↓
chatService.sendThreadReply(channelId, parentMessageId, content, 'human')
  ↓
if alsoSendToChannel:
  chatService.send(channelId, content, 'human', { replyTo: parentMessageId })
  ↓
chatService.notify()
  ↓
ThreadPanel re-derives replies from chatService.getThreadReplies(parentMessageId)
```

### 7.4 DM vs Channel Routing

```
DM (agent-specific):
  ┌─────────┐      steer()       ┌────────────┐
  │  User   │ ──────────────────→ │ AgentLoop  │
  │ message │                     │ (targeted) │
  └─────────┘                     └─────┬──────┘
                                        │ reply
                                        ↓
                                  chatService.route()

Channel (broadcast to all agents):
  ┌─────────┐   send to channel   ┌────────────┐
  │  User   │ ───────────────────→│ ChatRouter  │
  │ message │                     │ (broadcast) │
  └─────────┘                     └─────┬──────┘
                                        │ checks @mentions
                                        │ checks relevance
                                        ↓
                                  ┌────────────┐
                                  │ Agent(s)   │
                                  │ reply in   │
                                  │ thread     │
                                  └────────────┘
```

### 7.5 Home Initialization

```
App mounts → register-views registers "home" view
  ↓
Home view adapter (register-views.tsx):
  1. Get agentService.list()
  2. Find founding-agent (or first agent)
  3. Ensure DM channel exists: chatService.ensureDMChannel(agentSlug)
  4. dispatch({ type: 'SET_ACTIVE_CHANNEL', channelId: dmChannelId })
  5. Build sidebar sections:
     a. "Direct messages": one item per agent (DM channels)
     b. "Channels": from chatService.getChannels()
  6. Render <SlackLayout>
```

---

## 8. Integration with Existing Codebase

### 8.1 What Changes

| File | Change |
|------|--------|
| `store.ts` | Add `SlackStateExtensions` to `TuiState`, new actions to reducer |
| `app.tsx` | Rewrite `onInput` for focus-section-aware key routing |
| `register-views.tsx` | Rewrite "home" view registration to render `<SlackLayout>` |
| `keybindings.ts` | Add sidebar/messages/thread key handlers; keep insert mode for compose |
| `theme.tsx` | Extend with `slack` tokens |
| `tui-context.tsx` | No change (already provides state + dispatch) |

### 8.2 What's Reused Directly

| File | Status |
|------|--------|
| `router.tsx` | Kept. SlackLayout is a registered view. Other views (help, record-list, etc.) still use PUSH_VIEW/POP_VIEW. |
| `slash-commands.ts` | Kept as-is. All commands, init flow, handleInitInput work within ComposeArea. |
| `components/slash-autocomplete.tsx` | Kept. Rendered inside ComposeArea overlay zone. |
| `components/mention-autocomplete.tsx` | Kept. Rendered inside ComposeArea overlay zone. |
| `agent-colors.ts` | Kept. Used for avatar `[XX]` background colors and sender name colors. |
| `channel-colors.ts` | Kept but usage reduced. Sidebar items use Slack color scheme instead. |
| `components/markdown.tsx` | Kept. Used inside Message component for body rendering. |
| `components/spinner.tsx` | Kept. Used for loading states and typing indicator base. |
| `components/inline-select.tsx` | Kept. Used for picker widgets in ComposeArea. |
| `components/inline-confirm.tsx` | Kept. Used for confirm widgets in ComposeArea. |
| `services/chat-service.ts` | Kept + extended with `ensureDMChannel()`, `getThreadReplies()`, `sendThreadReply()`. |
| `services/agent-service.ts` | Kept as-is. DM routing calls `steer()` on the service. |

### 8.3 What's Removed

| File/Component | Reason |
|------|--------|
| `ChannelBar` (in home-screen.tsx) | Replaced by Sidebar |
| `AgentMessage` (in home-screen.tsx) | Replaced by MessageGroup/Message |
| `CalendarStrip` (in home-screen.tsx) | Removed from home. Calendar is a separate view via /calendar. |
| `Banner` (in home-screen.tsx) | Not in Slack UI |
| `StatusBar` | Replaced by Slack-style header/compose. May keep as a minimal bottom bar. |

### 8.4 globalThis Callback Bridge

The existing pattern of registering callbacks on `globalThis` for Tab key interception continues:

```typescript
// In ComposeArea (analogous to current home-screen.tsx):
useEffect(() => {
  globalThis.__nexHomeTabComplete = (direction: number): boolean => {
    // Try slash autocomplete, then mention autocomplete
    if (slashState.visible) { /* ... */ return true; }
    if (mentionState.visible) { /* ... */ return true; }
    return false; // Not consumed → app.tsx cycles focus
  };
  return () => { delete globalThis.__nexHomeTabComplete; };
}, [slashState, mentionState]);
```

---

## 9. Message Grouping Algorithm

```typescript
/**
 * Group flat messages into display groups with date separators and unread markers.
 * Pure function — called in useMemo.
 */
function groupMessages(
  messages: ChatMessage[],
  unreadAfterTimestamp?: number,
): GroupedMessage[] {
  const result: GroupedMessage[] = [];
  let lastSender = '';
  let lastTimestamp = 0;
  let lastDate = '';
  let unreadMarkerInserted = false;

  const GROUP_WINDOW_MS = 5 * 60 * 1000; // 5 minutes

  for (const msg of messages) {
    const msgDate = new Date(msg.timestamp);
    const dateStr = msgDate.toLocaleDateString('en-US', {
      weekday: 'long', month: 'long', day: 'numeric',
    });

    // Date separator
    let dateSeparator: string | undefined;
    if (dateStr !== lastDate) {
      dateSeparator = dateStr;
      lastDate = dateStr;
    }

    // Unread marker
    let isUnreadMarker = false;
    if (!unreadMarkerInserted && unreadAfterTimestamp && msg.timestamp > unreadAfterTimestamp) {
      isUnreadMarker = true;
      unreadMarkerInserted = true;
    }

    // Grouping: same sender within 5 min = continuation
    const isFirstInGroup =
      msg.sender !== lastSender ||
      msg.timestamp - lastTimestamp > GROUP_WINDOW_MS ||
      dateSeparator !== undefined ||
      msg.senderType === 'system';

    // Compute initials
    const initials = msg.sender
      .split(/[\s-]+/)
      .slice(0, 2)
      .map(w => w[0]?.toUpperCase() ?? '')
      .join('');

    result.push({
      id: msg.id,
      sender: msg.sender,
      senderType: msg.senderType,
      initials,
      content: msg.content,
      timestamp: msg.timestamp,
      isFirstInGroup,
      isSystem: msg.senderType === 'system',
      dateSeparator,
      isUnreadMarker,
      threadReplyCount: msg.threadReplyCount,
      threadParticipants: msg.threadParticipants,
      threadLastReply: msg.threadLastReply,
      reactions: msg.reactions,
      edited: msg.edited,
      isError: msg.isError,
    });

    lastSender = msg.sender;
    lastTimestamp = msg.timestamp;
  }

  return result;
}
```

---

## 10. ASCII Mockups — WUPHF-Specific States

### 10.1 Default State: DM with Founding Agent

```
┌────────────────────────┬───────────────────────────────────────────────────┐
│ ◆ WUPHF Workspace        │ ● Founding Agent  │  Generalist AI  │ online     │
│                        ├───────────────────────────────────────────────────┤
│ 🔍 Ctrl+K to search    │                                                  │
│ ────────────────────── │ ──────────── Today, March 17 ───────────────     │
│                        │                                                  │
│ ▼ Direct messages      │  [FA]  Founding Agent                  9:00 AM  │
│ ▎ ● Founding Agent     │        Welcome to WUPHF! I'm your first AI team   │
│   ● SEO Analyst        │        member. Ask me anything, or try /help     │
│   ● Lead Generator     │        to see available commands.                │
│                        │                                                  │
│ ▼ Channels             │  [YO]  you                              9:15 AM  │
│   # general            │        What's our pipeline looking like?          │
│   # leads          (2) │                                                  │
│   # seo                │  [FA]  Founding Agent                  9:15 AM  │
│                        │        Based on the context graph, here are      │
│                        │        the key pipeline metrics: ...              │
│                        │                                                  │
│                        │        ↩ 2 replies  Last reply 5m ago            │
│                        │                                                  │
│                        ├───────────────────────────────────────────────────┤
│                        │  @ mention · / command · Ctrl+B bold             │
│                        │  > Message Founding Agent...                      │
│                        │                                       [Enter ▶] │
└────────────────────────┴───────────────────────────────────────────────────┘
```

### 10.2 Channel View with Thread Open (≥120 cols)

```
┌──────────────────┬───────────────────────────────┬──────────────────────────┐
│ ◆ WUPHF Workspace  │ # leads │ Pipeline tracking   │ Thread in #leads    [✕] │
│                  ├───────────────────────────────┤                          │
│ 🔍 Ctrl+K        │                               │ [SE]  SEO Analyst        │
│ ──────────────── │ [SE]  SEO Analyst     10:00  │       Found 3 keyword    │
│                  │       Found 3 new keyword    │       opportunities:     │
│ ▼ Direct messages│       opportunities for Q2   │       1. "wuphf ai" ...    │
│   ● Founding Agt │                               │                          │
│   ● SEO Analyst  │       ↩ 3 replies            │ ──── 3 replies ────     │
│                  │                               │                          │
│ ▼ Channels       │ [LG]  Lead Generator  10:05  │ [LG]  Lead Generator    │
│   # general      │       Cross-referencing with │       These align with   │
│ ▎ # leads    (2) │       enrichment data...     │       prospect pool.     │
│   # seo          │                               │                          │
│                  │                               │ [YO]  you               │
│                  │                               │       Great, prioritize  │
│                  │                               │       keyword #1.        │
│                  │                               │                          │
│                  ├───────────────────────────────┤ ☐ Also send to #leads   │
│                  │  > Message #leads...          │ Reply...          [Send] │
│                  │                    [Enter ▶] │                          │
└──────────────────┴───────────────────────────────┴──────────────────────────┘
```

### 10.3 Quick Switcher Overlay

```
┌────────────────────────────────────────────────────────────────────────────┐
│                                                                            │
│         ┌──────────────────────────────────────────────────┐               │
│         │  🔍 Jump to...                                    │               │
│         ├──────────────────────────────────────────────────┤               │
│         │  Recent                                          │               │
│         │  ▶ ● Founding Agent                              │               │
│         │    # general                                     │               │
│         │    ● SEO Analyst                                 │               │
│         │    # leads                                  (2)  │               │
│         │    ● Lead Generator                              │               │
│         │                                                  │               │
│         │  ────────────────────────────────────────────    │               │
│         │  Type to filter channels and direct messages     │               │
│         └──────────────────────────────────────────────────┘               │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

### 10.4 Narrow Terminal (<80 cols, thread replaces main)

```
┌──────────────────┬─────────────────────────────────────┐
│ ◆ WUPHF Workspace  │ Thread in #leads               [✕] │
│                  ├─────────────────────────────────────┤
│ 🔍 Ctrl+K        │                                     │
│ ──────────────── │ [SE]  SEO Analyst                   │
│                  │       Found 3 new keyword opps      │
│ ▼ DMs            │                                     │
│   ● Founding Agt │ ──── 3 replies ────                │
│   ● SEO Analyst  │                                     │
│                  │ [LG]  Lead Generator                │
│ ▼ Channels       │       These align with prospects.   │
│   # general      │                                     │
│   # leads    (2) │ [YO]  you                           │
│   # seo          │       Great, prioritize #1.         │
│                  │                                     │
│                  ├─────────────────────────────────────┤
│                  │ ☐ Also send to #leads              │
│                  │ Reply...                    [Send]  │
└──────────────────┴─────────────────────────────────────┘
```

---

## 11. File Organization

New files to create:

```
src/tui/
  components/
    slack/
      sidebar.tsx             ← Sidebar, SidebarSection, SidebarItem, WorkspaceHeader, SearchTrigger
      channel-header.tsx      ← ChannelHeader
      message-group.tsx       ← MessageGroup, Message, ContinuationMessage
      message-list-slack.tsx  ← SlackMessageList (grouping, date seps, unread marker, scroll)
      date-separator.tsx      ← DateSeparator
      unread-marker.tsx       ← UnreadMarker
      system-message.tsx      ← SystemMessage
      typing-indicator.tsx    ← TypingIndicator
      compose-area.tsx        ← ComposeArea, HintBar, ComposeInput
      thread-panel.tsx        ← ThreadPanel, ThreadHeader, ReplyDivider, ThreadCompose
      quick-switcher.tsx      ← QuickSwitcher (modal overlay)
      thread-indicator.tsx    ← ThreadIndicator (reply count + avatars below message)
  views/
    slack-layout.tsx          ← SlackLayout root (3-panel responsive)
  slack-keybindings.ts        ← Focus-section-aware key handlers
  slack-theme.ts              ← Slack color token extensions
  message-grouping.ts         ← groupMessages() pure function
```

Modified files:

```
src/tui/
  store.ts                    ← Add SlackStateExtensions, new actions
  app.tsx                     ← Rewrite onInput for focus-section routing
  register-views.tsx          ← Rewrite "home" to render SlackLayout
  theme.tsx                   ← Extend with slack tokens
  services/chat-service.ts    ← Add ensureDMChannel(), getThreadReplies(), sendThreadReply()
```

---

## 12. Implementation Priority

**Phase 1 (tasks #3, #4, #5 can parallelize):**

1. `store.ts` extensions + `slack-keybindings.ts` — foundation for all components
2. `sidebar.tsx` — self-contained, needs store + services
3. `message-group.tsx` + `message-list-slack.tsx` + `message-grouping.ts` — self-contained
4. `compose-area.tsx` + `thread-panel.tsx` + `quick-switcher.tsx` — self-contained

**Phase 2 (task #6):**

5. `slack-layout.tsx` — wires sidebar + main + thread with responsive layout
6. `register-views.tsx` rewrite — connects SlackLayout as "home" view
7. `app.tsx` rewrite — focus-section key routing

**Phase 3 (task #7):**

8. Design review against spec: pixel-level audit of colors, spacing, interactions

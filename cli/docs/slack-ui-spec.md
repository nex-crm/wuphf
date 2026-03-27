# Slack UI Specification — TUI Clone Reference (Ink/React)

> Exhaustive spec for reproducing Slack's 2025–2026 desktop UI in an Ink terminal app.
> Every section maps to a component or behavior that must be implemented.

---

## 1. Global Layout

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ┌─────────┐ ┌──────────────────────────────────────────────────────────┐ │
│ │         │ │  HEADER BAR                                              │ │
│ │         │ │  #channel-name  ☆  ⓘ  👤3  📌  ▼                       │ │
│ │ SIDEBAR │ ├──────────────────────────────────┬───────────────────────┤ │
│ │         │ │                                  │                       │ │
│ │ (fixed  │ │  MESSAGE LIST                    │  THREAD PANEL         │ │
│ │  width) │ │  (scrollable)                    │  (optional, slides    │ │
│ │         │ │                                  │   in from right)      │ │
│ │         │ │                                  │                       │ │
│ │         │ │                                  │                       │ │
│ │         │ ├──────────────────────────────────┤                       │ │
│ │         │ │  COMPOSE AREA                    │                       │ │
│ │         │ │  [toolbar] [input] [send]        │  [thread compose]     │ │
│ └─────────┘ └──────────────────────────────────┴───────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

### Proportions (terminal columns)
| Region          | Width                  | Height              |
|-----------------|------------------------|---------------------|
| Sidebar         | 24–30 cols (fixed)     | Full height         |
| Message list    | Remaining - thread     | flexGrow=1          |
| Thread panel    | 40–50 cols (when open) | Full height of main |
| Header bar      | Full main width        | 1–2 rows            |
| Compose area    | Full main width        | 3–6 rows (dynamic)  |

### Three-Panel State Machine
```
STATE A: sidebar + messages (default)
STATE B: sidebar + messages + thread panel
STATE C: sidebar + thread panel only (mobile-like, for narrow terminals)
```

Transition: clicking "N replies" or pressing `T` on a message → opens thread panel (STATE B).
Pressing `Esc` in thread panel → closes it (back to STATE A).
If terminal width < 80 cols → STATE C (thread replaces message list).

---

## 2. Sidebar

### 2.1 Structure (top to bottom)
```
┌─ SIDEBAR ────────────────┐
│ ◆ Workspace Name     ▼  │  ← workspace switcher dropdown
│                          │
│ 🔍 Search...             │  ← search bar / Ctrl+K trigger
│ ✏️ New Message            │  ← compose new DM/channel msg
│                          │
│ ─────────────────────────│
│ 🏠 Home                  │  ← nav tab (bold when active)
│ 💬 DMs             (3)   │  ← badge = unread count
│ 🔔 Activity        (·)   │  ← dot = has activity
│ ⏰ Later                  │
│ ⋯ More                   │
│ ─────────────────────────│
│                          │
│ ▼ Channels               │  ← collapsible section header
│   # general              │
│   # engineering     (5)  │  ← unread count badge
│   # **leads**            │  ← bold = has unreads
│   🔒 private-team        │  ← lock icon = private
│   + Add channels         │
│                          │
│ ▼ Direct messages        │  ← collapsible section header
│   🟢 Alice Johnson       │  ← green dot = online
│   ⚫ Bob Smith       (2) │  ← gray dot = offline, badge
│   🟢 Charlie, Dave       │  ← group DM, multiple names
│   + Add teammates        │
│                          │
│ ▶ Apps                   │  ← collapsed section
│                          │
│ ▼ ⭐ Starred             │  ← custom/starred section
│   # important-channel    │
│   🟢 CTO                 │
└──────────────────────────┘
```

### 2.2 Section Behavior
| Element               | Behavior                                          |
|-----------------------|---------------------------------------------------|
| Section header        | Click/Enter toggles collapse. ▼=expanded, ▶=collapsed |
| Collapse all          | Alt+click any section header collapses all         |
| Drag reorder          | Sections can be reordered (not in TUI — skip)      |
| Custom sections       | User-created sections with emoji + name            |

### 2.3 Channel Item States
```
NORMAL:       "  # channel-name"          (dim text)
UNREAD:       "  # channel-name      (5)" (bold white text + count badge)
ACTIVE:       "▎ # channel-name"          (blue left border + bold)
MUTED:        "  # channel-name"          (very dim/gray, italicized)
```

### 2.4 DM Item States
```
NORMAL:       "  🟢 Alice Johnson"         (green dot = online)
OFFLINE:      "  ⚫ Bob Smith"             (gray dot = away/offline)
UNREAD:       "  🟢 Alice Johnson    (2)"  (bold + count badge)
ACTIVE:       "▎ 🟢 Alice Johnson"         (blue left border)
TYPING:       "  🟢 Alice Johnson  ..."    (animated dots)
```

### 2.5 Sorting Rules
| Section         | Default Sort                                    |
|-----------------|------------------------------------------------|
| Channels        | Alphabetical, OR by recent activity (user pref) |
| Direct messages | Most recent conversation first (frecency)       |
| Custom sections | User-defined order                              |

### 2.6 Terminal Color Mapping
| Slack Element        | Terminal Color           |
|----------------------|--------------------------|
| Sidebar background   | None (terminal default)  |
| Section header text   | gray (dim)               |
| Channel name (normal) | white                    |
| Channel name (unread) | white bold               |
| Channel name (active) | cyan bold                |
| Active indicator      | cyan ▎ (left border)     |
| Online dot            | green ●                  |
| Offline dot           | gray ●                   |
| Unread badge          | white on gray bg         |
| Muted item            | gray dim italic          |
| Lock icon (private)   | yellow 🔒                |
| Section triangle      | gray ▼/▶                 |

---

## 3. Header Bar

### 3.1 Channel Header
```
┌─────────────────────────────────────────────────────────────┐
│  # channel-name  │  Topic: Build amazing things  │  👤 12  │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 DM Header
```
┌─────────────────────────────────────────────────────────────┐
│  🟢 Alice Johnson  │  Product Manager  │  🕐 3:42 PM local │
└─────────────────────────────────────────────────────────────┘
```

### 3.3 Group DM Header
```
┌─────────────────────────────────────────────────────────────┐
│  Alice, Bob, Charlie  │  3 members                          │
└─────────────────────────────────────────────────────────────┘
```

### 3.4 Header Elements
| Element          | Position | Description                     |
|------------------|----------|---------------------------------|
| Channel name     | Left     | # prefix for channels, name for DMs |
| Topic            | Center   | Channel topic (truncated)       |
| Member count     | Right    | 👤 N                            |
| Star/bookmark    | Right    | ☆ toggle                       |
| Info/details     | Right    | ⓘ opens detail panel           |
| Tabs row         | Below    | Messages │ Pins │ Bookmarks │ Files │ Canvas |

### 3.5 Color Mapping
| Element            | Color                  |
|--------------------|------------------------|
| Channel name       | white bold             |
| # prefix           | gray                   |
| Topic text         | gray dim               |
| Member count       | gray                   |
| Tab (active)       | cyan bold underline    |
| Tab (inactive)     | gray                   |
| Divider            | gray dim ─             |

---

## 4. Message List

### 4.1 Message Group (first message from sender)
```
┌─ message group ──────────────────────────────────────────────┐
│  [AJ]  Alice Johnson                         11:42 AM       │
│        Hey team, just pushed the new feature! 🎉             │
│        Can someone review the PR?                            │
│                                                              │
│        😄 3   👍 2   🎉 1                     │ ↩ 5 replies  │
└──────────────────────────────────────────────────────────────┘
```

### 4.2 Continuation Message (same sender within ~5 min)
```
│                                               11:43 AM       │
│        Actually, also check the tests I added                │
```
**Rules**: No avatar, no sender name. Only timestamp (shown on hover/dim).

### 4.3 Message Group Rules
| Condition                             | Result                    |
|---------------------------------------|---------------------------|
| Same sender, < 5 min since last msg   | Continuation (compact)    |
| Same sender, ≥ 5 min since last msg   | New group (avatar+name)   |
| Different sender                      | New group (avatar+name)   |
| System message                        | Centered, no avatar       |
| Date boundary                         | Date separator inserted   |

### 4.4 Date Separator
```
──────────────── Tuesday, March 17 ────────────────
```
Centered text on a horizontal rule. Appears between messages from different calendar days.

### 4.5 Unread Marker ("New" line)
```
─────────────────── New ───────────────────────────
```
Red/orange text, horizontal rule. Appears at the point where unread messages begin.
On mark-as-read (Esc), this line disappears.

### 4.6 System Messages
```
                 ✦ Alice Johnson joined #general
                 ✦ Bob pinned a message to this channel
                 ✦ Charlie set the channel topic: "Ship it"
```
Centered, italicized, dim gray text. No avatar. Preceded by ✦ or similar icon.

### 4.7 Message Elements
```
[AV]  Sender Name                              HH:MM AM/PM
      Message body text with **bold**, _italic_,
      `inline code`, and ```code blocks```.

      > Quoted text appears indented with left border

      📎 document.pdf (2.4 MB)                    ← file attachment

      😄 3  👍 2  🎉 1                            ← reaction bar
      ↩ 5 replies  [av][av][av]  Last reply 2h ago ← thread indicator

      (edited)                                     ← edit indicator
```

### 4.8 Avatar Rendering (Terminal)
```
[AJ]   ← 2-letter initials in square brackets
        Background color assigned per-user (from agent-colors palette)
```

| User Position | Colors Available                    |
|---------------|-------------------------------------|
| User 1        | cyan bg                             |
| User 2        | green bg                            |
| User 3        | yellow bg                           |
| User 4        | magenta bg                          |
| User 5        | blue bg                             |
| User 6        | red bg                              |
| Cycle          | Repeats from cyan                   |

### 4.9 Reactions Bar
```
 😄 3   👍 2   🎉 1   +
```
Each reaction: emoji + count. Highlighted if current user reacted.
`+` button to add new reaction.

Terminal rendering:
```
 :smile: 3  :+1: 2  :tada: 1  [+]
```
Or with emoji support: render actual emoji if terminal supports it.

### 4.10 Thread Indicator
```
 ↩ 5 replies  [AJ][BS][CD]  Last reply 2 hours ago
```
- Reply count
- Avatar stack (up to 3 unique repliers, initials)
- Timestamp of most recent reply
- Clickable → opens thread panel

### 4.11 Hover Actions (message toolbar)
When cursor/focus is on a message, show a floating toolbar:
```
                                    [ 😀 ] [ 💬 ] [ ↗ ] [ ⋯ ]
┌──────────────────────────────────────────────────────────────┐
│  [AJ]  Alice Johnson ...                                     │
```
| Icon | Action         | Key |
|------|----------------|-----|
| 😀   | Add reaction   | R   |
| 💬   | Reply in thread | T   |
| ↗    | Forward/share  | F   |
| ⋯    | More actions   | M   |

In TUI: show action hints when message is selected/focused.

### 4.12 Color Mapping
| Element              | Color                        |
|----------------------|------------------------------|
| Sender name          | white bold                   |
| Timestamp            | gray dim                     |
| Message body         | white                        |
| Bold text            | white bold                   |
| Italic text          | white italic                 |
| Inline code          | yellow on dark bg            |
| Code block           | yellow on dark bg, bordered  |
| Blockquote border    | gray ▎ left                  |
| Blockquote text      | gray                         |
| Link text            | cyan underline               |
| @mention             | cyan bg, white text          |
| #channel ref         | cyan underline               |
| Reaction (normal)    | gray border, emoji + count   |
| Reaction (own)       | cyan border, bold count      |
| Thread indicator     | cyan text                    |
| System message       | gray dim italic              |
| Date separator       | gray dim                     |
| Unread marker        | red bold                     |
| Edit indicator       | gray dim "(edited)"          |
| File attachment      | blue 📎 + filename           |

---

## 5. Compose Area

### 5.1 Layout
```
┌─────────────────────────────────────────────────────────────┐
│  [B] [I] [U] [S] [</>] [❝] [1.] [•] [📎] [😀] [@] [/]    │  ← toolbar
├─────────────────────────────────────────────────────────────┤
│  Message #channel-name                                      │  ← placeholder
│  |                                                          │  ← cursor
│                                                   [Send ▶]  │  ← send button
└─────────────────────────────────────────────────────────────┘
```

### 5.2 Toolbar Buttons (left to right)
| Button | Label          | Shortcut (Mac)  | Shortcut (Win)   |
|--------|----------------|-----------------|------------------|
| **B**  | Bold           | ⌘B              | Ctrl+B           |
| *I*    | Italic         | ⌘I              | Ctrl+I           |
| U̲      | Underline      | ⌘U              | (n/a)            |
| ~~S~~  | Strikethrough  | ⌘⇧X             | Ctrl+Shift+X     |
| `</>` | Code           | ⌘⇧C             | Ctrl+Shift+C     |
| ❝      | Blockquote     | ⌘⇧9             | Ctrl+Shift+9     |
| 1.     | Ordered list   | ⌘⇧7             | Ctrl+Shift+7     |
| •      | Bulleted list  | ⌘⇧8             | Ctrl+Shift+8     |
| 📎     | Attach file    | ⌘O              | Ctrl+O           |
| 😀     | Emoji picker   | ⌘⇧`             | Ctrl+Shift+`     |
| @      | Mention        | (type @)        | (type @)         |
| /      | Slash command  | (type /)        | (type /)         |

### 5.3 TUI Simplification
In the terminal, reduce toolbar to a hint line:
```
┌─────────────────────────────────────────────────────────────┐
│  Ctrl+B bold · Ctrl+I italic · @ mention · / command        │  ← hint bar
├─────────────────────────────────────────────────────────────┤
│  > |                                                        │  ← input
│                                                   [Enter ▶] │
└─────────────────────────────────────────────────────────────┘
```

### 5.4 Input Behavior
| Trigger      | Behavior                                        |
|--------------|-------------------------------------------------|
| Enter        | Send message                                    |
| Shift+Enter  | New line within message                         |
| Up arrow     | Edit last sent message (when input is empty)    |
| @            | Opens mention autocomplete popup                |
| #            | Opens channel linking autocomplete               |
| /            | Opens slash command menu                        |
| :            | Opens emoji autocomplete (e.g., `:smile:`)      |
| Esc          | Dismiss autocomplete / cancel edit              |
| Tab          | Accept autocomplete selection                   |
| ⌘Z / Ctrl+Z | Unsend last message                             |

### 5.5 @Mention Autocomplete
```
┌──────────────────────────────┐
│  People                      │
│  ▶ 🟢 Alice Johnson         │  ← highlighted
│    ⚫ Bob Smith              │
│    🟢 Charlie Davis          │
│                              │
│  Channels                    │
│    # general                 │
│    # engineering             │
└──────────────────────────────┘
```
- Appears as overlay above compose area
- Filters as user types after `@`
- Up/Down arrows to navigate, Tab/Enter to select
- Shows online status for people
- Inserts `@Alice Johnson` as formatted mention

### 5.6 Slash Command Menu
```
┌──────────────────────────────────────────┐
│  Recently used                           │
│  ▶ /remind  Set a reminder              │  ← highlighted
│    /status  Set your status             │
│    /giphy   Search for a GIF            │
│                                          │
│  All shortcuts                           │
│    /invite  Invite someone              │
│    /topic   Set channel topic           │
│    /mute    Mute this channel           │
└──────────────────────────────────────────┘
```
- Appears when `/` is typed at start of message
- Shows recently used commands first
- Filters as user types after `/`
- Each item: command name + description

### 5.7 Emoji Autocomplete
```
┌──────────────────────────────┐
│  :smi                        │
│  ▶ 😄 :smile:               │  ← highlighted
│    😊 :smiling_face:         │
│    😏 :smirk:                │
└──────────────────────────────┘
```
- Triggers on `:` followed by 2+ characters
- Shows emoji preview + name
- Tab/Enter to insert

### 5.8 Placeholder Text
| Context         | Placeholder                           |
|-----------------|---------------------------------------|
| Channel         | "Message #channel-name"               |
| DM              | "Message Alice Johnson"               |
| Group DM        | "Message Alice, Bob, Charlie"         |
| Thread reply    | "Reply..."                            |

### 5.9 Color Mapping
| Element              | Color                     |
|----------------------|---------------------------|
| Input text           | white                     |
| Placeholder text     | gray dim                  |
| Toolbar hint         | gray dim                  |
| Active toolbar btn   | cyan                      |
| Send button          | green bold (when content)  |
| Send button (empty)  | gray dim                  |
| Autocomplete bg      | dark bg (inverse)         |
| Autocomplete selected | cyan bg, white text      |
| Autocomplete normal  | white text                |
| Mention highlight    | cyan bg                   |

---

## 6. Thread Panel

### 6.1 Layout
```
┌─ THREAD ─────────────────────────────────┐
│  Thread in #channel-name           [ ✕ ] │  ← header + close
├──────────────────────────────────────────┤
│                                          │
│  [AJ]  Alice Johnson       11:42 AM     │  ← parent message
│        Original message that started     │
│        this thread                       │
│                                          │
│        😄 3  👍 2                         │  ← parent reactions
│                                          │
│  ─────── 5 replies ────────             │  ← reply count divider
│                                          │
│  [BS]  Bob Smith            11:45 AM     │  ← reply 1
│        Great idea! Let me check.         │
│                                          │
│  [CD]  Charlie Davis        11:47 AM     │  ← reply 2
│        +1, I'll review the PR            │
│                                          │
│  [AJ]  Alice Johnson       11:50 AM     │  ← reply 3
│        Thanks team! 🙏                   │
│                                          │
├──────────────────────────────────────────┤
│  ☐ Also send to #channel-name           │  ← checkbox
│  Reply...                                │  ← thread compose
│                                   [Send] │
└──────────────────────────────────────────┘
```

### 6.2 Thread Panel Behavior
| Action                        | Result                              |
|-------------------------------|-------------------------------------|
| Click "N replies" on message  | Opens thread panel to right         |
| Press T on focused message    | Opens thread panel                  |
| Click ✕ or press Esc          | Closes thread panel                 |
| Send reply                    | Appends to thread, scrolls to bottom |
| "Also send to #channel" ✓    | Reply also appears in main channel  |
| New reply arrives             | Auto-scrolls if at bottom           |

### 6.3 Thread Header
```
Thread in #channel-name                              [ ✕ ]
```
- Shows source channel/DM name
- Close button (✕) on right
- Pressing Esc focuses back to main message list

### 6.4 Reply Count Separator
```
─────────── 5 replies ───────────
```
Centered text between parent message and replies.

### 6.5 Color Mapping
| Element               | Color                    |
|-----------------------|--------------------------|
| Thread header text    | white bold               |
| Thread header channel | cyan                     |
| Close button          | gray (hover: white)      |
| Reply count separator | gray dim                 |
| Checkbox unchecked    | gray ☐                   |
| Checkbox checked      | green ☑                  |
| "Also send to"        | gray dim                 |
| Panel border          | gray dim │ (left edge)   |

---

## 7. Quick Switcher (⌘K / Ctrl+K)

### 7.1 Layout
```
┌──────────────────────────────────────────────────────────────┐
│                                                              │
│    ┌────────────────────────────────────────────────────┐    │
│    │  🔍 Jump to...                                |    │    │  ← search input
│    ├────────────────────────────────────────────────────┤    │
│    │  Recent                                            │    │
│    │  ▶ # general                                       │    │  ← highlighted
│    │    # engineering                               (3) │    │  ← unread badge
│    │    🟢 Alice Johnson                                │    │
│    │    ⚫ Bob Smith                                    │    │
│    │    # design                                        │    │
│    │                                                    │    │
│    │  ─────────────────────────────────────────────     │    │
│    │  Not finding what you need?                        │    │
│    │  Try searching for "..."                           │    │
│    └────────────────────────────────────────────────────┘    │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### 7.2 Behavior
| Action            | Result                                        |
|-------------------|-----------------------------------------------|
| ⌘K / Ctrl+K      | Opens quick switcher overlay                  |
| Type text         | Filters channels/DMs by fuzzy match           |
| Up/Down arrows    | Navigate results                              |
| Enter             | Jump to selected channel/DM                   |
| Esc               | Close quick switcher                          |

### 7.3 Result Ranking (Frecency)
Results use a frecency algorithm combining frequency and recency:

| Time Bucket   | Score |
|---------------|-------|
| Last 4 hours  | 100   |
| Last day      | 80    |
| Last 3 days   | 60    |
| Last week     | 40    |
| Last month    | 20    |
| Last 90 days  | 10    |

Formula: `Total Access Count × Bucket Score ÷ min(Timestamps, 10)`

### 7.4 Initial Display
On open (no query typed): shows up to 24 recent unread channels and DMs.

### 7.5 Search Matching
- Fuzzy substring matching: "devweb" matches "#devel-webapp"
- Initial matching for people: "dh" matches "Duretti Hirpa"
- Hyphen/underscore-separated token matching

### 7.6 Result Item Types
```
# channel-name        ← channel (# prefix, channel color)
🔒 private-channel    ← private channel (lock icon)
🟢 Person Name        ← DM with online person
⚫ Person Name        ← DM with offline person
👥 Alice, Bob, Charlie ← group DM
```

### 7.7 Color Mapping
| Element             | Color                     |
|---------------------|---------------------------|
| Overlay background  | dark bg (semi-transparent) |
| Switcher background | terminal bg               |
| Switcher border     | gray                      |
| Search input text   | white                     |
| Search placeholder  | gray dim                  |
| Result (normal)     | white                     |
| Result (highlighted)| cyan bg, white bold       |
| Result badge        | white on gray bg          |
| Section header      | gray dim uppercase        |

---

## 8. DM vs Channel vs Group DM Differences

### 8.1 Visual Comparison

| Feature            | Channel              | DM                  | Group DM             |
|--------------------|----------------------|----------------------|----------------------|
| Sidebar icon       | `#` or 🔒            | 🟢/⚫ status dot     | 👥 or stacked dots  |
| Header left        | `# name`             | `🟢 Person Name`    | `Names...`           |
| Header detail      | Topic + member count | Title/role + local time | Member count      |
| Compose placeholder| "Message #channel"   | "Message Person"     | "Message Alice, Bob" |
| Thread label       | "Thread in #channel" | "Thread with Person" | "Thread in group"    |
| Member list        | Visible via ⓘ       | Just the two users   | All members via ⓘ    |

### 8.2 DM-Specific Features
- Online/offline status indicator (green/gray dot)
- User's custom status emoji + text shown
- Local time display in header
- Typing indicator: "Alice is typing..."

### 8.3 Channel-Specific Features
- # prefix for public, 🔒 for private
- Channel topic displayed in header
- Member count badge
- "X joined #channel" system messages
- Pinned messages tab
- Channel description/purpose

### 8.4 Group DM Specifics
- Shows avatars/names of all members (up to 9)
- Can be converted to private channel
- Header shows comma-separated names
- No topic or description

---

## 9. Typing Indicators

### 9.1 In-Channel Typing
```
┌─────────────────────────────────────────┐
│  Alice Johnson is typing...             │  ← below last message, above compose
└─────────────────────────────────────────┘
```
Multiple users:
```
│  Alice and Bob are typing...            │
│  Alice, Bob, and Charlie are typing...  │
│  Several people are typing...           │  ← 4+ users
```

### 9.2 Sidebar Typing
In DM sidebar item, replace last message preview with `...` animation.

### 9.3 Color
Typing indicator text: gray dim, animated dots.

---

## 10. Keyboard Shortcuts (TUI-Relevant Subset)

### 10.1 Global Navigation
| Shortcut              | Action                              |
|-----------------------|-------------------------------------|
| Ctrl+K / ⌘K          | Open quick switcher                 |
| Ctrl+Shift+K / ⌘⇧K   | Open DM picker                     |
| Ctrl+Shift+T / ⌘⇧T   | Open threads view                  |
| Ctrl+Shift+A / ⌘⇧A   | Open all unreads                   |
| Ctrl+Shift+M / ⌘⇧M   | Open activity                      |
| Alt+↑ / Option+↑      | Previous channel/DM in sidebar     |
| Alt+↓ / Option+↓      | Next channel/DM in sidebar         |
| Alt+Shift+↑            | Previous unread channel            |
| Alt+Shift+↓            | Next unread channel                |
| Ctrl+[ / ⌘[           | Go back in history                 |
| Ctrl+] / ⌘]           | Go forward in history              |
| F6                     | Move focus to next section         |
| Shift+F6               | Move focus to previous section     |
| Ctrl+. / ⌘.           | Toggle right sidebar               |

### 10.2 Message Actions (when message is focused)
| Shortcut | Action                    |
|----------|---------------------------|
| E        | Edit message              |
| Delete   | Delete message            |
| T or →   | Open/reply to thread      |
| F        | Forward message           |
| P        | Pin/unpin message         |
| A        | Save/bookmark message     |
| U        | Mark above as unread      |
| R        | Add emoji reaction        |

### 10.3 Message Reading
| Shortcut              | Action                    |
|-----------------------|---------------------------|
| Esc                   | Mark channel as read      |
| Shift+Esc             | Mark all as read          |
| ↑ / ↓                 | Navigate between messages |
| Alt+Click / Opt+Click | Mark message as unread    |
| Ctrl+J / ⌘J          | Jump to recent unread     |

### 10.4 Compose Area
| Shortcut              | Action                    |
|-----------------------|---------------------------|
| Enter                 | Send message              |
| Shift+Enter           | New line                  |
| ↑ (empty input)       | Edit last message         |
| Ctrl+B / ⌘B          | Bold                      |
| Ctrl+I / ⌘I          | Italic                    |
| Ctrl+Shift+X / ⌘⇧X   | Strikethrough            |
| Ctrl+Shift+C / ⌘⇧C   | Code                     |
| Ctrl+Shift+9 / ⌘⇧9   | Blockquote               |
| Ctrl+Shift+7 / ⌘⇧7   | Ordered list             |
| Ctrl+Shift+8 / ⌘⇧8   | Bulleted list            |
| Ctrl+Z / ⌘Z          | Undo / unsend            |
| Ctrl+N / ⌘N          | Compose new message      |
| Ctrl+O / ⌘O          | Upload file              |

### 10.5 TUI-Specific Mappings
For terminal compatibility, map Slack shortcuts to TUI equivalents:

| Slack Shortcut | TUI Mapping      | Notes                          |
|----------------|------------------|--------------------------------|
| ⌘K             | Ctrl+K           | Quick switcher                 |
| ⌘⇧K            | Ctrl+Shift+K     | DM picker                      |
| Option+↑/↓     | Alt+↑/↓          | Channel nav                    |
| ⌘.             | Ctrl+.           | Toggle thread panel            |
| F6             | Tab              | Cycle sections (sidebar→msg→compose) |
| ⌘⇧T            | Ctrl+T           | Threads view                   |
| Esc            | Esc              | Close panel / mark read        |

---

## 11. Scrolling & Viewport

### 11.1 Message List Scroll
| Action              | Behavior                              |
|---------------------|---------------------------------------|
| Scroll up           | Load older messages (lazy)            |
| Scroll down         | Move toward present                   |
| At bottom           | Auto-scroll on new messages           |
| Not at bottom       | Show "↓ New messages" indicator       |
| Page Up/Down        | Scroll by viewport height             |
| Home/End            | Jump to top/bottom                    |

### 11.2 Jump-to-Bottom Indicator
```
                    ┌──────────────────┐
                    │  ↓ New messages  │
                    └──────────────────┘
```
Floating at bottom of message list when scrolled up and new messages arrive.
Click/Enter → scrolls to bottom and marks as read.

### 11.3 Color
| Element             | Color             |
|---------------------|-------------------|
| "New messages" btn  | cyan bg, white text |
| Scroll indicator    | gray dim          |

---

## 12. Notification Indicators

### 12.1 Sidebar Badges
```
  # channel-name      (5)     ← unread count in parentheses
  # **bold-channel**          ← bold name = has unreads (no mention count)
  🟢 Person           (2)     ← DM unread count
```

### 12.2 Badge Rules
| Condition                        | Display                       |
|----------------------------------|-------------------------------|
| Unread messages, no mentions     | Bold channel name only        |
| Unread with @mentions            | Bold + (N) count badge        |
| Muted channel with @mention      | Dim + (N) count badge         |
| Muted channel, no mention        | Normal (not bold)             |
| DM with unread                   | Bold + (N) count              |

### 12.3 Workspace Badge
Top-level workspace icon shows aggregate unread count across all channels.

---

## 13. Empty States

### 13.1 New Channel (no messages)
```
┌──────────────────────────────────────────┐
│                                          │
│         🎉 #channel-name                 │
│                                          │
│   This is the very beginning of the      │
│   #channel-name channel.                 │
│                                          │
│   Description or purpose here.           │
│                                          │
│   Alice Johnson created this channel     │
│   on March 15, 2026.                     │
│                                          │
└──────────────────────────────────────────┘
```

### 13.2 New DM (no messages)
```
┌──────────────────────────────────────────┐
│                                          │
│         🟢 Alice Johnson                 │
│         Product Manager                  │
│                                          │
│   This is the very beginning of your     │
│   direct message history with            │
│   Alice Johnson.                         │
│                                          │
└──────────────────────────────────────────┘
```

---

## 14. Focus Management & Section Cycling

### 14.1 Focus Sections (F6 / Tab cycle)
```
1. Sidebar (channel list)
2. Message list
3. Compose area
4. Thread panel (if open)
→ cycles back to 1
```

### 14.2 Focus Indicators
| Section        | Focus Indicator                         |
|----------------|-----------------------------------------|
| Sidebar        | Highlighted item with ▎ left border     |
| Message list   | Selected message has subtle bg highlight |
| Compose area   | Cursor visible, border changes to cyan  |
| Thread panel   | Border changes to cyan                  |

### 14.3 Color
| Element              | Color                    |
|----------------------|--------------------------|
| Focused section border | cyan                   |
| Unfocused section border | gray dim             |
| Focused item bg      | very subtle dark highlight |
| Cursor               | white block/line         |

---

## 15. Component Architecture (Ink Implementation)

### 15.1 Component Tree
```
<SlackApp>
  <SlackLayout>
    <Sidebar>
      <WorkspaceHeader />
      <SearchBar />
      <NavTabs />                    {/* Home, DMs, Activity, Later, More */}
      <SidebarSections>
        <SidebarSection title="Channels">
          <ChannelItem />            {/* repeated */}
        </SidebarSection>
        <SidebarSection title="Direct messages">
          <DMItem />                 {/* repeated */}
        </SidebarSection>
        <SidebarSection title="Apps">
          <AppItem />
        </SidebarSection>
      </SidebarSections>
    </Sidebar>

    <MainPanel>
      <ChannelHeader />
      <MessageList>
        <DateSeparator />
        <UnreadMarker />
        <MessageGroup>
          <Message />                {/* first in group: avatar + name */}
          <ContinuationMessage />    {/* compact: just text */}
        </MessageGroup>
        <SystemMessage />
        <JumpToBottom />
      </MessageList>
      <TypingIndicator />
      <ComposeArea>
        <FormatToolbar />            {/* hint line in TUI */}
        <MessageInput />
        <SendButton />
      </ComposeArea>
    </MainPanel>

    <ThreadPanel>                    {/* conditionally rendered */}
      <ThreadHeader />
      <ParentMessage />
      <ReplyCountDivider />
      <ThreadReplyList>
        <Message />
      </ThreadReplyList>
      <AlsoSendCheckbox />
      <ThreadCompose />
    </ThreadPanel>
  </SlackLayout>

  {/* Overlays */}
  <QuickSwitcher />                  {/* modal overlay */}
  <MentionAutocomplete />            {/* popup above compose */}
  <SlashCommandMenu />               {/* popup above compose */}
  <EmojiPicker />                    {/* popup above compose */}
</SlackApp>
```

### 15.2 State Management
```typescript
interface SlackState {
  // Navigation
  activeChannel: string;
  focusedSection: 'sidebar' | 'messages' | 'compose' | 'thread';

  // Sidebar
  sidebarSections: SidebarSection[];
  collapsedSections: Set<string>;
  selectedSidebarIndex: number;

  // Messages
  messages: Map<string, Message[]>;        // channelId → messages
  unreadMarkers: Map<string, string>;      // channelId → messageId
  selectedMessageIndex: number;
  scrollPosition: 'bottom' | number;

  // Thread
  threadOpen: boolean;
  threadParentId: string | null;
  threadReplies: Message[];

  // Compose
  composeValue: string;
  threadComposeValue: string;
  editingMessageId: string | null;

  // Autocomplete
  autocomplete: {
    type: 'mention' | 'slash' | 'emoji' | 'channel' | null;
    query: string;
    results: AutocompleteItem[];
    selectedIndex: number;
  };

  // Quick Switcher
  quickSwitcherOpen: boolean;
  quickSwitcherQuery: string;
  quickSwitcherResults: SwitcherItem[];
  quickSwitcherIndex: number;

  // Typing
  typingUsers: Map<string, string[]>;      // channelId → user names
}
```

### 15.3 Key Props Interfaces
```typescript
interface MessageProps {
  id: string;
  sender: { name: string; initials: string; online?: boolean };
  content: string;
  timestamp: Date;
  isFirstInGroup: boolean;
  reactions?: { emoji: string; count: number; reacted: boolean }[];
  threadReplyCount?: number;
  threadParticipants?: { initials: string }[];
  threadLastReply?: Date;
  edited?: boolean;
  isSystem?: boolean;
  attachments?: { name: string; size: string }[];
}

interface ChannelItemProps {
  name: string;
  isPrivate: boolean;
  unreadCount: number;
  hasMention: boolean;
  isActive: boolean;
  isMuted: boolean;
}

interface DMItemProps {
  name: string;
  isOnline: boolean;
  unreadCount: number;
  isActive: boolean;
  isGroupDM: boolean;
  members?: string[];
  isTyping: boolean;
}

interface ThreadPanelProps {
  parentMessage: MessageProps;
  replies: MessageProps[];
  channelName: string;
  onClose: () => void;
  onSendReply: (text: string, alsoSendToChannel: boolean) => void;
}

interface QuickSwitcherProps {
  isOpen: boolean;
  onSelect: (item: SwitcherItem) => void;
  onClose: () => void;
  channels: ChannelItemProps[];
  dms: DMItemProps[];
}
```

---

## 16. Existing Codebase Reuse Map

Map existing TUI components to Slack UI components:

| Existing Component      | Slack Component          | Reuse Strategy             |
|-------------------------|--------------------------|----------------------------|
| `ChatInput`             | `MessageInput`           | Extend with @mention, /cmd |
| `MessageList`           | `MessageList`            | Refactor for grouping      |
| `ChannelMessage`        | `Message`                | Adapt colored borders      |
| `Picker`                | `QuickSwitcher` results  | Reuse cursor navigation    |
| `SlashAutocomplete`     | `SlashCommandMenu`       | Direct reuse               |
| `MentionAutocomplete`   | `MentionAutocomplete`    | Direct reuse               |
| `StatusBar`             | (remove or repurpose)    | Not in Slack UI            |
| `Markdown`              | Message body renderer    | Direct reuse               |
| `Spinner`               | Typing indicator         | Adapt animation            |
| `Banner`                | (remove)                 | Not in Slack UI            |
| `Viewport`              | Message scroll container | Reuse scroll logic         |
| `agent-colors`          | User avatar colors       | Direct reuse               |
| `channel-colors`        | Channel accent colors    | Direct reuse               |
| `theme.tsx`             | Slack theme tokens       | Extend with Slack colors   |
| `router.tsx`            | View navigation          | Keep for view switching    |
| `store.ts`              | State management         | Extend with SlackState     |
| `keybindings.ts`        | Keyboard handler         | Rewrite for Slack shortcuts |
| `tui-context.tsx`       | App-wide context         | Extend                     |

---

## 17. Terminal Rendering Constraints

### 17.1 Character Substitutions
| Slack GUI Element | Terminal Rendering            |
|-------------------|-------------------------------|
| User avatar       | `[XX]` initials with bg color |
| Online dot        | `●` green                     |
| Offline dot       | `●` gray                      |
| Lock icon         | `🔒` or `*` fallback          |
| Star icon         | `★` / `☆`                    |
| Pin icon          | `📌` or `^` fallback          |
| Reaction emoji    | Emoji char or `:name:` text   |
| Checkbox          | `☐` / `☑` or `[ ]` / `[x]`  |
| Close button      | `[x]` or `✕`                 |
| Expand/collapse   | `▼` / `▶`                    |
| Left border       | `▎` (thin vertical bar)       |
| Horizontal rule   | `─` repeated                  |
| Send button       | `[Enter ▶]` or `[Send]`      |

### 17.2 Width Breakpoints
| Terminal Width | Layout                                |
|----------------|---------------------------------------|
| ≥ 120 cols     | Sidebar (28) + Messages + Thread (45) |
| 80–119 cols    | Sidebar (24) + Messages               |
| 60–79 cols     | Sidebar (20) + Messages (condensed)   |
| < 60 cols      | Single panel (messages only)          |

### 17.3 Color Depth
Target 256-color terminals. Degrade gracefully to 16-color:

| 256-color        | 16-color fallback |
|-------------------|-------------------|
| #2980fb (brand)   | blue              |
| #03a04c (success) | green             |
| #e23428 (error)   | red               |
| #df750c (warning) | yellow            |
| #838485 (muted)   | gray              |
| #cfd0d2 (text)    | white             |

---

## 18. Animation & Transitions

### 18.1 Typing Indicator Animation
```
Frame 1: "Alice is typing"
Frame 2: "Alice is typing."
Frame 3: "Alice is typing.."
Frame 4: "Alice is typing..."
→ loop, 300ms per frame
```

### 18.2 Thread Panel Slide-In
In terminal: instant render (no slide animation). Use border change to indicate new panel.

### 18.3 Unread Marker Fade
On mark-as-read: remove the "── New ──" line immediately.

### 18.4 New Message Highlight
Brief highlight (1–2 render cycles) on newly received messages with a subtle background color, then fade to normal.

---

## Appendix A: Complete Color Reference

| Token Name           | Hex (GUI)  | Terminal (256) | Terminal (16) | Usage                    |
|----------------------|------------|----------------|---------------|--------------------------|
| brand-blue           | #2980fb    | 33             | blue          | Active items, links      |
| sidebar-bg           | #1a1d21    | 234            | (default)     | Sidebar background       |
| sidebar-text         | #cfd0d2    | 252            | white         | Normal sidebar text      |
| sidebar-text-active  | #ffffff    | 15             | bright white  | Active/selected item     |
| sidebar-text-muted   | #838485    | 244            | gray          | Muted items              |
| message-text         | #d1d2d3    | 252            | white         | Message body             |
| message-sender       | #ffffff    | 15             | bright white  | Sender name              |
| message-timestamp    | #838485    | 244            | gray          | Timestamp text           |
| mention-bg           | #1d9bd1    | 32             | cyan          | @mention highlight       |
| mention-text         | #ffffff    | 15             | bright white  | @mention text            |
| link                 | #1d9bd1    | 32             | cyan          | Hyperlinks               |
| code-bg              | #2d2d2d    | 236            | (default)     | Code block background    |
| code-text            | #e8912d    | 172            | yellow        | Code text                |
| blockquote-border    | #838485    | 244            | gray          | Quote left border        |
| unread-badge-bg      | #e01e5a    | 161            | red           | Unread mention badge     |
| unread-badge-text    | #ffffff    | 15             | bright white  | Badge count text         |
| online-green         | #2bac76    | 35             | green         | Online status dot        |
| offline-gray         | #838485    | 244            | gray          | Offline status dot       |
| hover-bg             | #222529    | 235            | (default)     | Hovered message bg       |
| selected-bg          | #1164a3    | 25             | blue          | Selected/focused item    |
| separator            | #3d3c3d    | 237            | gray          | Divider lines            |
| system-msg           | #838485    | 244            | gray          | System/join/leave msgs   |
| error                | #e23428    | 160            | red           | Error states             |
| success              | #03a04c    | 34             | green         | Success states           |
| warning              | #df750c    | 172            | yellow        | Warning states           |
| reaction-border      | #3d3c3d    | 237            | gray          | Reaction pill border     |
| reaction-active-border | #1d9bd1  | 32             | cyan          | Own reaction pill border |
| thread-indicator     | #1d9bd1    | 32             | cyan          | Thread reply link        |
| typing-indicator     | #838485    | 244            | gray          | "is typing..." text     |

---

## Appendix B: ASCII Layout — All States

### B.1 Default State (channel selected, no thread)
```
┌────────────────────┬───────────────────────────────────────────────────┐
│ ◆ WUPHF Workspace  ▼│ # general  │  Build amazing things  │  👤 12    │
│                    ├───────────────────────────────────────────────────┤
│ 🔍 Search...       │                                                  │
│                    │ ──────────── Tuesday, March 17 ──────────────    │
│ 🏠 Home            │                                                  │
│ 💬 DMs         (3) │  [AJ]  Alice Johnson                  9:15 AM   │
│ 🔔 Activity        │        Good morning team! Ready for standup?     │
│ ⏰ Later            │                                                  │
│                    │        😄 3  👍 1                                │
│ ▼ Channels         │                                                  │
│ ▎ # general        │  [BS]  Bob Smith                      9:17 AM   │
│   # engineering (5)│        Yep! Let me share my screen.              │
│   # design         │                                       9:18 AM   │
│   🔒 private       │        Actually, can we push 5 min?             │
│   + Add channels   │                                                  │
│                    │  [CD]  Charlie Davis                   9:20 AM   │
│ ▼ Direct messages  │        Sure, no rush.                            │
│   🟢 Alice Johnson │                                                  │
│   ⚫ Bob Smith  (2)│        ↩ 3 replies  [AJ][BS]  Last reply 10m ago│
│   🟢 Charlie, Dave │                                                  │
│   + Add teammates  │ ──────────────── New ────────────────────       │
│                    │                                                  │
│ ▶ Apps             │  [EF]  Eve Foster                     9:35 AM   │
│                    │        @Alice Johnson can you review PR #142?    │
│                    │                                                  │
│                    │                                                  │
│                    ├───────────────────────────────────────────────────┤
│                    │  Alice is typing...                               │
│                    ├───────────────────────────────────────────────────┤
│                    │  Ctrl+B bold · Ctrl+I italic · @ mention · / cmd │
│                    │  Message #general                                 │
│                    │  |                                    [Enter ▶]  │
└────────────────────┴───────────────────────────────────────────────────┘
```

### B.2 Thread Panel Open
```
┌──────────────┬──────────────────────────────┬────────────────────────┐
│ ◆ Workspace ▼│ # general │ Topic │ 👤 12   │ Thread #general   [✕] │
│              ├──────────────────────────────┤                        │
│ 🔍 Search... │                              │ [AJ]  Alice Johnson    │
│              │  [AJ]  Alice Johnson  9:15   │       Good morning     │
│ 🏠 Home      │        Good morning team!    │       team!            │
│ 💬 DMs   (3) │        😄 3  👍 1            │       😄 3  👍 1       │
│ 🔔 Activity  │                              │                        │
│              │  [BS]  Bob Smith      9:17   │ ──── 3 replies ────   │
│ ▼ Channels   │        Yep!                  │                        │
│ ▎ # general  │                              │ [BS]  Bob Smith        │
│   # eng  (5) │  [CD]  Charlie Davis  9:20   │       Sounds good!     │
│   # design   │        Sure, no rush.        │                        │
│              │        ↩ 3 replies ←(open)   │ [CD]  Charlie          │
│ ▼ DMs        │                              │       Count me in.     │
│   🟢 Alice   │                              │                        │
│   ⚫ Bob (2) │                              │ [AJ]  Alice Johnson    │
│              │                              │       Great, see you!  │
│              │                              │                        │
│              ├──────────────────────────────┤ ☐ Also send to #general│
│              │  Message #general            │ Reply...               │
│              │  |                  [Enter ▶]│                 [Send] │
└──────────────┴──────────────────────────────┴────────────────────────┘
```

### B.3 Quick Switcher Overlay
```
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│            ┌──────────────────────────────────────────┐              │
│            │  🔍 Jump to...                           │              │
│            ├──────────────────────────────────────────┤              │
│            │  Recent                                  │              │
│            │  ▶ # general                             │              │
│            │    # engineering                    (5)  │              │
│            │    🟢 Alice Johnson                      │              │
│            │    ⚫ Bob Smith                          │              │
│            │    # design                              │              │
│            │    🟢 Charlie, Dave                      │              │
│            │                                          │              │
│            │  Not finding what you need?              │              │
│            │  Try browsing all channels               │              │
│            └──────────────────────────────────────────┘              │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### B.4 Mention Autocomplete Active
```
┌───────────────────────────────────────────────────────┐
│                                                       │
│  [CD]  Charlie Davis                   9:20 AM       │
│        Sure, no rush.                                │
│                                                       │
│  ┌─────────────────────────────────────┐              │
│  │  People                             │              │
│  │  ▶ 🟢 Alice Johnson                │              │
│  │    ⚫ Bob Smith                     │              │
│  │    🟢 Charlie Davis                │              │
│  └─────────────────────────────────────┘              │
├───────────────────────────────────────────────────────┤
│  Hey @ali|                                            │
│                                           [Enter ▶]  │
└───────────────────────────────────────────────────────┘
```

### B.5 Empty Channel State
```
┌────────────────────┬──────────────────────────────────────────────────┐
│ ◆ Workspace     ▼ │ # new-project  │  │  👤 2                       │
│                    ├──────────────────────────────────────────────────┤
│ ...sidebar...      │                                                  │
│                    │                                                  │
│                    │            🎉 #new-project                       │
│                    │                                                  │
│                    │   This is the very beginning of the              │
│                    │   #new-project channel.                          │
│                    │                                                  │
│                    │   Alice Johnson created this channel             │
│                    │   on March 15, 2026.                             │
│                    │                                                  │
│                    │                                                  │
│                    ├──────────────────────────────────────────────────┤
│                    │  Message #new-project                            │
│                    │  |                                    [Enter ▶]  │
└────────────────────┴──────────────────────────────────────────────────┘
```

---

## Appendix C: Interaction Flow Diagrams

### C.1 Sending a Message
```
User types in compose → content appears in input
User presses Enter → message sent
  → message appears at bottom of message list
  → input clears
  → auto-scroll to bottom
  → typing indicator clears
```

### C.2 Opening a Thread
```
User focuses message with thread indicator
User presses T or clicks "N replies"
  → thread panel slides in from right
  → parent message shown at top
  → replies loaded below
  → thread compose area at bottom
  → focus moves to thread compose
```

### C.3 Using Quick Switcher
```
User presses Ctrl+K
  → overlay appears with search input focused
  → recent channels/DMs shown (up to 24)
User types query
  → results filter by fuzzy match + frecency
User presses Enter on result
  → overlay closes
  → navigates to selected channel/DM
  → focus moves to compose area
```

### C.4 @Mentioning
```
User types @ in compose area
  → mention autocomplete popup appears
  → shows all users, filtered as user types
User navigates with ↑/↓
User presses Tab or Enter
  → @Name inserted into message
  → popup closes
  → mention rendered with highlight color
```

### C.5 Switching Channels
```
User presses Alt+↑/↓ in sidebar
  → selection moves to prev/next channel
User presses Enter
  → channel activates (▎ indicator)
  → message list loads channel messages
  → compose placeholder updates
  → unread markers shown if applicable
  → header updates with channel info
```

---

*This spec is the authoritative reference for building the Slack-like TUI.
Every component, color, shortcut, and behavior described here must be
implemented to achieve UI parity.*

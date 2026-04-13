# Slack Clone Affordance Audit

**Date:** 2026-04-13
**Branch:** `feature/slack-ux-redesign`
**Reference:** Mattermost webapp (`/tmp/mattermost-ref/webapp/channels/src/components/`)

## Context

Visual CSS changes were made to restyle WUPHF as a Slack clone (aubergine sidebar,
system fonts, Slack color palette, etc.). This audit covers what's left: the
**affordances and interactions** that make something feel like Slack vs. just look
like it.

Two types of gaps identified:
1. **Missing entirely** -- Slack has it, WUPHF doesn't
2. **Exists but behaves differently** -- WUPHF has it, but the interaction doesn't match Slack

Mattermost (open-source Slack clone, 351 components) used as the reference implementation
for how these affordances should work in production.

---

## Part 1: What Exists But Works Differently

These are things WUPHF already implements but the behavior diverges from Slack.

### 1.1 Message Hover Toolbar

**Slack/Mattermost:** Hovering a message shows a toolbar at top-right with 5+ actions:
emoji reaction, reply in thread, share/forward, bookmark/save, and "..." more menu.
Mattermost's `DotMenu` (`dot_menu/dot_menu.tsx`) renders ~15 actions including reply,
forward, react, follow thread, mark unread, save, remind, pin, move, copy, edit, delete.

**WUPHF:** Toolbar appears on hover (opacity transition, `line 814`) but contains only
a **single Quote button** (`line 4000`). Missing: emoji react, thread reply shortcut,
bookmark, share, edit, delete, and the "..." overflow menu.

**Impact:** High. The hover toolbar IS the primary message interaction in Slack. Users
reach for it on every message.

### 1.2 Thread Reply Indicator

**Slack/Mattermost:** Below a threaded message, shows "[N replies]" as a clickable link
with a small horizontal avatar stack of thread participants, last reply timestamp, and
follow/unfollow button. Mattermost implements this in `thread_footer/thread_footer.tsx`.

**WUPHF:** Shows two separate buttons below the message (`line 4060`):
1. Inline thread toggle (chevron + "N replies") for expanding replies in-place
2. Thread panel button ("Open thread") for the side panel

No avatar stack. No participant indicator. No follow/unfollow. The inline expansion
is actually a feature Slack doesn't have (Slack always uses the side panel), so it's
a differentiation point.

**Impact:** Medium. The dual-button approach is functional but visually different from
what Slack users expect.

### 1.3 Unread Channel State in Sidebar

**Slack/Mattermost:** Unread channels appear BOLD with a white dot indicator. The sidebar
also shows an "N unread channels" floating indicator when unread channels are scrolled
out of view. Mattermost uses `unread-title` CSS class for bold text and
`ChannelMentionBadge` for red number badges, plus `unread_channel_indicator/` component.

**WUPHF:** Active channel gets subtle background highlight (`rgba(0,0,0,0.06)`, `line 659`).
Badge count exists (`line 1010`, `.sidebar-badge`) but channel names don't go bold when
unread. No floating "N unread" indicator.

**Impact:** Medium-high. Unread bold is one of the strongest visual signals in Slack.
Users scan the sidebar for bold channel names to know where action is.

### 1.4 Command Palette (Cmd+K)

**Slack:** "Jump to..." dialog. Primarily navigation. Shows recent conversations,
channels, people. Search is secondary.

**Mattermost:** Similar navigation-first approach in `channel_navigator/`.

**WUPHF:** Universal search/command palette (`line 5718`). Searches channels, agents,
slash commands, AND message content. More like VS Code's Cmd+P than Slack's Cmd+K.

**Impact:** Low. WUPHF's approach is arguably better since it combines navigation and
search. Not a problem, just different.

### 1.5 Reactions

**Slack/Mattermost:** Reaction pills show emoji + count. Hovering shows tooltip with
who reacted. Clicking your own reaction removes it. A "+" button at the end of the
reaction row opens the emoji picker to add new reactions. Mattermost implements this
across `reaction_list/`, `reaction/reaction.tsx`, and `emoji_picker/`.

**WUPHF:** Reaction pills display and clicking toggles via `toggleReaction()` (`line 3989`).
No "+" button. No emoji picker. No tooltip showing who reacted.

**Impact:** Medium. You can toggle existing reactions but can't add new ones without
an emoji picker.

### 1.6 Date Separators

**Slack/Mattermost:** Centered text between horizontal lines showing "Today",
"Yesterday", or the date. **Sticky** -- stays visible at the top of the viewport as
you scroll through messages. Mattermost implements this in `floating_timestamp/`.

**WUPHF:** Same visual layout (`line 2012`): centered text between horizontal lines,
"Today"/"Yesterday"/date. But **not sticky** -- scrolls with the messages.

**Impact:** Low. Nice polish but not critical.

### 1.7 Scroll State Indicators

**Slack/Mattermost:** When scrolled up from the bottom, a "Jump to latest" button
appears. New messages arriving while scrolled up show a floating "N new messages"
indicator. Mattermost has `scroll_to_bottom_arrows.tsx` and new message separator.

**WUPHF:** Auto-scrolls to bottom on new messages (`line 3821`:
`container.scrollTop = container.scrollHeight`). No "Jump to latest" button.
Unread divider exists (`line 3786`) but no floating "new messages" toast.

**Impact:** Medium. In active channels with fast-moving conversation, losing your
scroll position is frustrating.

### 1.8 Workspace Header Menu

**Slack/Mattermost:** Clicking workspace name opens dropdown with: set status,
pause notifications, profile, preferences, sign out, invite people, team settings.
Mattermost's `sidebar_team_menu.tsx` has invite, settings, members, leave team, etc.

**WUPHF:** Workspace header shows "WUPHF" text and a collapse button (`line 1714`).
No dropdown menu. No status setting. No preferences access.

**Impact:** Medium. Users expect to click the workspace name for settings/profile.

### 1.9 Thread Panel "Also Send to Channel"

**Slack:** Thread composer has an "Also send to #channel" checkbox below the input.

**Mattermost:** Implements this as a channel-level preference
(`channel_auto_follow_threads`) rather than per-reply checkbox.

**WUPHF:** Thread panel has a simple composer with no checkbox (`line 1836`). Replies
go only to the thread.

**Impact:** Low-medium. Useful for important thread conclusions that should be
visible to the whole channel.

### 1.10 Composer Up-Arrow Behavior

**Slack:** Pressing Up in an empty composer edits your last sent message (opens it
for editing inline).

**WUPHF:** Pressing Up in empty composer loads the last message from `composerHistory`
array into the input field (`line 5995`). This is **composer history recall**, not
**message editing**. The message isn't edited in-place -- it's re-sent.

**Impact:** Medium for power users. Different mental model.

---

## Part 2: What's Missing Entirely

### Tier 1: Core (notice in 30 seconds)

| # | Feature | Mattermost Reference | Notes |
|---|---------|---------------------|-------|
| 1 | **Emoji picker** | `emoji_picker/emoji_picker.tsx` -- searchable, categorized, skin tones, recent tracking | Needed for reactions AND composer |
| 2 | **Formatting toolbar** in composer | `formatting_bar/formatting_bar.tsx` -- B, I, S, code, quote, lists, link | Floating bar above composer on focus |
| 3 | **Full message hover toolbar** | `dot_menu/dot_menu.tsx` + `post_options.tsx` -- 15 actions | Currently only 1 action (quote) |
| 4 | **Message editing** | Edit via hover menu or Up arrow | No edit capability at all |
| 5 | **File/image sharing** | `file_upload/`, drag-drop, paste, previews | Not relevant for AI agents? |
| 6 | **Sidebar section collapse** | `sidebar_category.tsx` -- collapsible with animation | Chevron triangles on each section |
| 7 | **Starred/favorite channels** | Separate "Favorites" category at top of sidebar | Drag channel to star it |

### Tier 2: Important (notice in 5 minutes)

| # | Feature | Mattermost Reference | Notes |
|---|---------|---------------------|-------|
| 8 | **Right-click context menus** | Mattermost doesn't have these either (uses hover toolbar) | Can skip -- even Mattermost skipped it |
| 9 | **Link unfurling** (URL previews) | `post_attachment_opengraph/` | Low priority for AI agent tool |
| 10 | **Message pinning** | Pin action in DotMenu | Useful for important decisions |
| 11 | **Message deletion** | Delete in DotMenu + confirmation | Human messages should be deletable |
| 12 | **Channel bookmarks bar** | `channel_bookmarks/` -- links/files pinned below header | Nice but not critical |
| 13 | **Search filters** (from:, in:, before:) | `new_search/` with operators | Current search is basic text only |
| 14 | **"Jump to latest" scroll button** | `scroll_to_bottom_arrows.tsx` | Floating button when scrolled up |
| 15 | **Notification prefs per channel** | `channel_notifications_modal/` | Mute, all, mentions only |

### Tier 3: Polish

| # | Feature | Notes |
|---|---------|-------|
| 16 | Custom user status (emoji + text) | Not relevant for AI agents |
| 17 | Channel browser modal | Cmd+K already covers this |
| 18 | Mark as read/unread | Nice power-user feature |
| 19 | Sidebar drag-to-resize | CSS resize handle |
| 20 | Virtualized message list | Mattermost uses `post_list_virtualized.tsx` for perf |

---

## Part 3: WUPHF-Specific Features (Keep, Don't Exist in Slack/Mattermost)

These are WUPHF affordances that neither Slack nor Mattermost has. They should be
preserved and styled to feel native within the Slack visual language.

- **Runtime strip** -- active/blocked/need-you status pills
- **Live work cards** -- which agent is doing what, right now
- **Needs-you banner** -- human escalation with action buttons
- **Task board** -- Kanban view of agent work
- **Agent profile panel** -- skills, stream, DM
- **DM stream strip** -- live agent stdout (terminal output)
- **Recovery/rewind system** -- checkpoint/restore workspace state
- **Focus/collab modes** -- delegation vs collaborative agent behavior
- **Mood inference** -- emoji indicator on agent messages
- **Pixel art avatars** -- agent identity
- **Policy management** -- operational rules
- **Agent wizard** -- create/configure new agents
- **Inline thread expansion** -- expand thread replies in-place (Slack doesn't have this)
- **Interview/poll modal** -- structured human input collection
- **Slash commands** -- `/focus`, `/collab`, `/recover`, `/rewind`, `/doctor`, etc.

---

## Part 4: Recommended Priority Order

Based on impact-to-effort ratio for making WUPHF feel like Slack:

### Phase 1: The Big Three (closes 60% of the gap)

1. **Full message hover toolbar** -- Add emoji, thread, and "..." buttons. Wire emoji
   to a picker, thread to `openThread()`, "..." to a dropdown with edit/delete/pin/copy.
2. **Emoji picker component** -- Searchable grid. Triggered from hover toolbar AND
   composer. Can start simple (flat grid of common emoji) and enhance later.
3. **Sidebar section collapse** -- Add chevron toggles to Team, Channels, Apps sections.
   Persist collapsed state in localStorage.

### Phase 2: Feels Right (closes another 20%)

4. **Unread channel bold** -- Bold channel names + white dot when unread.
5. **Formatting toolbar** in composer -- B, I, ~~S~~, code, quote, list buttons.
6. **"Jump to latest" button** -- Floating arrow when scrolled up from bottom.
7. **Workspace header dropdown** -- Click workspace name for a menu.

### Phase 3: Power User Polish (final 20%)

8. **Message editing** -- Up arrow edits last message in-place.
9. **Message deletion** -- From hover toolbar "..." menu.
10. **Thread reply indicator** with avatar stack.
11. **"Also send to channel"** checkbox in thread composer.
12. **Search filters** -- from:, in:, before:, after: operators.

---

## Mattermost Reference Files

Key Mattermost components to study when implementing:

| Feature | Mattermost Path |
|---------|----------------|
| Hover toolbar | `components/dot_menu/dot_menu.tsx`, `components/post/post_options.tsx` |
| Emoji picker | `components/emoji_picker/emoji_picker.tsx` |
| Formatting bar | `components/advanced_text_editor/formatting_bar/formatting_bar.tsx` |
| Sidebar categories | `components/sidebar/sidebar_category/sidebar_category.tsx` |
| Unread indicators | `components/sidebar/sidebar_channel/sidebar_channel_link/` |
| Thread footer | `components/threading/channel_threads/thread_footer/thread_footer.tsx` |
| Scroll arrows | `components/post_view/scroll_to_bottom_arrows.tsx` |
| Team menu | `components/sidebar/sidebar_header/sidebar_team_menu.tsx` |
| Message editing | `components/edit_post/edit_post.tsx` |
| Virtualized posts | `components/post_view/post_list_virtualized/` |

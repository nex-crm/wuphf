# Slack TUI Design Review

> Audit of all Slack-style TUI components against `docs/slack-ui-spec.md`.
> Reviewed 2026-03-17 by ink-architect.

---

## 1. Sidebar (sidebar.tsx, sidebar-types.ts)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| Workspace header at top | **PASS** | Bold white name + divider |
| Collapsible sections (▾/▸) | **PASS** | Toggle works, triangle correct |
| Channel # prefix | **PASS** | Gray `#` prefix |
| DM online/offline dots (●/○) | **PASS** | Green online, gray offline |
| Active item indicator (▎) | **FIXED** | Was `>`, changed to `▎` per spec §2.3 |
| Active item color = cyan bold | **PASS** | |
| Unread count badge (N) | **PASS** | Shows `(N)` for unread > 0 |
| Unread = bold name | **PASS** | `isBold = isActive \|\| isUnread` |
| Muted = gray dim | **FIXED** | Was treating muted same as normal; now gray |
| Private channel lock icon (🔒) | **MINOR GAP** | Not rendered — `visibility: "private"` exists in types but sidebar doesn't show lock. Low priority since no private channels yet. |
| "+ Add channel" footer | **PASS** | |
| Cursor navigation via arrow keys | **PASS** | Flat index with section offsets |
| Cursor upper-bound clamp | **FIXED** | Was unbounded upward; now clamped to `totalItems - 1` |

### Verdict: **PASS with 3 fixes applied**

---

## 2. Message List (messages.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| 5-minute grouping window | **PASS** | `GROUP_WINDOW_MS = 5 * 60 * 1000` |
| Same sender continuation (no avatar) | **PASS** | `ContinuationMessage` renders content only |
| Different sender = new group | **PASS** | |
| Date boundary = date separator | **PASS** | `getDateLabel()` with Today/Yesterday/full date |
| Date separator centered with lines | **PASS** | `DateSeparator` pads with `─` |
| Unread marker (red "New") | **PASS** | Red bold centered separator |
| System messages centered + italic | **PASS** | Gray dim italic with ✦ prefix |
| Avatar = [XX] initials | **PASS** | 2-letter initials in brackets |
| Agent color from palette | **PASS** | Uses `getAgentColor()` for sender color |
| Human sender = white name | **PASS** | |
| Timestamp = gray dim | **PASS** | |
| Thread indicator (↳ N replies) | **PASS** | Cyan thread indicator with last reply time |
| Edited indicator "(edited)" | **PASS** | Gray dim |
| Markdown rendering | **PASS** | Uses existing Markdown component |
| Reactions bar | **MINOR GAP** | Types exist (`ReactionData`) but no rendering. Reactions not yet in data flow. |

### Verdict: **PASS** — all core grouping and rendering correct

---

## 3. Thread Panel (thread-panel.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| Header: "Thread" + source channel | **PASS** | White bold "Thread" + cyan channel label |
| Close button (✕ Esc) | **PASS** | Right-aligned gray "✕ Esc" |
| Parent message shown first | **PASS** | `ThreadMessageItem` with full avatar |
| Reply count divider centered | **PASS** | `ReplyDivider` with centered "N replies" |
| Replies listed below divider | **PASS** | Mapped with grouping support |
| Thread compose at bottom | **PASS** | Reuses `ComposeArea` with `isThread=true` |
| "Also send to #channel" checkbox | **PASS** | ☑/☐ with green/gray color |
| Border = cyan when focused | **PASS** | `borderColor={focused ? "cyan" : "gray"}` |
| Escape closes thread | **PASS** | Wired through globalThis → app.tsx |
| Thread reply grouping | **MINOR GAP** | All replies have `isFirstInGroup: true` (hardcoded). Should apply 5-min grouping. Acceptable for v1. |

### Verdict: **PASS** — all structural elements correct

---

## 4. Compose Area (compose.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| Placeholder "Message #channel" for channels | **PASS** | |
| Placeholder "Message PersonName" for DMs | **PASS** | No # prefix |
| Placeholder "Reply in thread..." for threads | **PASS** | |
| Label text matches channel type | **FIXED** | Was always showing `Message #name` even for DMs; now conditional |
| Hint bar visible when focused | **PASS** | "@ mention · / command · Enter send" |
| Slash command autocomplete on `/` | **PASS** | `useSlashAutocomplete` with overlay |
| @mention autocomplete on `@` | **PASS** | `useMentionAutocomplete` with overlay |
| Tab cycles autocomplete | **PASS** | globalThis bridge with priority |
| Send button color (green when content) | **PASS** | "Enter=send" green/gray |
| Border = cyan when focused | **PASS** | |
| TextInput remount trick for clearing | **PASS** | `key={submitKey}` |

### Verdict: **PASS with 1 fix applied**

---

## 5. Quick Switcher (quick-switcher.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| Opens on Ctrl+K | **PASS** | Wired in app.tsx |
| Fuzzy substring match | **PASS** | `fuzzyMatch()` with case-insensitive substring |
| Token initials match ("fa" → "Founding Agent") | **PASS** | |
| Frecency-based sort | **PASS** | `score` field, sorted descending |
| Channel # prefix in results | **PASS** | |
| DM online/offline dots in results | **PASS** | |
| Unread badge in results | **PASS** | |
| Selected item highlight (cyan bold) | **PASS** | |
| Up/Down arrow navigation | **FIXED** | Was missing — added `__nexQuickSwitcherNav` globalThis bridge + app.tsx routing |
| Enter selects and closes | **PASS** | |
| Escape closes | **PASS** | Wired through app.tsx |
| "Switch to..." header | **PASS** | |
| Search input with 🔍 | **PASS** | |
| "No matches found" empty state | **PASS** | |
| Max 10 visible items | **PASS** | `maxVisible = 10` |

### Verdict: **PASS with 1 critical fix applied**

---

## 6. Channel Header (slack-channel-header.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| # prefix for channels | **PASS** | Gray # |
| Status dot for DMs | **PASS** | Green ●/gray ○ |
| Bold white channel name | **PASS** | |
| Action hints (Ctrl+K, Tab) | **PASS** | Right-aligned gray hints |
| Border bottom only | **PASS** | `borderBottom={true}` only |
| Topic text in center | **MINOR GAP** | Spec shows channel topic — not yet supported. No topic data in Channel type. |
| Member count badge | **MINOR GAP** | Not shown. Low priority for TUI v1. |

### Verdict: **PASS** — essential elements present

---

## 7. Layout (layout.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| 3-panel layout (sidebar + main + thread) | **PASS** | |
| Breakpoint < 60: main only | **PASS** | No sidebar |
| Breakpoint 60–79: narrow sidebar (20) + main | **PASS** | Thread replaces main |
| Breakpoint 80–119: sidebar (24) + main | **PASS** | Thread replaces main |
| Breakpoint ≥ 120: sidebar (28) + main + thread (45) | **PASS** | Full 3-panel |
| Thread replaces main when narrow | **PASS** | `threadReplacesMain` flag |
| Overlay renders on top | **PASS** | After flexRow in column layout |
| Sidebar fixed width, main grows | **PASS** | `flexShrink={0}` on sidebar |

### Verdict: **PASS** — responsive behavior correct at all breakpoints

---

## 8. Keyboard Navigation (app.tsx + slack-home.tsx)

### Spec Compliance
| Requirement | Status | Notes |
|---|---|---|
| Ctrl+K opens quick switcher | **PASS** | |
| Escape closes quick switcher | **PASS** | Priority over thread close |
| Escape closes thread panel | **PASS** | When focused on thread |
| Tab cycles focus sections | **PASS** | sidebar → messages → compose → (thread) |
| Shift+Tab reverse cycles | **PASS** | |
| Arrow up/down in sidebar | **PASS** | When sidebar focused |
| Enter selects in sidebar | **PASS** | |
| Space toggles section collapse | **PASS** | |
| Tab in autocomplete | **PASS** | Priority over focus cycling |
| Ctrl+C double-press exit | **PASS** | Existing behavior preserved |
| Quick switcher arrow nav | **FIXED** | Was missing; now routed via app.tsx |

### Verdict: **PASS with 1 fix applied**

---

## 9. Color Consistency

| Element | Expected | Actual | Status |
|---|---|---|---|
| Active item | cyan bold | cyan bold | **PASS** |
| Active border ▎ | cyan | cyan | **PASS** |
| Focused panel border | cyan | cyan | **PASS** |
| Unfocused panel border | gray | gray | **PASS** |
| Online dot | green ● | green ● | **PASS** |
| Offline dot | gray ○ | gray ○ | **PASS** |
| Unread badge | white | white (dimColor when inactive) | **PASS** |
| Channel name (normal) | white | white | **PASS** |
| Channel name (unread) | white bold | white bold | **PASS** |
| Section header | gray | gray | **PASS** |
| Timestamp | gray dim | gray dim | **PASS** |
| System message | gray dim italic | gray dim italic | **PASS** |
| Date separator | gray | gray | **PASS** |
| Unread marker | red bold | red bold | **PASS** |
| Thread indicator | cyan | cyan | **PASS** |
| Agent color palette | 6-color cycle | 6-color cycle | **PASS** |
| Send button active | green | green | **PASS** |
| Send button empty | gray | gray | **PASS** |

### Verdict: **PASS** — full color consistency

---

## 10. DM vs Channel Rendering

| Feature | Channel | DM | Status |
|---|---|---|---|
| Sidebar prefix | # | ●/○ | **PASS** |
| Header prefix | # | ●/○ | **PASS** |
| Compose label | "Message #name" | "Message name" | **FIXED** |
| Compose placeholder | "Message #name" | "Message PersonName" | **PASS** |
| Thread header | "#channel" | "PersonName" | **PASS** |
| Message routing | chatService.send() | chatService.send() + agentService.steer() | **PASS** |

### Verdict: **PASS with 1 fix applied**

---

## Summary of Fixes Applied

| # | Component | Issue | Severity | Fix |
|---|---|---|---|---|
| 1 | sidebar.tsx | Active indicator was `>` instead of `▎` | Medium | Changed to `▎` per spec §2.3 |
| 2 | sidebar.tsx | Muted items rendered same as normal | Low | Added gray color for muted items |
| 3 | compose.tsx | DM compose label showed `#` prefix | Medium | Conditional label based on channelType |
| 4 | slack-home.tsx | Sidebar cursor had no upper bound | Medium | Clamped to `totalItems - 1` |
| 5 | quick-switcher.tsx | No arrow key navigation in results | Critical | Added `__nexQuickSwitcherNav` bridge |
| 6 | app.tsx | Quick switcher had no up/down routing | Critical | Added arrow key routing when QS open |
| 7 | app.tsx | Status bar hint was stale ("Tab=channels") | Low | Updated to "Tab=focus  Ctrl+K=search" |
| 8 | sidebar.test.tsx | Tests expected old `>` indicator | N/A | Updated to match new `▎` |

## Known Minor Gaps (acceptable for v1)

1. **Private channel lock icon** — types support `visibility: "private"` but no 🔒 rendering
2. **Reactions bar** — types defined but no UI rendering
3. **Thread reply grouping** — all replies show as first-in-group (no 5-min window)
4. **Channel topic** — header doesn't show topic text
5. **Member count** — header doesn't show member count badge
6. **Typing indicators** — not implemented
7. **Message scroll/viewport** — no scroll position tracking or "jump to bottom"
8. **Empty channel state** — no special first-message display

## Test Results After Fixes

- **TypeScript**: Clean compile (`tsc --noEmit`)
- **Slack component tests** (node:test): **74/74 pass**
- **Vitest suite**: **20/20 pass** (58 empty stubs are pre-existing)

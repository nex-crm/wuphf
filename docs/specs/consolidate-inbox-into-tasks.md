# Consolidate the Inbox into the Task board

## Why

We have too many top-level surfaces. The Inbox is a separate nav destination
that only ever held two conceptual buckets:

1. **Items needing human attention** — decision-state tasks, blocking agent
   requests, pending wiki/code reviews.
2. **Items that are done and have an output to look at** — landed work.

Both already map onto the Task board's lifecycle lanes (`Needs human input`
and `Done`). Blocking requests already surface in chat (`InterviewBar`) and
reviews in the Wiki Reviews tab, so the Inbox was a pure aggregator.

## What changes

- **Tasks becomes the primary navigation item.** It moves to the front of the
  `Work` nav section and carries the attention badge + chime that used to live
  on the Inbox button (`inbox_attention` from `/office/stats` — the same
  broker-computed roll-up: requests + reviews + human-attention-state tasks).
- **Requests + reviews fold into the `Needs human input` lane** of the board.
  Each renders as a card next to the decision-state tasks already there.
  - Request card → opens the chat channel where its `InterviewBar` answers it.
  - Review card → opens the Wiki Reviews tab (`/reviews`).
- **The standalone Inbox surface is removed.** `/inbox` and the legacy
  `/apps/requests` redirect to `/tasks`. `DecisionInbox` (the routed surface)
  and its test are deleted. The `/inbox` route is kept as a redirect so old
  bookmarks resolve.

## What stays

- `inbox_attention` semantics (the badge) are unchanged — still a board-wide
  roll-up. The badge is a count of everything that needs the human across the
  board; the board organizes those items into their stage lanes (decision +
  requests + reviews in `Needs human input`, `review`/`changes_requested`
  tasks in `In progress`, `blocked_on_pr_merge` in `Blocked`).
- The Wiki Reviews tab and the chat `InterviewBar` remain the canonical
  answer surfaces; the board lane links to them rather than re-implementing.

## Files

- `web/src/components/sidebar/TasksNavButton.tsx` (was `InboxButton.tsx`)
- `web/src/components/sidebar/AppList.tsx`
- `web/src/components/lifecycle/TasksList.tsx`
- `web/src/routes/RootRoute.tsx`
- `web/src/components/sidebar/SidebarModules.stories.tsx`
- removed: `web/src/components/lifecycle/DecisionInbox.tsx` (+ test)

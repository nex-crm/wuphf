# 03b — Morgan's asset pipeline self-escalates (Scenario 3)

## Who and why

**Persona:** Morgan Lee, agency founder, six-person team. They have
been burned by tools that look great in the demo and fall apart on the
second day. The pattern Morgan is testing is "an agent escalates a
blocker without me being in the room". In Morgan's agency, this
question — the one that should take two minutes — sits in someone's
DMs for six hours every time.

**Outcome they came for:** ENG (or DSG, or any non-CEO agent) posts an
escalation in `#general` while Morgan is in a meeting, and CEO routes
it correctly without Morgan intervening.

## Steps

### 1. Drop a multi-step goal with an implicit dependency

In `#general`:

```
Ship a one-pager for our new client deck. DSG owns the layout, CMO
writes the copy, ENG hosts it at /client/<slug>.
```

#### Verify

- All three named agents pick up subtasks in the thread within 2 min.

### 2. Walk away for 30 minutes

Literally — open another window, attend a meeting, do anything else.

### 3. Come back and scan `#general`

#### Verify

- At least one of the agents posted an escalation or a clarifying
  message in `#general` (not just in a private agent-to-agent thread).
- The escalation names a specific blocker — for example "CMO copy
  references the old pricing table" or "DSG layout exports require a
  CSS grid polyfill on the production stack".
- CEO posted a routing reply that either resolved it or assigned a new
  owner.

### 4. Open the Inbox

Sidebar → **Inbox**.

#### Verify

- The Task row shows state `decision` (a decision packet awaits) OR
  `running` with a "Blocked on" entry naming the escalation.
- Open the detail pane: the inline section shows the human-readable
  blocker text from `#general`.

## What success looks like

The escalation Morgan reads in `#general` is the same blocker the Inbox
shows as a Decision Packet item — proving that channel chatter and the
operator's review queue are one surface, not two. Morgan should be able
to click one row in Inbox and have the entire conversation context
inline, without opening a separate "Reviews" or "Requests" app.

If they can do this without me telling them which app to open, the
unified inbox is shipping the way it was designed.

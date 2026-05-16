# 02a — Sam drops the onboarding goal (Scenario 2: drop a goal)

## Who and why

**Persona:** Sam Rivera, CTO at an eight-person startup. Already has
WUPHF running on a small office server. They are spending the morning
delegating one concrete deliverable — the new-user onboarding flow —
to the office and watching how the agents decompose it.

**Outcome they came for:** see ENG declare a real dependency
("Need copy from CMO first") rather than producing a confident chunk
of code that ignores the actual blocker.

## Steps

### 1. Open `#general`

In the browser, sidebar → `#general`.

#### Verify

- The channel is empty (or has the morning standup) — no leftover
  goal from yesterday.

### 2. Drop the goal

```
Ship the onboarding flow by Friday. ENG owns the implementation; DSG
exports the assets; CMO finalizes the in-product copy. CEO can
coordinate.
```

#### Verify

- Within ~60s, CEO replies in-thread (not as a top-level post) with a
  numbered decomposition.
- ENG replies to CEO's thread within ~90s with a scoped plan that
  includes the literal phrase "depends on" or "blocked on" referencing
  CMO's copy.

### 3. Open the Inbox

Sidebar → **Inbox**.

#### Verify

- A "Task" row exists for the onboarding flow.
- The row's state pill reads `running` (not `decision` yet).
- The detail pane on the right shows the spec section with the
  acceptance criteria CEO posted.

### 4. Open the Task detail and confirm the dependency

#### Verify

- The detail pane shows a "Blocked on" or "Depends on" section with
  CMO listed.
- The lifecycle state pill matches the row pill (`running`).

## What success looks like

Sam sees ENG self-declare the CMO dependency *in writing* in `#general`
within the first two minutes, AND the same dependency shows up in the
Inbox detail pane. The "agents coordinate, they do not invent code in
isolation" claim is verified by both the channel transcript and the
structured packet view.

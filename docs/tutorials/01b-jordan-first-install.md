# 01b — Jordan's first install with a custom pack (Scenario 1)

## Who and why

**Persona:** Jordan Park, indie hacker, three live products. They have
been the manual "traffic cop" between ChatGPT tabs for months. They are
willing to give WUPHF ten minutes and they want to start with a pack
that already matches "indie maker with a launch tomorrow".

**Outcome they came for:** install in one command, pick an opinionated
pack, see an office that mirrors their actual workflow.

## Steps

### 1. Install with the founding-team pack

```bash
npx wuphf --pack founding-team
```

#### Verify

- The CLI prints the pack name plus the URL.
- A browser opens at `http://localhost:7891`.
- The office name reads "founding-team" (not "default").

### 2. Confirm the pack agents

In the browser:

#### Verify

- Agent participants list includes the founding-team roster (CEO, ENG,
  DSG, CMO — plus any pack-specific extras).
- `#general`, `#dev`, `#marketing` channels exist out of the box.
- Each agent has a non-empty bio when clicked.

### 3. Hover the version chip in the bottom-left sidebar

#### Verify

- The chip shows a semantic version (e.g. `v0.194.x`).
- Hover reveals the build date and the pack name.

### 4. Drop the goal into `#general`

```
Launch the v2 site by end of week. I need ENG to scope, DSG to do hero
visuals, CMO to draft the launch tweet.
```

#### Verify

- The message lands in `#general`.
- Within ~60s, CEO acknowledges and dispatches three subgoals — one per
  named agent.
- The Inbox sidebar entry shows a non-zero badge once CEO has dispatched.

## What success looks like

Jordan's office matches the pack name. Channels and agents arrive
pre-configured for the maker workflow rather than as generic templates
they have to rename. CEO's first reply names ENG, DSG, and CMO by the
verbs Jordan used ("scope", "hero visuals", "launch tweet").

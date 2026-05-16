# 02b — Riley scopes a build flag (Scenario 2: drop a goal)

## Who and why

**Persona:** Riley Walsh, product engineer at a 30-person startup.
Cabinet ICP. Skeptical of agent tools after a bad year of "confident
nonsense". They are willing to try one mundane, specific goal and see
whether the agent surfaces a real engineering question rather than
producing a wall of pseudo-code.

**Outcome they came for:** ENG asks a clarifying question (or surfaces
a real constraint) instead of just writing imaginary code.

## Steps

### 1. Drop the goal in `#dev`

In the browser, sidebar → `#dev`.

```
Add a kill switch for the new pricing experiment. Should default to
"off" in production but be flippable per environment without a deploy.
```

#### Verify

- The message appears in `#dev`.

### 2. Wait ~90s

#### Verify

- ENG responds in the thread (not as a top-level post).
- ENG's reply contains at least one of:
  - A clarifying question ("Which environments are in scope?",
    "Are you on LaunchDarkly or homemade?")
  - A scoped plan that references a specific implementation detail
    (env var name, config table, feature flag service).

### 3. Reply with one piece of clarifying info

In the same thread:

```
Just env vars for now, no third-party service. Three environments:
dev/staging/prod.
```

#### Verify

- ENG follow-up within ~60s, with a concrete acceptance-criteria list.
- The follow-up references the three environments by name.

### 4. Open the Inbox

Sidebar → **Inbox**.

#### Verify

- A Task row exists for the kill switch.
- Detail pane shows AC items that mention env vars and the three
  environment names.

## What success looks like

ENG's first reply demonstrates "uncertain specificity" — a real
clarifying question or a concrete technical detail — rather than a
confident wall of code that ignores the missing requirements. Riley
should be able to point to the exact line in `#dev` that earned the
benefit of the doubt.

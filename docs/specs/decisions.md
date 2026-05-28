# Decisions — Sub-Spec (Slice 5)

**Status:** Sub-spec, implementation deferred to post-MVP follow-up.
**Parent spec:** [`docs/specs/issue-execution-loop.md`](./issue-execution-loop.md) (Slice 5).
**Owner:** Najmuzzaman
**Branch:** `mvp/v3-architecture-shape`

## What this fixes

Before Slice 1, clicking "Approve & Start" on a Drafting Issue called
`writeWikiPromotionLocked` which produced a wiki article titled "Decision"
containing the issue spec verbatim. There was no actual decision being
made — the human was approving the *start of work*, not the *outcome*.
Slice 1 stripped that write. This sub-spec describes the first-class
Decision concept that fills the gap.

## Why Decisions are not the same as Issues

| | Issue | Decision |
|---|---|---|
| Purpose | Scoped unit of work | Captured choice the team made |
| Author | Whoever scoped the work | Whoever made the call |
| Lifecycle | Backlog → InProgress → Done | One-shot record |
| Wiki write | No (unless approval is for a Decision) | Yes, always |
| Linked to | Sub-tasks, comments, owner | Issue(s) that surfaced the question, prior Decisions it supersedes |

A single Issue can produce zero, one, or many Decisions. A Decision can
span multiple Issues. The Issues board renders work; the Decisions log
renders judgment.

## Decision shape

```go
type Decision struct {
    ID          string    // dec-{n}
    Title       string    // "Switch from Postgres to SQLite for Cellar"
    Problem     string    // What was being decided + why now
    Options     []DecisionOption
    Choice      string    // OptionID of the chosen path
    Rationale   string    // Why this option won
    DecidedBy   string    // human slug (the human approves Decisions)
    DecidedAt   time.Time
    Impact      string    // What changes immediately + downstream
    IssueIDs    []string  // Issues that surfaced this Decision
    Supersedes  []string  // Decision IDs this Decision overrides
    SupersededBy string   // Decision ID that later overrode this one (mutable)
    WikiPath    string    // team/decisions/dec-{n}.md (written on record)
}

type DecisionOption struct {
    ID          string
    Label       string
    Description string
    Tradeoffs   string
}
```

## Surfaces

### MCP tool: `team_decision`

```ts
team_decision({
  action: "propose" | "record" | "supersede",
  title: string,
  problem: string,
  options?: DecisionOption[],         // for action=propose
  choice?: string,                    // OptionID, for action=record
  rationale?: string,                 // for action=record
  impact?: string,                    // for action=record
  issue_id?: string,                  // the Issue this came from
  supersedes?: string,                // Decision ID, for action=supersede
})
```

- `action=propose` opens a `human_interview`-style request with the
  options so the human picks. The agent SHOULD NOT call `action=record`
  directly when the call is human-facing — let the human pick the
  option to make it explicit.
- `action=record` is the agent declaring a decision (for engineering
  picks the agent owns, e.g. "I chose Postgres over MySQL because
  pgvector").
- `action=supersede` records a new Decision that overrides a previous
  one. The old Decision is preserved (immutable history) with
  `SupersededBy` populated.

### Broker endpoints

- `POST /decisions` — record a new Decision. Writes the wiki article in
  one atomic operation; rejects with 409 if a Decision with the same
  title already exists.
- `GET /decisions?issue_id=...` — list Decisions linked to an Issue
  (for the Issue detail surface).
- `GET /decisions/{id}` — fetch a Decision packet.

### FE surfaces

1. **Issue detail** — "Decisions made under this Issue" section showing
   links to recorded Decisions.
2. **Inbox card** for `action=propose` — human picks an option.
3. **Channel chat card** when a Decision is recorded — `Kind="decision_recorded"`
   with payload {id, title, choice, rationale_excerpt, wiki_path}.
4. **Decisions log** route (`/decisions`) — chronological view, filter
   by Issue or by status (current / superseded).

### Wiki article format

`team/decisions/dec-{id}.md`:

```markdown
# {Title}

- Decision ID: `dec-{id}`
- Decided by: @{slug}
- Decided at: {RFC3339}
- Status: current | superseded by dec-{n}
- Linked Issues: {id}, {id}

## Problem

{problem}

## Options considered

- **{label-1}** — {description}. Tradeoff: {tradeoffs}
- **{label-2}** — {description}. Tradeoff: {tradeoffs}
- ...

## Choice

**{chosen option label}**

## Rationale

{rationale}

## Impact

{impact}

## Supersedes

- dec-{n}: {title} ({why this overrides it})
```

## Implementation slices

1. **Type + broker storage** (~2 hr). Decision type, storage on disk
   under `~/.wuphf/decisions/dec-{n}.json`, broker handlers.
2. **MCP `team_decision` tool** (~1 hr). All three actions; wires
   into the existing `team_request` flow for `action=propose`.
3. **Wiki write** (~1 hr). Atomic write of the markdown article using
   the existing wiki worker. Reuse `wikiPromotionPath` shape.
4. **FE: chat card + Issue detail section** (~2 hr). Reuse the
   `IssueLifecycleCard` pattern for the `decision_recorded` system
   message; add the "Decisions" section in `IssueDocument.tsx`.
5. **FE: Decisions log route** (~2 hr). New top-level route, simple
   list view. Filter by Issue, status, decider.
6. **Prompt block** (~30 min). New `decisionsBlock()` instructing CEO
   when to use `team_decision` (propose vs record).

Total: ~8.5 hr, one focused day.

## Open questions

- Should Decisions auto-expire (e.g. "stale after 90 days, needs
  re-affirmation")? Likely no — Decisions are immutable; superseding
  is the way to update.
- Per-team scoping? Today: single office. Multi-team is post-MVP and
  Decisions inherit whatever scoping Issues end up with.
- LLM-assisted Decision drafting (CEO drafts the problem + options
  block from chat context)? Plausible follow-on after the manual
  flow is proven.

## Why deferred from the current bundle

The triggering pain (bogus wiki "decision" on Approve) is already
gone — Slice 1 stripped the write. The full Decision concept is a
half-day+ effort that introduces new wire shapes, a new MCP tool,
new broker endpoints, a new FE route, and a new prompt rule.
Bundling it with Slices 1–4 + 6 would have made the change-set too
large to live-test in one session. Spec written here so the work
queues cleanly for the next focused session.

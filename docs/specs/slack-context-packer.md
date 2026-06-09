# Inbound Context Packer

> Status: **DRAFT / design v2** (2026-06-09). Not yet implemented. Revised after a
> `/codex` consult review — see "Review dispositions" at the end.
> Owner: founder + harness team.
> Scope: open-source self-hosted WUPHF. Designed as a **shared kernel** the Nex
> cloud (hosted, multi-tenant) version inherits without forking.

## One line

The component that lets an agent from **anywhere** — a custom Slack bot, a dev's
Cursor/Claude session, a vendor bot — participate fully in a WUPHF office with
**zero integration on its side**, by having our CEO agent retrieve the right
slice of the team brain, **classify and redact it for export**, and deliver it
inside the message the bot already reads.

## Why this exists

WUPHF's compounding loop works because, when a WUPHF-hosted agent spawns,
`promptBuilder.Build(slug)` (`internal/team/prompt_builder.go:53`) injects the
roster, the `== AVAILABLE SKILLS ==` catalog (`renderSkillsCatalogBlock`, `:483`),
and the `== PRIOR TEAM LEARNINGS ==` block (`learningSnapshot` →
`renderPriorLearningsBlock`, `:403`/`:410`) into its system prompt. That is
**push-side** injection.

A foreign bot in Slack sets its own system prompt and never receives our
injection, so by default it is a dumb bot in a channel. The CEO membrane fixes
this from our side only:

- **Inbound (this doc): retrieve, classify, inject.** Before the CEO delegates a
  step to a foreign bot, the packer gathers candidate brain context, runs it
  through an **export-classification and redaction stage**, fits the survivors to
  a budget, and writes them into the `@`-mention the bot already reads.
- **Outbound (shipped by #1033): observe-and-curate.** The Librarian (Pam)
  observes the thread and is the sole writer to the wiki; foreign output is
  promoted only through the existing human gate.

The packer is the inbound half and the **only large net-new component** of the
feature. It is also a **data-egress boundary** — see the egress section, which is
the highest-risk part of this design and the reason the v1 "task-attached is safe"
model was rejected.

## Where it sits

```
Slack workspace channel  (just a workspace; carries no structure)
        │  human posts a goal / bot posts a reply
        ▼
  Slack bridge  ──────────────►  Broker  (holds the broker token; one trust domain)
   (transport adapter)            │
        ▲                         ├─ Task lifecycle (#1033): plan → approve → run → review
        │ posts packed delegation │
        │                         ├─ CEO triage: assign present agents, restate intent
        │                         │        │ StepIntent (+taint) + target bot
        │                         │        ▼
        │                         │   ┌──────────────────────────────────┐
        │                         │   │  INBOUND CONTEXT PACKER           │ ◄── this doc
        │                         │   │  Gather → CLASSIFY/REDACT →       │
        │                         │   │  Budget → Render → Deliver        │
        │                         │   └──────────┬───────────────────────┘
        │                         │              │ reads via BrainHandle
        │                         │   ┌──────────▼───────────┐
        │                         │   │ shared retrieval      │ primitives + ranking
        │                         │   │ (Learnings·Wiki·Skills·Roster·Task)
        └─────────────────────────┘   └──────────────────────┘
```

The Slack channel has no model of tasks. The Task lives in the broker. The packer
turns "CEO wants bot B to do step S of task T" into a Slack message B can act on
— minus anything the egress policy forbids B from seeing.

## Data model

All structs immutable once constructed (return new values, never mutate). No
`any`. Validate at the `Gather` boundary.

```go
// BrainHandle is the office/tenant brain the packer reads from. OSS self-hosted:
// a singleton. Nex cloud: one per tenant. The packer NEVER touches global state
// directly. This is the single seam that keeps the component cloud-portable
// without a fork.
type BrainHandle interface {
    OfficeID() string
    Task(id string) (TaskView, error)        // teamTask + IssueDraftSpec + approved plan + UpdatedAt
    Roster(taskID string) ([]RosterLine, error)
    Learnings() LearningSearcher              // wraps LearningLog.Search (learnings.go:247) — task-scoped filters
    Wiki() WikiSearcher                       // wraps WikiIndex.Search (wiki_index.go:418) + retrieveWithClass
    Skills() SkillCatalog                     // wraps ListActiveSkillSummaries
    Egress() EgressPolicy                     // versioned; see egress section
    Secrets() SecretScanner                   // redaction pass over every export candidate
}

// BotIdentity is the full provenance anchor. A bare Slack user id is NOT enough
// across workspaces / enterprise grids, and display names are spoofable. Trust
// binds to this whole tuple, never to DisplayName.
type BotIdentity struct {
    SlackTeamID     string // workspace id
    SlackEnterprise string // enterprise-grid id, if any
    AppUserID       string // the bot's app/bot user id
    DisplayName     string // advisory only — never a trust input
    VerifiedVia     string // how identity was confirmed (install OAuth, admin add)
}

// BotDataHandling describes where this bot's prompts actually go. Trust is about
// data handling, not just "we built it" — a first-party bot can still forward
// prompts to a third-party LLM, retain logs, or run in a personal workspace.
type BotDataHandling struct {
    ModelProvider      string // "anthropic" | "openai" | "self-hosted" | "unknown"
    RetainsLogs        bool
    WorkspaceOwned     bool   // company-owned workspace vs a personal one
    NetworkEgress      string // "none" | "vendor-llm" | "open" | "unknown"
    ReadsThreadHistory bool
}

type BotTrust int // DERIVED partly from DataHandling, not just origin
const (
    BotUntrusted BotTrust = iota // DEFAULT for anything externally originated
    BotFirstParty                // in-house, company-owned-workspace, known data handling
    BotHosted                    // WUPHF-hosted agent (also gets push-side injection)
)

type ReadScope int
const ( ReadMentionOnly ReadScope = iota; ReadThread )

type Invocation int
const ( InvokeMention Invocation = iota; InvokeSlash; InvokeKeyword )

type BotProfile struct {
    Version      int          // bumped on every change; referenced by every InjectionRecord
    Slug         string
    Identity     BotIdentity
    Trust        BotTrust
    DataHandling BotDataHandling
    ReadScope    ReadScope    // upgraded to ReadThread ONLY via a nonce probe (below)
    Invoke       Invocation
    Trigger      string       // slash command or keyword when Invoke != InvokeMention
    Specialties  []string     // from OBSERVED task history + human confirmation — NOT display name
    Notes        string
}

// StepIntent is tainted if it was influenced by foreign-bot output. Tainted
// intent must NOT drive free retrieval (it would be a retrieval prompt-injection
// path into WikiIndex.Search). Taint is DERIVED, not caller-declared: if any
// SourceRef is a foreign-bot message, the intent stays TaintForeign unless it is
// rebuilt from approved task metadata / human-authored text with NO spans copied
// from the foreign source. The CEO cannot launder foreign text into a "clean"
// restatement. Only genuinely clean terms drive retrieval.
type Taint int
const ( TaintClean Taint = iota; TaintForeign )

type StepIntent struct {
    Text       string   // the precise ask, CEO-restated
    Taint      Taint
    SourceRefs []string // message ids the intent derives from, for audit
}

// ContextRequest — the packer's input for one delegation. Carries snapshot +
// version guards so Gather/Classify/Deliver cannot race a task edit or a trust
// downgrade.
type ContextRequest struct {
    Brain           BrainHandle
    TaskID          string
    TaskUpdatedAt   string      // re-checked before Deliver; stale → abort
    PlanID          string
    PlanVersion     int
    Target          BotProfile  // carries Target.Version
    Intent          StepIntent
    Thread          ThreadRef   // workspace + channel + thread ts
    EgressPolicyVer int
    Approver        string      // who approved the plan / this egress, when a gate applies
    IdempotencyKey  string      // dedupes both delivery and the InjectionRecord
}

// ExportClass is decided per item by the egress policy + secret scan.
type ExportClass int
const (
    ExportDenied   ExportClass = iota // never leaves the brain
    ExportRedacted                    // leaves only after redaction
    ExportAllowed                     // leaves as-is
)

type ContextItem struct {
    Ref        string      // id/path of the source brain item
    Kind       string      // "plan" | "learning" | "wiki" | "roster" | "skill" | "task"
    Body       string      // POST-redaction text
    Class      ExportClass
    Redactions int         // count of secrets/PII removed
}

// ContextBundle — classified + redacted, pre-budget. Only ExportAllowed and
// ExportRedacted items survive Classify into here.
type ContextBundle struct {
    Ask        string        // Intent.Text, length-capped + taint-checked; NOT a retrieval input
    ReturnPact string        // what to return, where, who to tag
    Guards     []string      // ADVISORY to the bot — explicitly NOT a security control
    Items      []ContextItem
}

// PackedDelegation — the output the bridge posts to Slack.
type PackedDelegation struct {
    MentionText   string          // ALWAYS carries the essentials
    ThreadContext string          // CHANNEL-VISIBLE: classified against the LEAST-trusted reader
                                  // in the channel, not just Target (Slack threads are not ACL)
    Injection     InjectionRecord
}

type DeliveryStatus int
const ( DeliveryPending DeliveryStatus = iota; DeliverySent; DeliveryFailed )

type ItemAudit struct {
    Ref        string
    Kind       string
    Class      ExportClass
    Redactions int
}

// InjectionRecord — append-only egress audit. Strong enough for incident
// response: proves exactly what was sent, where, under which policy, and whether
// it landed.
type InjectionRecord struct {
    IdempotencyKey string
    TaskID         string
    PlanID         string
    PlanVersion    int
    Identity       BotIdentity   // full tuple, not a bare id
    BotTrust       BotTrust
    ProfileVersion int
    PolicyVersion  int
    WorkspaceID    string
    ChannelID      string
    ThreadTS       string
    MessageTS      string        // filled on DeliverySent
    Items          []ItemAudit
    RenderedHash   string        // hash of exactly what was sent
    TokenCount     int
    Status         DeliveryStatus
    FailureReason  string
    Timestamp      string
}
```

## The pipeline

Security-first ordering: **classify and redact before you rank**, so containment
is never traded away for relevance.

```go
func Gather(ctx context.Context, req ContextRequest) (RawBundle, error)        // task-scoped retrieval
func Classify(ctx context.Context, raw RawBundle, req ContextRequest) (ContextBundle, error) // export-class + redact; drop ExportDenied
func Budget(b ContextBundle, t BotProfile) ContextBundle                       // rank + trim the survivors to fit
func Render(b ContextBundle, t BotProfile) PackedDelegation                    // shape to profile; ThreadContext classified vs channel
func Deliver(ctx context.Context, bridge SlackBridge, d PackedDelegation, key string) error // idempotent
```

1. **Gather** — retrieve candidates, **task-scoped**. Always sets `Ask`,
   `ReturnPact`, `Guards`. Pulls the approved plan step (`teamTask.IssueDraftSpec`
   `broker_types.go:269` + the owner-notebook plan from `LifecycleStatePlanning`).
   Retrieval uses `Learnings().Search` scoped by the task's tags/files (the
   current push-side `learningSnapshot` is broad — `Limit: 8`, no task scope
   `prompts.go:61` — so the shared primitive must add scoping, not be extracted
   as-is) and `Wiki().Search` driven **only by clean intent terms** (tainted
   intent cannot reach retrieval).
2. **Classify** — the egress boundary. The classified unit is the **whole
   delegation envelope**, not just `Items`: `Ask`, `ReturnPact`, and `Guards` are
   export data too — a tainted task can plant a secret or customer datum in the
   ask, and those fields are "never dropped" by Budget, so an unclassified
   envelope is a direct exfiltration channel. For every candidate item **and each
   envelope field**: assign `ExportClass` from `Egress().Allow(item, audience)`
   and run the **egress redaction contract** (see Egress boundary) to redact
   secrets/PII. Drop `ExportDenied`; a field that cannot be sanitized **fails
   closed** — the whole delegation is held, never sent partial. `Redactions` is
   recorded per item. Runs **before** budgeting so denied content is gone before
   ranking.
3. **Budget** — rank the survivors and trim to the profile's token tier. `Ask`,
   `ReturnPact`, `Guards` are never dropped, but `Ask` is length-capped and
   taint-checked (an oversized or tainted ask is truncated/flagged, not trusted).
4. **Render** — shape to the profile. `ThreadContext` is classified against the
   **least-trusted reader in the channel**, because a thread post is visible to
   every human and bot in that channel, not just the target.
5. **Deliver** — idempotent on `IdempotencyKey`. First **re-validate the
   snapshot**: re-check `TaskUpdatedAt`, `Target.Version`, `EgressPolicyVer`, and
   the *live* trust tier — not just `TaskUpdatedAt`. A trust downgrade or policy
   bump between Classify and Deliver must abort the send; stale-authorized context
   must never ship. Then run a **final redaction scan over the exact rendered
   bytes** (`MentionText` + `ThreadContext`): Render can reintroduce raw refs,
   titles, plan text, or failure reasons that a pre-render item scan never saw, so
   the post-render sealed text is what gets hashed into `RenderedHash`. Write
   `InjectionRecord` as `DeliveryPending` first, post, then update to
   `DeliverySent` (with the Slack `ts`) or `DeliveryFailed` (with reason). **Do
   not rely on the generic outbound dispatcher** (`broker_outbound_dispatch.go`),
   which marks delivered on dequeue and drops send failures — for an egress
   boundary, dropped or duplicated context is security-relevant, so the packer
   tracks its own delivery state.

### Shared retrieval, not a shared bundle

`promptBuilder` (push) and the packer (pull) **share the retrieval primitives and
ranking** — `LearningLog.Search`, `WikiIndex.Search`, the skill match, the roster
read — so the two cannot drift on what the brain *contains*. They do **not** share
a bundle contract. Hosted agents get durable system instructions, policies, the
full skills catalog, and broad learnings through trusted tools; foreign bots get
an **egress-filtered, task-scoped, redacted subset**. The packer's `Classify`
stage is the difference, and it has no push-side equivalent. Extract the query +
rank helpers into one place; keep the bundle shaping separate.

## Egress boundary (the hard part — read this twice)

The packer ships internal brain content into a foreign LLM we do not control,
which may log the prompt or forward it to a third-party model. **It is not safe by
default and the v1 "task-attached content is safe" rule was wrong.** "Attached to
the task" proves *relevance*, not *exportability*: `teamTask.Details` and
`IssueDraftSpec` are free-form and routinely contain pasted logs, customer data,
credentials, or injection text.

Required controls, all of them, before any item is injected:

1. **Per-item export classification.** Every candidate runs through
   `EgressPolicy.Allow(item, target)` → `ExportAllowed | ExportRedacted |
   ExportDenied`. No item reaches Slack without a class.
2. **Secret/PII redaction on every export candidate**, including task-attached
   content and the envelope fields. Redaction count is audited. **The existing
   display scanner is not a deny gate.** `scanner.RedactSecretsForDisplay`
   (`internal/scanner/scanner_detector.go:130`) returns a `RedactionResult` with
   **no error**; its entropy pass *stops* after `maxEntropyHitsPerFile` (`:191`),
   leaving over-cap secrets in `.Content`; and the current caller
   `redactSecretsInText` (`internal/team/message_redaction.go:9`) returns
   `.Content` directly. The egress path needs a **fail-closed wrapper** on top of
   it: deny the item when `RedactionResult.Poisoned` is true, when the entropy-hit
   cap was reached (the remainder cannot be proven clean), or on any scanner
   error. Never emit `.Content` from a result that hit a limit.
3. **First-egress human gate for untrusted bots.** The first time the office
   sends *any* brain content to a given bot, raise the approval card (reuse the
   deterministic-integrations `ExternalActionApprovalCard`) so a human signs off
   on "this external bot may now receive task context." The approval is keyed to
   the **verified install** — Slack app/install id + `SlackTeamID` +
   `SlackEnterprise` + `AppUserID` + a `BotDataHandling` fingerprint + trust tier +
   profile version — **never a bare user id or display name**. Any reinstall,
   enterprise-grid move, data-handling change, trust downgrade, or revocation
   **re-gates**. Subsequent sends to the same verified install are gated only by
   classification.
4. **Channel-visibility classification.** `ThreadContext` is visible to everyone
   in the channel — **and to anyone who joins later and reads history.** Classify
   it against the **least-trusted reader present**; and because membership at
   classify time cannot prove a future untrusted joiner won't read the thread,
   treat a non-DM thread post as visible to a future unknown reader unless history
   visibility is provably closed. Deliver sensitive, target-only context via
   ephemeral/DM rather than a thread post. `Egress().Allow` therefore takes the
   **audience**, not a single target.
5. **Taint-aware intent.** `StepIntent.Taint == TaintForeign` cannot drive
   retrieval. The CEO must restate intent in clean terms; the packer enforces this
   structurally rather than trusting the CEO to have done it.
6. **Trust reflects data handling, not origin.** A bot is `BotFirstParty` only if
   `DataHandling` is known and acceptable (company-owned workspace, known model
   provider). Origin alone never grants a tier.

Policy by tier (still conservative; labels are a later refinement that only
*widens* first-party reach):

| Bot trust | What survives Classify |
|---|---|
| `BotUntrusted` (default) | The CEO-authored `Ask` + `ReturnPact` + the approved plan step, all redacted. **Raw `Details`/`IssueDraftSpec` prose is NOT exported wholesale** — redaction catches credential *tokens*, not confidential narrative, source, or customer data — so untrusted bots get only human-approved excerpts until per-item sensitivity labels exist. No free wiki/learning retrieval. First egress human-gated. |
| `BotFirstParty` | The above + task-scoped learnings + **explicitly task-linked** wiki refs (not free wiki retrieval until per-item sensitivity labels exist) |
| `BotHosted` | Everything (it already gets the full push-side injection) |

**`Guards` are advisory, not a control.** A foreign bot may ignore "no external
action without approval." The real boundary is withholding the data and never
embedding credentials or high-sensitivity detail. The action gate
(`ExternalActionApprovalCard`) is the enforced control; `Guards` text is a hint.

## Budget heuristic

Run the security pass first, then rank what remains.

| ReadScope | Mention budget | Thread block budget |
|---|---|---|
| `ReadMentionOnly` (default) | ~600 tok hard cap | — |
| `ReadThread` (nonce-verified) | ~400 tok | up to ~2000 tok |

- **Never dropped:** `Ask`, `ReturnPact`, `Guards`. `Ask` is length-capped and
  taint-checked; if it overflows, truncate the `Ask`, never the `Guards`.
- **Rank order for `Items`** (trim from the bottom): approved plan step → task-
  scoped high-confidence learnings → task-linked wiki refs → roster lines (cap ~3)
  → matching skill hints. Reuse `WikiIndex.Search` (BM25), `retrieveWithClass`
  (typed/relationship), `LearningLog.Search` (filtered). Do not reinvent retrieval.

## Per-bot delivery profile

- **On first sight:** `Trust=BotUntrusted`, `ReadScope=ReadMentionOnly`,
  `Invoke=InvokeMention`, empty `Specialties`. Safe defaults that work for any bot.
- **ReadScope upgrade requires an explicit nonce probe.** Send a unique token in
  thread-only context and confirm the bot echoes/acts on it before setting
  `ReadThread`; store the evidence. Never infer thread-reading from ordinary
  replies (humans paste, LLMs quote — both produce false positives).
- **Specialties** come from observed successful task history plus human
  confirmation for routing-sensitive changes, never from the display name.
- **Trust** is set by a human, informed by `DataHandling`. Default stays
  `BotUntrusted`. Hosted agents are `BotHosted` automatically.

## Plan-mode interaction (#1033)

Plan mode stays on. For foreign-bot work the CEO owns planning, a human approves
the plan, then the packer delivers only the approved step(s). The bot never plans
into a notebook; it receives a scoped, approved `Ask`. Combined with the
first-egress gate, a human has signed off on both *what* the bot does and *that it
may receive context* before any brain content leaves.

## Reuse map (verified surfaces)

| Need | Existing surface |
|---|---|
| Push-side reference behavior | `promptBuilder.Build` `internal/team/prompt_builder.go:53` |
| Prior-learnings gather/render | `learningSnapshot` `:403`, `renderPriorLearningsBlock` `:410`; broad call site `prompts.go:61` |
| Skills catalog | `renderSkillsCatalogBlock` `:483`, `ListActiveSkillSummaries` |
| Learning search (typed filters) | `LearningLog.Search` `learnings.go:247`; HTTP `handleLearningSearch` `broker_learning.go:90` |
| Wiki retrieval | `WikiIndex.Search` `wiki_index.go:418`; `retrieveWithClass` `wiki_query_retrieve.go:44`; `searchArticles` `wiki_git.go:926` |
| Task spec / plan | `teamTask` + `IssueDraftSpec` `broker_types.go:261/269`; `LifecycleStatePlanning` |
| Roster + watching | `officeMember` `broker_types.go:465`, `.Watching` `:483` |
| Slack delivery | `transport.Host` `transport/transport.go:156`; `PostInboundSurfaceMessage`/`ReceiveMessage`/`UpsertParticipant` `broker_transport.go`; model the bridge on `telegram.go` |
| Delivery state caution | generic dispatch marks delivered on dequeue, drops failures: `broker_outbound_dispatch.go` — do not reuse for egress |
| Action gate + first-egress gate | `ExternalActionApprovalCard` from the deterministic-integrations lane |
| Egress redaction (fail-closed wrapper) | `scanner.RedactSecretsForDisplay` `scanner_detector.go:130` + `RedactionResult.Poisoned`/entropy cap `:191` — **wrap, do not call raw** like `message_redaction.go:9` |
| Channel membership (least-trusted reader) | `teamChannel` member lists `broker_types.go:452` — a stored list; cannot prove thread history is closed |

**Two primitives must be extended before this is implementable as specified:**

- `LearningSearchFilters` (`learnings.go:107`) has **no `TaskID` field** — only
  scope/playbook/file/query, and `LearningRecord` carries `TaskID` (`:99`) that
  is never filterable. Task-scoped learning egress requires adding an exact
  `TaskID` (and entity/tag) filter and **AND-scoping**; deny export if exact
  scope cannot be expressed. Fuzzy scope matching is not acceptable for egress.
- "Explicitly task-linked wiki refs" (`BotFirstParty` policy) has **no backing
  link** — a task carries `Details`/`Tags` but no wiki-ref field, and the only
  wiki primitive is free `WikiIndex.Search` (`wiki_index.go:418`). Either add a
  real task→article link, or drop wiki from the FirstParty tier until per-item
  sensitivity labels exist. Do **not** fall back to free search — that is exactly
  the path the policy forbids.

## Acceptance scenarios (ICP-first — test against all three)

1. **Untrusted vendor bot + redaction + first-egress gate.** A vendor
   `@notetaker` (`BotUntrusted`, `ReadMentionOnly`) is assigned a step where the
   task `Details` contain a pasted API key. Assert: (a) the first send to this
   identity raised the human egress gate; (b) the key is redacted (`Redactions > 0`)
   and never appears in `RenderedHash`'s source; (c) the mention carries
   `Ask`/`ReturnPact`/`Guards`/plan step and **no** wiki; (d) `InjectionRecord`
   lists only task items, each `ExportAllowed`/`ExportRedacted`, with full identity
   tuple and `DeliverySent` + a Slack `ts`.
2. **First-party warehouse bot, thread-verified.** `@warehouse-bot`
   (`BotFirstParty`, `ReadThread` set only after a passed nonce probe) gets a lean
   mention plus a thread block carrying a task-scoped learning and a
   **task-linked** wiki ref (not free wiki retrieval). Assert the thread block was
   classified against the least-trusted reader in the channel.
3. **Hosted parity = same retrieval, filtered bundle.** A `BotHosted` agent on the
   same task gets context via the push path; the packer does not double-inject.
   Assert the **shared retrieval primitive** returns the same candidate set for
   both paths, and the packer's `Classify` yields a subset for any non-hosted bot.
   Not byte-identical bundles.

## Out of scope

- Multi-tenant auth, network listener, credential issuance — that is **Nex cloud**.
  Here the bridge holds the broker token inside one trust domain.
- Per-item wiki sensitivity labels (later refinement that widens first-party wiki
  reach; the default policy ships without them).
- The Slack bridge adapter and the Block Kit gate cards (separate specs).

## Before implementation

The egress boundary is the highest-risk surface. Triangulation
(`scripts/dispatch-triangulation.sh`, security/API/architecture lenses) **plus** a
verification agent on `Classify` + `EgressPolicy` + redaction were run on
2026-06-09 — dispositions below. New wire shapes still get a cross-language sanity
check if any cross a process boundary to the bridge.

**Implementation is gated on three code prerequisites surfaced by the review:**
(1) a fail-closed egress redaction wrapper over `internal/scanner` (deny on
`Poisoned`/entropy-cap/error); (2) a `TaskID` filter on `LearningSearchFilters`;
(3) a task→wiki-article link field (or wiki dropped from the FirstParty tier).
None exist today.

## Review dispositions (codex consult, 2026-06-09)

| # | Finding | Sev | Disposition |
|---|---------|-----|-------------|
| 1 | "task-attached is safe" is wrong; Details/IssueDraftSpec are free-form | CRITICAL | FIXED — per-item classification + redaction + first-egress human gate |
| 2 | `BotFirstParty` "non-sensitive wiki" undefined without labels | CRITICAL | FIXED — FirstParty = task-linked wiki refs only until labels exist |
| 3 | StepIntent taint hand-waved; drives `WikiIndex.Search` | CRITICAL | FIXED — `StepIntent.Taint`; tainted intent cannot reach retrieval |
| 4 | `InjectionRecord` too weak for audit | HIGH | FIXED — added identity tuple, channel/ts, status, policy/profile versions, hash, redactions, failure reason |
| 5 | Delivery atomicity/idempotency unspecified | HIGH | FIXED — `IdempotencyKey` + pending→sent/failed; do not reuse generic dispatcher |
| 6 | Slack identity underspecified (bare id, spoofable name) | HIGH | FIXED — `BotIdentity` tuple; DisplayName never a trust input |
| 7 | Trust models ownership, not data handling | HIGH | FIXED — `BotDataHandling`; tier derived from it |
| 8 | Guards treated as a control | HIGH | FIXED — Guards labeled advisory; data withholding + action gate are the controls |
| 9 | Shared `ContextGatherer` wrong if identical queries | MEDIUM | FIXED — share retrieval+ranking primitives, not the bundle; `Classify` is packer-only |
| 10 | Push-side learning retrieval not task-scoped | MEDIUM | FIXED — shared primitive must add task scope (noted at `prompts.go:61`) |
| 11 | Wire shapes lack snapshot/version fields | MEDIUM | FIXED — plan id/version, TaskUpdatedAt, policy/profile versions on `ContextRequest` |
| 12 | Budget optimizes relevance over containment | MEDIUM | FIXED — Classify/redact runs before Budget; `Ask` length-capped + taint-checked |
| 13 | ReadScope auto-discovery fragile | MEDIUM | FIXED — explicit nonce probe + stored evidence |
| 14 | Specialties from display name is weak/manipulable | LOW | FIXED — observed task history + human confirmation |
| 15 | ThreadContext is channel-visible, not target-scoped | LOW | FIXED — classify vs least-trusted channel reader; prefer ephemeral/DM |
| 16 | Acceptance #3 overreaches (byte-identical bundles) | LOW | FIXED — relaxed to shared retrieval semantics + subset |

## Review dispositions (triangulation + verification, 2026-06-09)

Three orthogonal codex lenses (security/API/architecture) plus one adversarial
verification agent, scoped to `Classify` + `EgressPolicy` + redaction. Confidence
= number of independent agents that hit the same `file:line`. Several findings
were confirmed against real code (noted). All FIXED in the design above; three
also create code prerequisites tracked in **Before implementation**.

| # | Finding | Agents | Sev | Disposition |
|---|---------|--------|-----|-------------|
| H1 | `Ask`/`ReturnPact`/`Guards` are "never dropped" → bypass Classify (unredacted egress channel) | 4 | BLOCK | FIXED — Classify covers the whole envelope; a field that can't be sanitized fails closed |
| H2 | "redaction failure denies item" unbacked — `RedactSecretsForDisplay` returns no error, entropy caps & leaves over-cap secrets in `.Content`; no post-render scan (code-verified `scanner_detector.go:130/191`, `message_redaction.go:9`) | 4 | BLOCK | FIXED — fail-closed wrapper (deny on `Poisoned`/cap/error) + final byte-scan at Deliver; **code prereq** |
| H3 | `BotUntrusted` exports raw free-form `Details`/`IssueDraftSpec`; redaction catches tokens, not confidential prose/source/customer data | 3 | BLOCK/HIGH | FIXED — untrusted tier ships only redacted `Ask`/`ReturnPact`/plan step + human-approved excerpts (product call) |
| H4 | Trust-downgrade/version race: only `TaskUpdatedAt` rechecked at Deliver | 3 | HIGH/BLOCK | FIXED — Deliver re-validates `Target.Version` + `EgressPolicyVer` + live tier, aborts on change |
| H5 | Channel-visibility TOCTOU + `Allow(item, target)` can't express least-trusted reader | 4 | HIGH/BLOCK | FIXED — audience-aware `Allow`; non-DM thread = future-unknown-reader; sensitive context via ephemeral/DM |
| H6 | `LearningSearchFilters` has no `TaskID` — "task-scoped learnings" unimplementable (code-verified `learnings.go:107`) | 4 | HIGH | FIXED — named in reuse map; add `TaskID` filter + AND-scope or deny; **code prereq** |
| H7 | "task-linked wiki refs" has no backing link; only free `WikiIndex.Search` exists → implementer forced into the forbidden path | 2 | HIGH | FIXED — add task→article link or drop wiki from FirstParty; **code prereq** |
| H8 | First-egress gate keyed loosely — reinstall/grid-move/spoof inherits stale approval | 2 | HIGH | FIXED — keyed to verified install + identity tuple + data-handling fingerprint + trust tier + profile version; re-gates on any change |
| H9 | Taint caller-declared — CEO can launder foreign text into "clean" intent | 2 | HIGH | FIXED — taint derived structurally from `SourceRefs`; no copied spans |
| H10 | `BotHosted` double-inject (packer + push path) | 1 | LOW | ALREADY-COVERED — acceptance #3 already asserts the packer does not double-inject hosted agents |

No direct disagreements required escalation: severity divergences (architecture
BLOCK vs security HIGH on H3/H5) agreed *that* each was a problem; the higher
severity was taken.
```

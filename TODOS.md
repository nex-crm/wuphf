# TODOs

Tracking work that is deliberately deferred from the current branch. Each item names the trigger that would unblock revisiting it.

## Open

### 1. Validate customer signal frequency before it becomes load-bearing in marketing

**What:** Add explicit instrumentation to count how often users actually shred their existing office and rebuild from a different blueprint. The current observation ("users shredding to test different business ideas") is anecdotal.

**Why:** The multi-workspace v1 design (2026-04-28) ships with founder velocity as the primary signal and customer behavior as a secondary hypothesis. If multi-workspace ships and the rebuild-after-shred pattern is actually 1-2 anecdotes rather than a recurring behavior, marketing copy and product positioning that leans on the customer-facing argument will be off-tone.

**Pros:** Decouples product claims from unverified user behavior. Keeps the founder-velocity argument honest and load-bearing without overstating.

**Cons:** Adds analytics surface area. Requires deciding what "shred + rebuild" looks like instrumentally (timestamp delta on shred → next blueprint create within X days?).

**Context:** The 2026-04-28 office-hours design doc (`~/.gstack/projects/nex-crm-wuphf/najmuzzaman-feat-multi-workspace-design-20260428-125124.md`) flagged this in "Demand Evidence" and "Open follow-ups." The signal is currently founder-reported, frequency unspecified. Multi-workspace ships regardless — this TODO is about marketing accuracy, not product blocking.

**Depends on / blocked by:** Multi-workspace v1 must ship before we can measure the signal in the new UI surface. Then ~30 days of telemetry before drawing conclusions.

**Trigger to revisit:** First marketing copy pass that mentions multi-workspace, OR a v1.1 product decision that depends on customer-facing rationale.

---

### 2. Decide whether to rip broker auth entirely (separate design pass)

**What:** Evaluate whether bearer-token authentication on the local WUPHF broker is justified given the threat model (single-user single-machine). Today every endpoint except `/web-token` requires `Authorization: Bearer <token>`.

**Why:** Surfaced during multi-workspace review (2026-04-29) when the user asked "why do we have auth now if this is just a locally running cli?" Auth exists today to defend against rogue local browser tabs (CORS isn't airtight on localhost). But the cost in code surface (per-handler checks, peer-token map for cross-broker, token file lifecycle) is non-trivial.

**Pros:** Removing auth would simplify ~200 LOC across broker, web client, peer-token map, and the new multi-workspace design. Single-user CLI tools rarely run web auth.

**Cons:** Real attack surface today: any local web app on `localhost:*` could fetch broker endpoints and read team data, send messages, dispatch agents. CORS reduces but doesn't eliminate. Removing auth is a one-way door if WUPHF ever ships a hosted/multi-user variant.

**Context:** Multi-workspace v1 inherits the existing auth scheme via a new `withAuth` middleware. The middleware refactor reduces blast radius of auth changes. Once that lands, ripping auth is a clean follow-up — change the middleware to be a noop and audit the `/web-token` endpoint.

**Depends on / blocked by:** Multi-workspace v1's `withAuth` middleware must land first. Then a fresh design pass on the threat model, ideally with a security review.

**Trigger to revisit:** When the broker code surface around auth feels disproportionate to the threat, OR when a future feature needs to add another auth-required route and the per-handler check feels gratuitous.

---

### 3. Post-MVP: shared API keys via `~/.wuphf-spaces/keys.json` symlink

**What:** Today, multi-workspace forks API keys at create time (each workspace's `config.json` gets a copy). Future: a global `~/.wuphf-spaces/keys.json` that each workspace's `config.json` symlinks to (or reads as a fallback) so updating an API key in one place propagates everywhere.

**Why:** The fork-at-create pattern means rotating an API key requires re-pasting it into every workspace. For founders running 3-4 workspaces, that's friction at every key rotation (typically every few months for security).

**Pros:** Single point of truth for API keys. Rotations are one-touch. Matches the LLM-CLI auth model (`~/.codex`, `~/.claude` already global).

**Cons:** Couples workspaces by introducing shared mutable state. A malformed update breaks every workspace. Loses per-workspace key isolation (e.g., different LLM provider quotas per workspace).

**Context:** Multi-workspace v1 (2026-04-28 design) explicitly defers this and documents the fork semantics. The design notes "out of scope" but worth tracking because it directly affects founder velocity, which is multi-workspace's primary justification.

**Depends on / blocked by:** Multi-workspace v1 must ship. Then ~30 days of usage to see how often users actually rotate keys and whether the fork friction is real.

**Trigger to revisit:** First user complaint about "I changed my Anthropic key in workspace A but workspace B still has the old one," OR a security incident requiring a forced rotation.

---

## Closed

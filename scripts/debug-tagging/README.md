# debug-tagging — isolated repro rig for PR #218

## Purpose

The user-reported bug: *"Tagging any specialist agent apart from CEO is not
working. No response comes back."* PR #218 and PR #223 merged fixes and 17+
regression tests in `internal/team/mention_routing_bug_test.go` and
`internal/team/mention_auto_promote_test.go`. Those tests all pass on
`origin/main` at v0.0.6.2, yet the bug persists in the coworker's real install.

This rig stands up a completely isolated WUPHF instance so we can test the real
runtime path (HTTP → broker auto-promote → launcher targeting → headless
dispatch) without any contamination from the coworker's existing `~/.wuphf`
state.

## What it checks

- **Target computation** — Does the broker's `/messages` POST with
  `tagged: [pm]` get stored with `tagged=["pm"]`?
- **Headless dispatch** — Does PM's queue (`headless-codex-pm.log` or
  `headless-claude-pm.log`) get written? Does
  `headless-codex-latency.log` show `agent=pm stage=started`?
- **CEO absorption** — Was CEO *also* dispatched (expected in collab mode)?

If the specialist log shows nothing and CEO got the turn, we've reproduced the
bug and can bisect from there. If the specialist log has entries, the runtime
path is fine and the coworker's bug is state-specific — compare their
`~/.wuphf` to the sandbox.

## Isolation

- Custom `HOME` at `/tmp/wuphf-debug-tagging-home` — no touch to real state.
- Broker on `:7899`, web UI on `:7900` — no collision with default `:7890/:7891`.
- Pre-seeded `onboarded.json` + `config.json` — no wizard.
- Fake `claude` and `codex` binaries on `PATH` — the turn is dispatched but
  exits immediately; we're testing routing, not LLM quality.
- Nex disabled (`--no-nex`, `WUPHF_NO_NEX=1`).

## Usage

```bash
# Default: pack=founding-team, tag @pm in collab mode.
./scripts/debug-tagging/run.sh

# Tag a different specialist (must be in the pack):
SPECIALIST=fe ./scripts/debug-tagging/run.sh

# Different pack (roster must include SPECIALIST):
PACK=coding-team SPECIALIST=qa ./scripts/debug-tagging/run.sh

# Focus mode instead of collaborative:
MODE=focus ./scripts/debug-tagging/run.sh

# Leave the server running after the test for manual inspection:
KEEP=1 ./scripts/debug-tagging/run.sh
# -> broker at http://127.0.0.1:7899, web at http://127.0.0.1:7900
```

Exit code: `0` if the specialist was dispatched (fix works), `1` if not (bug
reproduced).

## Findings so far

Running this rig revealed that PR #218 only fixed *half* the round-trip. With
`HIRE_SLUG=qa-spec` the rig shows:

- Notification routing: ✅ `qa-spec` is correctly dispatched a turn
  (`agent=qa-spec stage=started` in `headless-codex-latency.log`).
- Reply posting: ❌ `fallback-post-error: channel access denied` in
  `headless-claude-qa-spec.log`.

The broker's `/messages` POST handler enforces
`canAccessChannelLocked(from, channel)` at `internal/team/broker.go:7078`,
which requires the sender slug to be in `ch.Members` for every non-lead
agent. CEO is hard-wired to bypass; wizard-hired specialists are not.

`handleOfficeMembers` with `action: create` (broker.go:5992–6060) appends the
new member to `b.members` but **never adds them to any channel's `Members`
array**. So the agent is hireable, taggable, and dispatches correctly, but
its reply is silently 403'd — the human sees nothing.

Quick confirmation:
```bash
# start fresh
HIRE_SLUG=qa-spec KEEP=1 ./scripts/debug-tagging/run.sh

# Inspect general's roster:
curl -s -H "Authorization: Bearer $(cat /tmp/wuphf-broker-token-7899)" \
  http://127.0.0.1:7899/channels | jq '.channels[] | select(.slug=="general") | .members'
# -> [ceo, pm, fe, be, ai, designer, cmo, cro]   <-- qa-spec is missing
```

This matches the coworker's symptom exactly: "tag a specialist → no response
comes back." Tags to pack-native agents work because those slugs are seeded
into `#general.members` at launch (broker.go:3146). Wizard-hired agents are
not.

### Suspected fixes

1. **handleOfficeMembers `action: create`** — add the new slug to all public
   channels (or at least `#general`) when the member is created. Symmetric
   with how `/channel-members` already handles the reverse.
2. **canAccessChannelLocked** — treat the agent's own reply to a thread they
   were tagged in as allowed, even if not in `ch.Members`. Parallel to PR
   #218's explicit-tag bypass on the read side.

Option 1 is more defensible — the bug is a missing side-effect on hire, not
a missing permission carve-out on write.

## If the bug still doesn't reproduce for the coworker

Compare our sandbox to their environment:

```bash
# On the coworker's machine:
curl -s -H "Authorization: Bearer $(cat /tmp/wuphf-broker-token)" \
  http://127.0.0.1:7890/channels | jq '.channels[] | {slug, members, disabled}'
curl -s -H "Authorization: Bearer $(cat /tmp/wuphf-broker-token)" \
  http://127.0.0.1:7890/office-members | jq '.members[] | {slug, provider}'
```

Differences to look for:
1. Is the failing specialist in `#general.members`? If not → same bug as above.
2. Does the specialist have `provider: { kind: "openclaw" }`? That routes
   through a different dispatch path not covered by PR #218's tests.
3. Is the specialist slug mismatched (e.g., UI shows `@PM` but the roster
   slug is `pm-eng` or `product-manager`)?

# Wiki Prune Signals — ICP Tutorial Examples

These three concrete personas drive the spec for PR 3 (prune signals). The
feature must work end-to-end for all three before the PR is ready.

The signal: every catalog entry carries a `word_count` and a derived
`prune_score`. Prune score formula:

```
PruneScore = (words * daysUnread) / max(humanReads + 0.3*agentReads, 1.0)
```

Higher score = more verbose AND more stale AND less read. Top-decile articles
get a `verbose` badge. `sort=prune_score` sorts descending.

---

## Example 1 — Alex, Account Executive

**Persona.** Alex is an AE who keeps a small wiki of customer briefs and
playbooks. He has not touched the wiki in six weeks while chasing a renewal.

**Input state.** Catalog has 5 articles. One of them, `team/playbooks/old-discovery.md`,
is 800 words long, has zero reads (no humans, no agents), and was last edited
45 days ago. The other four are 100-200 words and were read this week.

**Expected output.**

- `GET /wiki/catalog` returns 5 entries.
- The bloated playbook entry has `word_count: 800`, `days_unread: 45`, and
  `prune_score: 36000.0` (= 800 * 45 / 1.0).
- The other four have low `prune_score` (e.g. 100 words * 0 days / 1.0 = 0).
- In the catalog UI, `team/playbooks/old-discovery.md` shows the `verbose`
  badge because it sits in the top decile by prune_score (1 of 5 entries =
  20%, threshold index `floor(5 * 0.1) = 0` → top entry only).

---

## Example 2 — Jordan, CS Manager

**Persona.** Jordan runs CS for a mid-market book of business. The Slack
agent reads her playbooks every time it answers a customer question.

**Input state.** Same `team/playbooks/old-discovery.md` (800 words, 45 days
since last human edit), but the Slack agent has read it three times in the
last week. So `agent_read_count = 3`, `human_read_count = 0`,
`days_unread = 7` (last read seven days ago).

**Expected output.**

- `prune_score = 800 * 7 / max(0 + 0.3 * 3, 1.0) = 800 * 7 / 1.0 = 5600.0`
  (denominator clamps to 1.0 because 0.9 < 1.0).
- Score is meaningfully lower than Alex's case (5600 vs 36000) — agent reads
  matter, just less than human reads.
- If this is no longer in the top decile of her larger catalog, no `verbose`
  badge appears.

---

## Example 3 — Marcus, RevOps Lead

**Persona.** Marcus runs a wiki audit every quarter. He sorts by prune
score to surface the most bloated stale articles first.

**Input state.** Catalog of 20 articles with mixed prune_score values
ranging from 0 to 50000.

**Expected output.**

- `GET /wiki/catalog?sort=prune_score` returns entries sorted descending by
  `prune_score` — highest first.
- Tie-breaker is path (ascending), so output is deterministic across reloads.
- The top two articles in the response are the most prunable: high word
  count, long since read, no agent or human traffic.
- Marcus opens the top entry, sees the `verbose` badge in the catalog grid,
  and queues it for the compress button (PR 4).

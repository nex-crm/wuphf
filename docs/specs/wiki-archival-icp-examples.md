# Wiki Archival — ICP Tutorial Examples

Spec for `feat/wiki-archival` (PR 2 of 4).
The feature is done when all three work end-to-end.

**What archival means:** `WikiArchiver.Sweep` moves articles that have had
zero reads for ≥ 90 days to `.archive/<original-path>` and replaces the
original file with a tombstone (frontmatter `archived: true`). The daily
midnight system cron runs the sweep automatically. Tombstones are excluded
from the default catalog and deindexed from Bleve search; they reappear with
`?include_archived=true`.

---

## Example 1 — Priya, VP of Sales (stale customer brief gets archived)

**Persona:** Priya's team has a WUPHF workspace with ~200 entity briefs.
"NovaTech Solutions" was a prospect that went cold 18 months ago. The brief
has 0 human reads and 0 agent reads in the past 90 days.

**Setup:** `team/company/novatech-solutions.md` exists with 340 words.
`reads.jsonl` has no entries for this path in the past 90 days.
`commitBoundsByPath` reports the file was first committed 400 days ago.

**Step 1 — Daily sweep runs at midnight**

```
WikiArchiver{repo, readLog, cutoff: 90 days}.Sweep(ctx)
```

Archiver walks `team/`. For `novatech-solutions.md`:
- Age: 400 days since first commit > 90-day cutoff ✓
- Zero reads in cutoff window ✓
- Word count: 340 ≥ 50 (not a stub) ✓

Archiver moves the article:

```
.archive/team/company/novatech-solutions.md  ← full content
team/company/novatech-solutions.md           ← tombstone
```

Tombstone frontmatter:

```yaml
archived: true
archived_at: 2026-05-02T00:00:00Z
archive_path: .archive/team/company/novatech-solutions.md
```

Commits both files under archivist identity. Deindexes the tombstone from
Bleve. Returns `SweepResult{Archived: 1, Skipped: 0, Errors: 0}`.

**What Priya sees:** The NovaTech brief disappears from the wiki catalog and
search. If she navigates directly to the article URL, she sees the tombstone:
"This article was archived on 2026-05-02. It had 0 reads in the past 90
days." A link points to the archive path for recovery.

**Assert:** `SweepResult.Archived == 1`. Tombstone at original path has
`archived: true`. Full content in `.archive/` path. Bleve has no hit for
"NovaTech" in default search.

---

## Example 2 — Marcus, Customer Success Manager (short ghost stub is skipped)

**Persona:** Marcus's workspace has dozens of ghost stubs for entities that
were mentioned once in an email but never had enough facts to synthesize.
"Acme Micro" has a ghost brief with only 30 words — a two-line placeholder.

**Setup:** `team/company/acme-micro.md` exists with `ghost: true`, 30 words,
zero reads in the past 90 days.

**Step 1 — Sweep runs**

```
WikiArchiver{repo, readLog, cutoff: 90 days}.Sweep(ctx)
```

For `acme-micro.md`:
- Age: 120 days > 90-day cutoff ✓
- Zero reads ✓
- Word count: **30 < 50** — SKIP (too short to warrant archival overhead)

Returns `SweepResult{Archived: 0, Skipped: 1, Errors: 0}`.

**What Marcus sees:** The stub stays in the catalog. It is flagged as a ghost
brief but is not archived. The archiver leaves short stubs alone on the
assumption they may grow into real briefs as more facts land.

**Assert:** `SweepResult.Skipped == 1`. `team/company/acme-micro.md` is
unchanged. No archive entry created.

---

## Example 3 — Jordan, Customer Success Manager (recently-read article is kept)

**Persona:** Jordan's team has a brief for "BlueSky Corp" that they open once
a month before renewals. The article is 200 days old but was last read 15
days ago.

**Setup:** `team/company/bluesky-corp.md` exists with 280 words.
`reads.jsonl` has an entry for this path 15 days ago.

**Step 1 — Sweep runs**

```
WikiArchiver{repo, readLog, cutoff: 90 days}.Sweep(ctx)
```

For `bluesky-corp.md`:
- Age: 200 days > cutoff ✓
- **Last read 15 days ago — within 90-day window → SKIP**

Returns `SweepResult{Archived: 0, Skipped: 1, Errors: 0}`.

**What Jordan sees:** The BlueSky Corp brief stays in the catalog, fully
searchable. The archiver never touches articles that have been accessed
within the cutoff window, regardless of how old the file is.

**Assert:** `SweepResult.Skipped == 1`. `team/company/bluesky-corp.md`
unchanged. No archive entry.

---

## Sweep logic (all three examples together)

An article is archived when ALL conditions hold:

1. File age ≥ cutoff (days since first commit via `commitBoundsByPath`)
2. Days since last read ≥ cutoff (from `readLog.Stats`)  
   — articles never read: `LastRead == nil`, treated as unread since the
   first commit date
3. Word count ≥ 50 (skip stubs)
4. Not already a tombstone (`archived: true` in frontmatter)

Any condition fails → skip.

## Catalog and search behaviour

| Surface | Default | With `?include_archived=true` |
|---|---|---|
| `GET /wiki/catalog` | excludes tombstones | includes tombstones |
| Bleve search | no hits for archived articles | n/a (tombstones deindexed permanently) |
| `GET /wiki/article?path=…` | returns tombstone (so UI can show archive notice) | same |

## Implementation checklist (PR 2 scope only)

- [ ] New file `wiki_archiver.go` — `WikiArchiver` struct + `Sweep` method
- [ ] `Sweep` uses `commitBoundsByPath` batch for age, `readLog.AllStats()` for read recency
- [ ] Tombstone written at original path; full content moved to `.archive/<original-path>`
- [ ] Both files committed under archivist identity in a single git commit
- [ ] Bleve: deindex tombstone after commit (call `repo.IndexDeletePath`)
- [ ] `BuildCatalog`: skip entries where frontmatter `archived: true` (default); include when `?include_archived=true`
- [ ] `GET /wiki/archive/sweep` admin endpoint — runs sweep synchronously, returns `SweepResult` JSON
- [ ] `wiki-archive-sweep` system cron — `1440` min default (daily), `MinFloor: 60`
- [ ] `startArchiveSweepLoop` goroutine started from `broker.go`
- [ ] Tests: all 3 ICP scenarios + `BuildCatalog` exclusion + admin endpoint

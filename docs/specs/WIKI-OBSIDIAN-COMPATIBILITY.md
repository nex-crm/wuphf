# WIKI-OBSIDIAN-COMPATIBILITY.md — Contract for opening WUPHF's wiki as an Obsidian vault

This document is the source of truth for **how WUPHF's wiki interoperates with Obsidian**. It is a companion to `WIKI-SCHEMA.md`: that document defines the wiki contract; this one defines the additional invariants required to make the on-disk layout work as an Obsidian vault (read and write).

If a decision below conflicts with code, the contract wins. Fix the code.

This is the Obsidian-side counterpart to the substrate guarantee in `WIKI-SCHEMA.md` §1: "`rm -rf .wuphf/index/` → restart broker → the wiki still works." The Obsidian extension is: open the wiki in Obsidian → the vault loads cleanly. Edit a brief in Obsidian → the next reconcile picks it up. Click a wikilink → it resolves to the right brief.

---

## 1. Goal

A user can:

1. Point Obsidian at `~/.wuphf/wiki/team/` and the vault loads cleanly. No `.jsonl` files in the file tree, no broken-link spam, link graph reflects the entity graph.
2. Edit a brief in Obsidian; the next WikiWorker reconcile picks it up, attributes the commit, and graph/index stay consistent.
3. Use Obsidian-native primitives that have semantic meaning for WUPHF — `[[wikilinks]]`, `![[image]]` embeds, callouts — and have them survive round-trips through synthesis without stomping user edits.
4. Open Obsidian's Graph view and see a structure that matches the typed adjacency log at `team/entities/.graph.jsonl`.

---

## 2. Vault root — `team/`

The Obsidian vault root is `<wiki-root>/team/`, not the wiki root itself. The wiki root is the git repository; the vault is a subdirectory of it.

This choice is load-bearing for compatibility:

- **Wikilink resolution.** WUPHF emits `[[people/nazz]]`. Obsidian's resolver looks for `<vault>/people/nazz.md`. With vault root at `team/`, that path matches `team/people/nazz.md` directly. With vault root at the wiki root, Obsidian would fall back to basename matching and silently mis-resolve collisions.
- **Noise suppression.** Sibling directories `wiki/` (facts, artifacts, lint reports, DLQ), `.reads/`, `.reviews/`, `index/`, and `.git/` all live outside the vault and never appear in Obsidian's file tree.
- **Single bootstrap point.** `.obsidian/` lives at the vault root, so it lives inside `team/`. It is committed to the wiki git repo so a fresh clone of the wiki gives the user a working Obsidian configuration.

Hidden directories that remain inside `team/` (and must be explicitly excluded by the bootstrap — see §4):

- `team/playbooks/.compiled/` — derived skill files
- `team/entities/.graph.jsonl` — adjacency log
- `team/inbox/raw/` — immutable artifacts (visible but read-only by convention)
- `team/agents/` — runtime skill manifests (visible)

---

## 3. Layout invariants

Everything Obsidian needs to render lives under `team/` and matches the brief layout in `WIKI-SCHEMA.md` §3. The full set, with vault-root-relative paths:

| Path (vault-relative) | Kind | Author | Obsidian-visible |
|---|---|---|---|
| `people/{slug}.md` | brief, md+YAML | archivist or human | ✅ |
| `companies/{slug}.md` | brief, md+YAML | archivist or human | ✅ |
| `customers/{slug}.md` | brief, md+YAML | archivist or human | ✅ (lazy-created) |
| `projects/{slug}.md` | brief, md+YAML | archivist or human | ✅ |
| `playbooks/{slug}.md` | playbook, md+YAML | human | ✅ |
| `learnings/{slug}.md` | learning, md+YAML | archivist or human | ✅ |
| `decisions/{slug}.md` | decision, md+YAML | human | ✅ |
| `inbox/raw/**.md` | artifact, immutable | extractor | ✅ (read-only) |
| `agents/**` | skill manifest | broker | ✅ |
| `playbooks/.compiled/**` | derived | archivist | hidden |
| `entities/.graph.jsonl` | adjacency log | archivist | hidden |

**Lazy directory creation.** `customers/` is not created by `Repo.ensureLayoutLocked` (`internal/team/wiki_git.go:209`); it is created on demand when the first customer brief is filed. Obsidian handles a missing directory gracefully — wikilinks to it surface as broken until the first file lands. This is the documented pattern; do not pre-seed `customers/` just to satisfy Obsidian.

---

## 4. `.obsidian/` bootstrap (Phase 2)

On `Repo.Init` and on every `ensureLayoutLocked`, WUPHF idempotently writes a minimal `.obsidian/app.json` at the vault root. It is **committed** to the wiki git repo — a fresh clone gives the user a working configuration without further setup.

Bootstrapped settings:

| Setting | Value | Reason |
|---|---|---|
| `useMarkdownLinks` | `false` | Force `[[wikilink]]` form, not `[label](url)` |
| `newLinkFormat` | `"absolute"` | New links typed in Obsidian use vault-absolute paths (`people/nazz`), matching our convention |
| `alwaysUpdateLinks` | `false` | Obsidian does not silently rewrite links when files are renamed (the watcher pipeline owns rename + redirect) |
| `attachmentFolderPath` | `"inbox/raw"` | Pasted images flow into the artifact directory the extractor already watches |
| `userIgnoreFilters` | `["playbooks/.compiled/", "entities/.graph.jsonl"]` | Hide derived/internal files from the file tree |

Not bootstrapped (user-specific, gitignored):

- `.obsidian/workspace.json` (pane layout)
- `.obsidian/workspace-mobile.json`
- `.obsidian/graph.json` (graph view tuning)

`.obsidian/.gitignore` entries for those files are written on first bootstrap and never overwritten.

**Idempotency.** If `.obsidian/app.json` already exists, the bootstrap reads it, merges WUPHF's required keys with any user customizations (user values win on conflict for non-required keys; WUPHF values win for `useMarkdownLinks`, `newLinkFormat`, `userIgnoreFilters`), and rewrites the file. User customization of other keys (theme, hotkeys, plugins) is preserved.

---

## 5. Wikilink contract — kinded form is canonical

WUPHF emits and stores wikilinks in their **kinded form**: `[[kind/slug]]` or `[[kind/slug|Display]]`. The parser at `internal/team/entity_graph.go:87` accepts `people|companies|customers` as kinds. This form is canonical because:

1. It is unambiguous — `[[people/acme]]` and `[[companies/acme]]` are distinct entities even when slugs collide.
2. It resolves natively in Obsidian when the vault root is `team/` — `[[people/nazz]]` → `<vault>/people/nazz.md`.
3. It survives round-trips through synthesis without loss.

**Loose forms** users type in Obsidian (`[[Acme Corp]]`, `[[acme]]`) are accepted but normalized to the kinded form by the watcher commit pipeline (Phase 4). The normalizer:

- Resolves the display text against the SQLite signal index (exact email, normalized name, fuzzy ≥ 0.9).
- Rewrites `[[Acme Corp]]` to `[[companies/acme-corp|Acme Corp]]`, preserving the display string.
- Commits the normalization under the `archivist` identity with message `wiki: normalize wikilinks in {slug}`.
- Runs only on brief bodies under `team/**/*.md`. Artifacts under `inbox/raw/` are immutable and preserve verbatim text.

**Bare slug ambiguity.** A bare `[[acme]]` whose slug collides across kinds is left unresolved. The `ExtractRefs` parser receives a `known(slug)` callback that returns `("", false)` for ambiguous slugs (`internal/team/entity_graph.go:125-146`), and no edge is created. This is the basename-collision guard required for Obsidian compatibility — see the regression test in `entity_graph_test.go`.

---

## 6. Round-trip writes — coexistence with the single-writer invariant

`WIKI-SCHEMA.md` §1.3 establishes the single-writer invariant: all writes go through the WikiWorker queue. Obsidian writes directly to disk and breaks this invariant by construction.

The Phase 3 reconciliation:

1. **fsnotify watcher on the vault root.** Debounce 1.5s. Commit batches under the user's configured per-human git identity (same resolution used by WikiWorker; falls back to a "needs-attribution" queue if unresolved — never commits as `archivist` for human writes).
2. **Advisory `flock` in `Repo.Commit`.** WikiWorker takes an OS-level advisory lock on the target file for the duration of write + commit. Obsidian's editor respects advisory locks on most platforms; on platforms where it does not, the watcher's debounce window absorbs interleaved writes.
3. **`last_human_edit_ts` frontmatter sentinel.** The synthesizer checks `last_human_edit_ts > last_synthesized_ts` and switches from rewrite mode to append-section mode. User edits to the brief body are never stomped; synthesis-derived content lands in `## What we've learned` instead.
4. **Worker-originated write filter in the watcher.** The watcher tracks paths the worker has written within the last 5 seconds and ignores fsnotify events on them. Without this, every worker commit would trigger a watcher commit and the system would loop.

---

## 7. Obsidian-native primitives

The following primitives are supported (Phase 4):

### 7.1 Callouts

Obsidian-flavored callouts (`> [!note]`, `> [!info]`, `> [!warning]`, etc.) render in WUPHF's web wiki surface. The renderer in `web/src/components/wiki/WikiArticle.tsx` recognizes the blockquote-with-bang-tag prefix and emits a styled box matching the design system in `DESIGN-WIKI.md`.

Callouts have no structured-extractor semantics in v1 — they render but do not feed the fact log. See §10 open items.

### 7.2 Image embeds (`![[image.png]]`)

`![[image.png]]` references in brief bodies resolve against the vault attachment folder (`inbox/raw/`). The watcher pipeline:

- Detects new `![[...]]` references on commit.
- Copies the referenced media into `inbox/raw/` if not already there (Obsidian sometimes paste-saves into the brief's directory; we normalize).
- Rewrites the reference to use the canonical path.

Embeds of `.md` files (transclusion) are out of scope for v1.

### 7.3 Frontmatter tags

Obsidian uses `tags` and `aliases` as special frontmatter fields. WUPHF already uses `aliases` with matching semantics. `tags` is added as a derived projection at synthesis time: `kind`, key signals (`job_title`, `domain`), and playbook author land in `tags` so Obsidian's tag panel works. The projection is additive — readers that do not know about `tags` ignore it.

---

## 8. Non-goals

The following are explicitly out of scope for the foreseeable future:

- **Obsidian Canvas** (`.canvas` JSON files).
- **Dataview** queries — WUPHF has its own query layer.
- **Templater** — Obsidian's template plugin is a local-only convenience.
- **Excalidraw** and other drawing plugins.
- **Obsidian Sync** as a replacement for the wiki git transport.
- **Bidirectional sync of arbitrary external Obsidian vaults** into WUPHF. Importing an external vault is a separate spec.
- **Dark-mode parity** between Obsidian and WUPHF's web wiki surface (`DESIGN-WIKI.md` §1.7 — light-only in v1).
- **Mobile Obsidian.** Desktop only.

---

## 9. Phased plan

- **Phase 1 (this commit).** This document. Schema-doc drift fix for playbook paths. Regression test naming the basename-collision guard.
- **Phase 2.** `.obsidian/` bootstrap in `Repo.ensureLayoutLocked`. Vault-root migration note for existing users.
- **Phase 3.** fsnotify watcher, advisory `flock` in `Repo.Commit`, `last_human_edit_ts` sentinel, synthesizer guard.
- **Phase 4.** Loose-link normalizer, Obsidian callout rendering, image embed ingestion, `tags` projection.

Each phase ships behind a single reviewable PR. Phase 2 depends on Phase 1's vault-root decision being public; Phase 3 depends on Phase 2's bootstrap; Phase 4 depends on Phase 3's watcher.

---

## 10. Open items (flagged, not resolved in Phase 1)

1. **Insights drift.** `WIKI-SCHEMA.md` §3 documents `wiki/insights/entity/{slug}.jsonl` and `wiki/insights/knowledge/{slug}.md`. Neither path is referenced in code. Either the insights layer is aspirational (and should be marked as such in `WIKI-SCHEMA.md`) or it has been superseded by something else (e.g. promoted facts). Out of scope for Phase 1 of this work, but tracked here so it does not get forgotten when Phase 4 ships `tags`.
2. **Two graph files.** `graph.log` (wiki root, typed predicates per `WIKI-SCHEMA.md` §6.2, referenced by `wiki_index.go`) and `team/entities/.graph.jsonl` (auto-generated adjacency log per `entity_graph.go`). Both exist; their relationship is not documented in either file. If they are intentional duplicates (one human-editable, one derived), say so. If one is dead code, remove it. Tracked for a future round.
3. **Structured callouts.** Whether `> [!fact]`, `> [!decision]`, `> [!question]` should feed the extractor as deliberate filings is deferred to v2. Need usage data on Obsidian-side callout use before defining the predicate vocabulary.
4. **Per-vault git identity sniffing.** Current plan is to reuse WikiWorker's resolved per-human identity. If multi-tenant runtimes need a different identity per vault (e.g. one machine, two users sharing a wiki), this needs an explicit answer.

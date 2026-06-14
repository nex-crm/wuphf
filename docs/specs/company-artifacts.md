# Artifact → task attribution in the wiki

> Status: implemented. Branch `feat/company-artifacts`.

## One line

The wiki already *is* the artifacts agents produce. The only thing missing was
**which task produced a given article** — so each article now shows a "Produced
for **<task>**" link, in place. No separate Artifacts surface.

## Why

Artifacts are work outcomes, and in WUPHF those outcomes already live in the
wiki: HTML visual artifacts (`wiki/visual-artifacts/*.html`) and markdown
deliverables (`team/**.md`). What was missing was provenance — opening an
article told you nothing about the task that produced it. This adds that link,
naturally, where the article already renders. (Earlier drafts of this branch
built a separate company-wide "Artifacts" page/tab; that was the wrong model —
the wiki is the artifacts — and was removed.)

## Data model — no new store

Pure derivation over links we already record, in priority order:

1. A task that declared the article as its delivered work product
   (`teamTask.Artifact == ref`).
2. A visual artifact that names its originating task (`RichArtifact.RelatedTaskID`),
   keyed by the artifact id (`ra_…`) or its promoted wiki path.

`ref` is a visual-artifact id (`ra_…`) or a wiki-relative article path
(`team/playbooks/launch.md`). A ref whose task was deleted resolves to nothing.

## API

`GET /article-attribution?ref=<id|path>` — `requireAuth`-gated, registered next
to `/notebook/visual-artifacts`. Always 200; body is
`{ "attribution": { task_id, task_title, owner } | null }`. `null` = no
producing task found (so the chip simply renders nothing).

## Frontend

A single reusable `<ArticleAttribution articleRef={…} />` chip (React Query,
renders nothing until/unless a producing task resolves), dropped into:

- the visual-artifact viewer (`ArticleView`, `ref = articleId`), and
- the wiki markdown read view (`WikiArticle`, `ref = path`).

The chip links to the producing task (`#/tasks/<id>`). Token-only styling.

## ICP scenarios

1. **Maya opens a generated teardown article.** The header reads "Produced for
   OFFICE-3 · Q2 pricing launch · @revops"; one click jumps to the task.
2. **An operator browses a wiki playbook.** If a task delivered it, the same
   provenance chip appears; if it was hand-authored, nothing extra shows.
3. **A deleted task's old artifact.** No stale link — the chip stays hidden.

## Non-goals

- No Artifacts page/tab/sidebar entry. The wiki is the surface.
- No new artifact store or projection. Pure link resolution.
- Generated-image indexing / upload are out of scope here.

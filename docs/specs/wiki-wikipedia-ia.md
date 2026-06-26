# Wikipedia-style wiki IA — design spec

> Status: design approved (2026-06-18). Phase 0 (WIKI-SCHEMA contract amendment) landed.
> This is the authoritative spec for the wiki reorg; `WIKI-SCHEMA.md` is the machine contract,
> this doc is the design rationale + the Wikipedia-fidelity standard the implementation follows.
> Detailed phased plan: `~/.claude/plans/wobbly-sleeping-kitten.md` (+ `-agent-…` file:line version).

## 1. Context & goal

Today the wiki organizes articles by **fixed top-level folders** under `team/`
(people / companies / customers / playbooks / …) and an article's "category" in the UI is just
its first path segment. This causes the customer-reported problems: insight/concept articles get
filed into entity folders (`team/companies/hubspot-mql.md`, `linear-ci.md` are insights, not
companies), the nav is rigid, there's no home for non-entity **concept** articles, and agents
dump standalone "insight" pages instead of folding knowledge into the right article.

**Goal: make the wiki behave like Wikipedia** across four dimensions — **navigation/IA**,
**article writing**, **associations**, and the **agent read/write/query** paths — because the
wiki is the agents' compounding knowledge substrate, not only a human reading surface.

## 2. Approach: a category LAYER, not a physical flatten

Keep the on-disk folder layout (preserves the load-bearing invariants below); add a
**category + concept layer** on top. Folders become an invisible implementation detail;
**categories drive navigation**. This is exactly the MediaWiki model (flat main namespace +
Category namespace), achieved without breaking the substrate.

**Load-bearing invariants the design must not break** (from `WIKI-SCHEMA.md`):
- **Substrate/rebuild:** `rm -rf index/` + restart rebuilds identically from markdown; every new
  index layer is derived + added to `ReconcileFromMarkdown` + `CanonicalHashAll`.
- **Single-writer:** all writes via `WikiWorker.Enqueue*` under a git identity. No direct writes.
- **Slug immutability + redirects:** never rename in place; re-file uses redirect stubs (§7.2).
- **`[[kind/slug]]` wikilink grammar + Obsidian vault compatibility** stay intact.

## 3. The Wikipedia IA model (what we are mirroring)

Grounded in **WP:LAYOUT** (Manual of Style/Layout) and **Help:Category** (MediaWiki category model).

### 3.1 Namespaces / IA
- **Flat article namespace** (Main): entities AND concepts live in one flat-feeling namespace;
  there is no folder hierarchy for articles.
- **Category namespace**: categories are **first-class pages** (`Category:X`) with a description
  and **parent categories**, forming a **subcategory tree (DAG)**. A category page auto-lists its
  **subcategories + member articles alphabetically**.
- **Redirects**: alternate titles → canonical (we already have the mechanism; add the on-disk
  `redirects.md` mirror). **Disambiguation pages** for ambiguous titles.

### 3.2 Article-writing standard (WP:LAYOUT order, top → bottom)
Every generated/synthesized article emits, in this order:
1. **Hatnote(s)** — e.g. "Redirected from X", disambiguation pointers.
2. **Infobox** — structured key facts (our `## Summary` definition list).
3. **Lead section** — no heading; the **first sentence bolds the title and defines it**
   ("**X** is a {kind}…"); summary-style; **encyclopedic, neutral, third person**; stands alone.
4. **Body sections** — `##` / `###` headings, no skipped levels, single blank line between.
5. **Appendices, in this exact order:** `## See also` (bulleted **blue** links to related articles
   only — no red links, no disambig, no external) → `## References`/Notes (footnote citations) →
   Further reading → External links.
6. **Foot:** navbox(es) (grouped related-article navigation) → **categories rendered at the very
   bottom**.

### 3.3 Categories (Help:Category)
- `categories:` frontmatter ≈ `[[Category:X]]`: **many-to-many**, assigned by **defining
  characteristics** (not subjective traits); **every article in ≥1 category**.
- Categories are **pages** with `parent_categories:` → the nav tree is real (built on demand from
  category→parent edges), not folder containment.

### 3.4 Associations (the full Wikipedia set)
- **Wikilinks** `[[kind/slug]]` / `[[concepts/slug]]` — the primary association; wikilink every
  entity AND concept **on first mention**.
- **Categories** — classification association.
- **Backlinks / "What links here"** — reverse links (we have the bidirectional entity graph;
  surface it as a backlinks view agents and humans can query).
- **`## See also`** — curated related-article links.
- **Navboxes** — grouped related-article nav at the foot (generated from the graph / category
  co-membership).
- **Redirects** + **disambiguation** pages.

## 4. WUPHF ↔ Wikipedia mapping

| Wikipedia | WUPHF implementation |
|---|---|
| Main (article) namespace, flat | `team/**/*.md` (folders invisible; nav by category) |
| Entity vs concept article | `type: entity` (people/companies/customers) vs `type: concept` (`team/concepts/{slug}.md`) |
| `[[Category:X]]`, many-to-many | `categories:` frontmatter (derived index) |
| Category page + subcategories | category page model + `parent_categories:`; `_category/<slug>` pseudo-path nav |
| Lead ("X is a Y") | deterministic article lead (entity_article.go / concept_article.go) |
| Infobox | `## Summary` definition list (lifted to a right-rail infobox by the reader) |
| See also | `## See also` (curated; reshaped from today's `## Associated`) |
| References/footnotes | `## References` + `[^n]` citations to tasks/artifacts |
| Navbox / What links here | EntityGraph in/out-edges → navbox + backlinks view |
| Redirect | redirect stub (`canonical_slug`/`redirect_to`) + `redirects.md` mirror |
| `[[wikilinks]]` | `[[kind/slug]]` wikilink grammar (concepts = `[[concepts/slug]]`) |

## 5. Agent-facing model (write · find · associate)

The wiki is the agents' substrate; the Wikipedia model governs their paths:

**WRITE (Wikipedia-faithful authoring).**
- Extraction (`prompts/extract_entities_lite.tmpl`, `ValidEntityKinds()` entity_facts.go:52):
  recognize **concepts** as a kind so mentions link to `[[concepts/slug]]` and emit
  entity↔concept relationships (today only person/company/customer).
- Synthesis prompt (`SynthesisPromptSystem`) + librarian prompt (`librarian.go`): write the lead,
  assign `categories:` by **defining characteristics**, put related links in `## See also`,
  wikilink every entity/concept on first mention, and **fold insights under a header** in the most
  relevant article — never a standalone page.
- **`WIKI-SCHEMA.md §10`** (the opening directive every wiki prompt references) gains the Wikipedia
  authoring rules — highest-leverage single edit; all prompts inherit it.

**FIND (retrieval via the graph, not folders).**
- `context_assembler.go` + `prompt_builder.go`: assemble an agent's turn context from
  **categories** (same-category articles) + **wikilinks** + **backlinks (What links here)** +
  **concept articles**, not just the entity's own brief.
- Index `categories` + concept articles in bleve/sqlite; add **category-scoped retrieval**.
- Expose a **"What links here"** backlinks query (EntityGraph in-edges).

**ASSOCIATE (entities AND concepts).**
- `entity_graph.go`: add **concept nodes** + entity↔concept / concept↔concept edges (`Coalesce`/
  `Query` are already kind-generic; the gate is `ValidEntityKinds` + extraction emitting refs).
- Associations rendered as `## See also` + navbox + foot categories from the graph + category index.

**Net effect:** agents file encyclopedic entity/concept articles, categorize by defining trait,
wikilink+backlink associations, fold new insights under the right header, and retrieve by
category/link/backlink — so knowledge compounds and stays findable, like Wikipedia.

## 6. Phased delivery (each = one shippable PR)

0. **Schema amendment** (✅ done) — `type`, `categories`, `team/concepts/`, `redirect_to`,
   redirects.md writer, §7.2 re-file, §12 changelog. Follow-up: add `parent_categories`, the
   WP:LAYOUT order, the Category-page model, disambiguation, and the §10 agent-authoring rules.
1. **Category derived index** (✅ done) — `article_categories` on both stores (sqlite +
   in-memory), reconcile hook in `reconcileEntityBrief`, folded into `CanonicalHashAll`
   (backend-agnostic), `meta.Categories` populated from frontmatter; `rm -rf index`
   rebuild-equality + backend-parity tests. **Re-sequence:** the category→parent (subcategory
   tree) edges moved to Phase 3 — their only data source is category pages, which Phase 3
   authors; an always-empty parent table here would be speculative plumbing, and
   `CREATE TABLE IF NOT EXISTS` keeps the Phase 3 addition non-breaking.
2. **Category API** (✅ done) — `GET /wiki/categories` (cached list, slug+title+count) and
   `GET /wiki/categories/{slug}` (live member articles) mirroring `/wiki/sections`: debounced
   in-memory cache fed by wiki:write, `wiki:categories_updated` SSE, reads the derived index.
   Frontend stubs (`fetchCategories`/`fetchCategory`/`subscribeCategoriesUpdated`) in `web/src/api/wiki.ts`.
3. **Flip nav to categories** (the visible win) — re-point `WikiCategoryPage`/`CategoriesFooter`/
   `Wiki.tsx` from folder `group` to real categories; introduce **category pages** (with
   `parent_categories:`) + the category→parent derived edges and render the **subcategory tree**;
   backfill so nav is never empty.
4. **Concept articles + agent authoring** — `EntityKindConcepts`; `concept_article.go` mirroring
   `entity_article.go` in **WP:LAYOUT order**; extraction recognizes concepts; librarian/synthesis
   prompts + §10 updated; `## Associated` → `## See also`; categories at foot.
5. **Insights fold under headers** — `wiki_section_merge.go` (`mergeUnderHeader`, fence-safe,
   outside sentinel regions); agent names target; no target → notebook for librarian promotion.
6. **Re-file via redirects** (riskiest, last) — `wiki_redirect.go` (`redirects.md` writer +
   `Refile` = survivor + stub, never rename) + `DetectMisplaced`; "Redirected from" hatnote;
   human/librarian-confirmed.

Cross-cutting **agent retrieval + backlinks ("What links here") + navbox** work rides Phases 3–4.

## 7. Verification (per phase)
- Go: `bash scripts/test-go.sh ./internal/team`; `gofmt`, `go vet`, **`golangci-lint run`**
  (catches staticcheck — not just vet). Web: `bash scripts/test-web.sh`, `bunx tsc --noEmit`,
  `bunx biome check`. Substrate test: `rm -rf index` rebuild ⇒ `CanonicalHashAll` stable.
- Live (browser-harness): category nav renders the subcategory tree; a concept article opens in
  WP:LAYOUT order; an insight folds under a header; a re-filed article shows the hatnote.
  **Run the test broker on isolated `--web-port` AND `--broker-port` (7900+) — never 7890/7891 (prod).**

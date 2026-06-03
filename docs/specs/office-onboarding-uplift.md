# v2 (2026-06-02) — CONVERGED ARC + VERIFIED PROVIDER PICKER (supersedes the AUGMENT scope below)

Course-correction after live review. The v1 AUGMENT scope under-delivered: it kept WUPHF's
wizard untouched and added the tour as a SECOND full-screen gate over the already-mounted office,
which reads as "two onboardings," and it dropped the reference product's guided + verified provider
picker. v2 fixes both.

## A. One converged arc (the office mounts ONCE, at the very end)

Target flow, single uninterrupted full-screen canvas:
`PrePick (verified provider) → CEO chat (greet→team) → CEO: "Come on, I'll show you around" → TOUR (4 slides) → "Write your first issue" → OFFICE (composer prefilled)`

- The tour is onboarding's FINAL ACT, not a modal over the office. Remove the modal-over-Shell
  behavior. `RootRoute` mounts the office `Shell` only once the tour is finished or skipped.
- Gate: while `onboardingComplete === true` AND the office tour has not been seen
  (`localStorage: wuphf.office-tour-done`), render `<OfficeTour>` as the full surface (no Shell
  behind it). The tour's finish/skip marks it done, then the `Shell` mounts and the finish handoff
  prefills the CEO DM composer (existing `pendingComposerDraft` path).
- Narrate the seam: the tour's intro reads as the CEO walking you around (hand-off from the chat),
  so chat → tour feels continuous, not like a new product. No office flash between them.

## B. Verified, guided provider picker (port the reference gem in WUPHF style)

Reference behavior to reproduce: a provider list with live status (Ready / Login required / Not
installed, colored, version shown), an expandable **guided setup** (numbered steps with copyable
commands, an "Open terminal" action, doc links), and a **Verify** button that runs a real check
returning `pass | not_installed | auth_required` (+ `payment_required`/`quota_exceeded` later),
shows a hint, and highlights the failed step. "Next" when a provider is ready, else "Skip for now."

WUPHF already has the backend primitives in `internal/onboarding/prereqs.go` (`CheckOne`,
`runtimeSessionProbes`, `SignedIn`, `SignInCommand`). v2 adds:
- **Backend:** a verify endpoint (e.g. `POST /onboarding/verify` `{runtime}`) returning
  `{status, command, hint, failedStep?}` by reusing `CheckOne` + the session probe
  (not found → `not_installed`; found + not signed in → `auth_required` with `SignInCommand`;
  found + signed in → `pass`). Per-runtime `installSteps` metadata (title/detail/command/link).
- **Frontend:** uplift `PrePickScreen` so a selected runtime expands into the guided setup + a
  Verify button + classified result with a hint, in WUPHF tokens/voice/themes. Keep the existing
  detection + copy-sign-in; add the guide + verify loop on top.

Everything in WUPHF style: design tokens only, three themes, no em dashes, no contractions, Office
voice, "WUPHF is not a CRM" framing intact. The RevOps reskin (steward/analyst, CRM hygiene) stays.

---

# Office Onboarding Uplift — bring the best of the reference onboarding into WUPHF

> Branch: `feat/office-onboarding-uplift`
> Hard rules honored throughout: no reference-product name in code, copy, or filenames;
> never framed as a CRM; no em-dashes and no contractions in new copy; design tokens only;
> reuse `web/src/components/ui/` primitives; follow the existing `onboarding.css` class + token pattern.

## 1. Scope recommendation — AUGMENT

Keep WUPHF's CEO-chat phase wizard as the entire setup act, and add the three pieces WUPHF is
genuinely missing:

1. A post-onboarding **guided office tour** reimagined in the office metaphor.
2. Richer **seeded getting-started knowledge** wired into the wiki, so the office is never empty.
3. A rendered **getting-started checklist UI** for the already-dormant Go `DefaultChecklist`.

**Rationale.** WUPHF's wizard-as-conversation-with-the-CEO is load-bearing product identity, not
just an onboarding mechanic: it reuses the real `useMessages` / `MessageBubble` / `InterviewBar`
chat plumbing, it is deterministic and injection-safe (`sanitizeCEOPayload` plus the frontend
`sanitizeStructuredPayload` from PR #684), it is resumable and migration-safe, and it teaches the
core metaphor (you talk to your team, they ship) before the user is even in the office. The
reference product's multi-step form wizard is strictly worse than this for WUPHF's "tell, do not
ask, simplest path first" principles, so replacing it would be a regression. What the reference
product does better is the second act: a guided tour with a live mockup sidebar that auto-opens
once per browser and hands off into the real first-task composer, plus a real seeded knowledge
base. Those are pure additive wins.

## 2. Mapping table

| Reference surface | WUPHF target | Action | Notes |
|---|---|---|---|
| Intro step (dictionary cold-open) | `OfficeTour` slide 1 | Adapt | Retell as "what an office is" in WUPHF voice, serif `--font-logo` headline. |
| Welcome / "what is this" / room setup steps | CEO greet + blueprint + team phases | Drop | Already covered, far better, by the CEO chat. |
| Connect AI step (provider + verify) | `PrePickScreen` (exists) | Drop | WUPHF runtime picker + copy-sign-in-command (#932) already exceeds this. |
| Hire agent step | CEO blueprint/team + `OfficeTour` slide 2 | Adapt | Hiring stays in the wizard; the explanation moves to the tour. |
| First task step | CEO `bridge` phase + tour finish handoff | Adapt | Tour finish drops the user into a real composer prefilled with an issue intent. |
| Community step (GitHub / Discord / Cloud) | `GettingStartedChecklist` items | Adapt | Surfaced as the dormant `DefaultChecklist`. No "Cloud" upsell. |
| Launch step | CEO `seed` + `complete` phases | Drop | Seed + land-in-`#general` already cover launch. |
| **Tour modal + slides Intro/Data/Agents/Tasks** | `OfficeTour` + `useOfficeTour` | **Net-new** | The biggest gap. See section 4. |
| **Tour mockup-sidebar** | `TourMockupSidebar` | **Net-new** | Mock of WUPHF's real sidebar (channels `#` + agents `@`), completion ticks. |
| Tour View-Transitions morph | `OfficeTour` transitions | Adapt | Feature-detect `startViewTransition`, gate behind `prefers-reduced-motion`, transform/opacity only. |
| Tour ends in real composer | Tour finish navigates to CEO DM / `#general` | Port | Deposit the user mid-action, do not dead-end on "Done". |
| **Seeded six-page getting-started wiki** | `team/getting-started/*.md` seeded at office seed | **Net-new content** | Authored fresh in WUPHF voice. See section 5. |
| 15 fake showcase windows (reference-product gallery) | none | Drop | Heavy, reference-specific. Tour mockup sidebar carries the "see it live" job. |
| House blueprint background | none | Drop | Cozy-home metaphor is off-brand for the office. |
| feedback-popup, data-dir-prompt, i18n/RTL, CLI verify, reference sidecar metadata | none | Drop | Not WUPHF concerns; English-only; already covered; reference-specific. |

## 3. Net-new WUPHF components

All under `web/src/components/onboarding/` unless noted. Reuse `ui/` primitives + design tokens.

| File | Description | Tokens / primitives |
|---|---|---|
| `tour/tourSlides.ts` | Slide id union + copy constants + finish CTA (copy in one reviewable place). | n/a |
| `tour/useOfficeTour.ts` | Auto-open once per browser via `localStorage: wuphf.office-tour-done`, `requestShowOfficeTour()` replay event, `markDone()`. Guards `typeof window`. | n/a |
| `tour/TourMockupSidebar.tsx` | Decorative mock of WUPHF's real sidebar: workspace label + `#channel`/`@agent` rows + green completion ticks. | `--preview-row-*`, `--green`/`--green-bg`, `--font-mono`, `PixelAvatar` |
| `tour/SlideIntro.tsx` | Slide 1: what an office is. Serif headline, mock sidebar materializing. | `--font-logo`, `--text-2xl`, `--text-secondary` |
| `tour/SlideAgents.tsx` | Slide 2: agent = persona + heartbeat + claims work. | `--accent`, `--accent-bg`, status dots, `PixelAvatar` |
| `tour/SlideIssues.tsx` | Slide 3: file an issue, team cuts tasks and ships. Typed-command animation. | `--font-mono`, `--accent`, `--green` |
| `tour/SlideWiki.tsx` | Slide 4: your context graph; seeded pages light up; agents read as first-class consumers. | `--bg-card`, `--border`, `--text` |
| `tour/OfficeTour.tsx` | Modal host: slide registry, back/next/finish footer, progress dots, Esc-to-skip, arrow-key nav, finish handoff. | `--z-modal`, `--shadow-overlay`, `Button`, `Kbd` |
| `GettingStartedChecklist.tsx` | Renders the dormant checklist (pick_team, second_key, github_repo, github_star, discord) as a dismissible panel. | `CollapsibleSection`, `Button`, `--green` ticks |
| `useGettingStartedChecklist.ts` | Query/mutation hook over the EXISTING `/onboarding/state` + `/onboarding/checklist/{id}/done` + `/onboarding/checklist/dismiss`. | TanStack Query |
| `web/src/styles/onboarding.css` (extend) | `.office-tour-*`, `.tour-mockup-*`, `.getting-started-checklist-*`. Keyframes transform/opacity only + reduced-motion guard. Delete dead legacy wizard CSS. | all `--*` tokens |
| Stories + tests | `OfficeTour`, `TourMockupSidebar`, `GettingStartedChecklist` under `Onboarding / *`; co-located `*.test.tsx`. | seed state in-story |

## 4. The guided tour, in WUPHF style

Four slides, auto-opened once per browser on first landing in the office (right after `complete`
flips `onboarded=true`).

**Placement: tour-modal, not inline-in-CEO-chat.** A modal overlay (`--z-modal`) is correct because
the CEO chat is the *setup* surface and the tour is the *orientation* surface; mixing them muddies
the "you have left setup and entered your office" boundary WUPHF deliberately draws
(`OnboardingChat` unmounts, the real `Shell` mounts). Auto-opens once (`localStorage`), Esc-skippable,
replayable from a Help entry via `requestShowOfficeTour()`. Slide motion uses `startViewTransition`
when available, synchronous fallback otherwise, disabled under `prefers-reduced-motion`.

**Mock sidebar** (`TourMockupSidebar`): a faithful mock of WUPHF's real left sidebar — workspace
label, `#general`/`#engineering` channel group, `@ceo`/`@analyst`/`@engineer` agent rows with
`PixelAvatar`s. Completion ticks light up per slide so the user watches their office fill in.
`aria-hidden` on slides.

**Slides** (copy finalized in section 6):

1. **Intro — "This is your office."** Serif headline, one-line subhead, mock sidebar materializing.
2. **Agents — "Your team, on the clock."** Hero agent card with three callouts: Persona, Heartbeat, Claims work.
3. **Issues — "File it. They ship it."** Typed `@engineer ...` command, fan-out grid of task cards with live heartbeat dots, `#general` destination pill.
4. **Wiki — "Write it once. The whole office knows."** Seeded wiki pages light up; agents read as first-class consumers. Finish CTA "Write your first issue" navigates to the composer.

**Finish handoff.** The finish button navigates to the CEO DM / `#general`, focuses the composer,
optionally prefills an issue intent, and marks `wuphf.office-tour-done`.

## 5. Seeded getting-started content

WUPHF already seeds `team/about/{README,company,owner}.md` at the office seed boundary
(`company_seed.go`). Add a parallel `team/getting-started/` tree, authored fresh in WUPHF voice,
seeded in the same atomic transaction, then regenerate the wiki index. Agents read these as
first-class consumers, so this doubles as agent priming.

| Reference topic | WUPHF page (`team/getting-started/*.md`) | Teaches |
|---|---|---|
| index.md | `index.md` — "How your office works" | The office metaphor: channels, agents, issues, wiki. |
| editor / WYSIWYG | folded into `the-wiki.md` | The wiki is the shared brain; markdown on disk; promotion from notebooks. |
| AI panel / delegating-between-agents | `working-with-agents.md` | Personas + heartbeats; DM or `@mention`; issues cut into tasks and handed off. |
| apps-and-repos | `connecting-your-work.md` | Connect a GitHub repo, bring real work in (maps to `github_repo` checklist item). |
| skills | `skills-and-runtimes.md` | What a skill is; how agents gain capabilities; runtime per agent. |
| rooms | `channels.md` | WUPHF channels and DMs as the coordination surface. |
| symlinks / load-knowledge | folded into `your-context-graph.md` | Wiki as a context graph agents query; temporal facts; reads count for humans and agents. |

Go side: extend `internal/operations/company_seed.go` with a `seedGettingStarted(...)` step
(mirror the existing about/ seed), gated identically for blueprint and scratch paths, embed a
`resources/getting-started/` markdown directory via `go:embed`. Front-matter only; no sidecar.

## 6. Copy rewrites (WUPHF voice)

| Intent | WUPHF copy |
|---|---|
| Tour dialog name | "A quick tour of your office" |
| Skip button | "Skip the tour" |
| Next button | "Next" |
| Finish button | "Write your first issue" |
| Slide 1 headline | "This is your office." |
| Slide 1 subhead | "A team of agents lives here. They claim work, they ship, and they actually answer your messages." |
| Slide 2 eyebrow | "MEET THE TEAM" |
| Slide 2 headline | "Your team, on the clock." |
| Slide 2 body | "Every agent has a role, a heartbeat that keeps it checking in, and a memory that does not reset. Unlike Ryan Howard, they actually ship." |
| Slide 3 eyebrow | "FILE AN ISSUE" |
| Slide 3 headline | "File it. They ship it." |
| Slide 3 body | "Mention an agent with @, hand off a problem, and the work fans out into tasks across the team while you watch." |
| Slide 4 eyebrow | "YOUR CONTEXT GRAPH" |
| Slide 4 headline | "Write it once. The whole office knows." |
| Slide 4 body | "Your wiki is the shared brain. Agents read it as first-class citizens, so context you capture once never has to be repeated." |
| Replay entry | "Replay the office tour" |
| Checklist heading | "Settle into your office" |
| Item pick_team | "Pick or trim your team" |
| Item second_key | "Add a second runtime so the office never stalls" |
| Item github_repo | "Connect a GitHub repo and bring real work in" |
| Item github_star | "Star WUPHF on GitHub (Michael would be proud, probably)" |
| Item discord | "Join the Discord and meet other founders" |
| Checklist dismiss | "I am settled in" |
| Tour transition fallback | "Setting up the next room of the tour." |

## 7. File-by-file build order (three independently shippable slices)

### Slice A — Guided tour (frontend-only, highest value)
1. `web/src/components/onboarding/tour/tourSlides.ts`
2. `web/src/components/onboarding/tour/useOfficeTour.ts`
3. `web/src/components/onboarding/tour/TourMockupSidebar.tsx`
4. `web/src/components/onboarding/tour/SlideIntro.tsx`
5. `web/src/components/onboarding/tour/SlideAgents.tsx`
6. `web/src/components/onboarding/tour/SlideIssues.tsx`
7. `web/src/components/onboarding/tour/SlideWiki.tsx`
8. `web/src/components/onboarding/tour/OfficeTour.tsx`
9. `web/src/styles/onboarding.css` — tour rules + keyframes; delete dead wizard CSS.
10. `web/src/routes/RootRoute.tsx` — mount `OfficeTour` in the office `Shell` branch; replay entry in Help.
11. Stories + tests.

### Slice B — Seeded getting-started knowledge (Go + content)
12. `resources/getting-started/*.md` — author the WUPHF pages from section 5.
13. `internal/operations/company_seed.go` — `seedGettingStarted(...)` on both paths + regenerate wiki index.
14. `internal/operations/company_seed_test.go` — both paths materialize the pages + index lists the section.

### Slice C — Getting-started checklist UI (frontend-only; backend already exists)
> Backend confirmed present: `/onboarding/checklist/{id}/done`, `/onboarding/checklist/dismiss`,
> state exposed at `checklist` / `checklist_dismissed`. No Go work required.
15. `web/src/components/onboarding/useGettingStartedChecklist.ts`
16. `web/src/components/onboarding/GettingStartedChecklist.tsx`
17. `web/src/styles/onboarding.css` — checklist rules.
18. `web/src/components/layout/Shell.tsx` — mount the panel for onboarded-but-not-dismissed users.
19. Stories + tests.

## 8. Verification

```bash
cd web && bunx tsc --noEmit
cd web && bunx biome check --write
bash scripts/test-web.sh web/src/components/onboarding/tour/OfficeTour.test.tsx
bash scripts/test-web.sh                       # full Vitest suite
cd web && bun run build                         # broker embeds web/dist; build BEFORE binary rebuild
go build -o wuphf ./cmd/wuphf
bash scripts/test-go.sh ./internal/operations
gofmt -l internal/ && go vet ./...
bunx secretlint "**/*"
```

### ICP tutorial scenarios (must pass end to end)

1. **Sam, solo founder, first run.** Picks Claude Code, talks to CEO, picks Engineering blueprint,
   trims to two agents, lands in `#general`. The office tour auto-opens once. Steps through four
   slides, mock sidebar fills with green ticks, finish CTA drops Sam into a composer prefilled with
   an issue intent. Wiki shows a populated Getting Started section. Checklist appears with five items.
   **Pass = tour auto-opens exactly once, finish lands in a focused composer, wiki and checklist non-empty.**
2. **Maya, returning operator, second browser.** Already onboarded; lands straight in `#general`,
   no wizard. Tour does NOT auto-open; she opens it from Help → "Replay the office tour", skips at
   slide 2 with Esc. **Pass = no forced tour for an onboarded user, replay works, Esc skips and persists done.**
3. **Priya, scratch-path minimalist.** Chooses "Start from scratch", minimal CEO + `#general` office.
   Tour still runs; Wiki slide shows seeded getting-started pages (scratch seeds them too). Checks off
   `github_repo` + `discord`, dismisses with "I am settled in", reloads, checklist stays dismissed.
   **Pass = scratch office has seeded content, checklist toggles persist, dismiss survives reload.**

All three must pass and every visual component must render in `nex`, `nex-dark`, and `noir-gold`.

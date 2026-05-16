# WUPHF tutorials

Each tutorial in this folder is a runnable scenario for one persona doing
one realistic job. Tutorials are written for the documented ICP (see
`docs/product-experience-test-2026-04-16-paperclip-cabinet-icp.md`) and
serve as both onboarding docs and the integration spec the product must
pass end-to-end.

The format is opinionated:

- **One persona, one job, one outcome.** No generic instructions.
- **Concrete inputs and outputs.** Word-for-word channel messages, file
  names, PR titles.
- **Verifiable.** A reader (or QA harness) should be able to follow it
  on a clean wuphf install and observe the same surface state.

Tutorials cover five scenarios from the demo script:

| # | Scenario | Files |
|---|---|---|
| 1 | Install + first look | `01a-alex-first-install.md`, `01b-jordan-first-install.md` |
| 2 | Drop a goal — agents coordinate | `02a-sam-onboarding-goal.md`, `02b-riley-build-flag.md` |
| 3 | Autonomous work surfaces a blocker | `03a-alex-svg-blocker.md`, `03b-morgan-asset-pipeline.md` |
| 4 | Configure agents + packs | `04a-sam-fork-and-swap.md`, `04b-morgan-custom-pack.md` |
| 5 | Day 2 — agents remember context | `05a-alex-postmortem.md`, `05b-jordan-day-two-recall.md` |
| share | Invite a team member | `share-with-team-member.md` |

The five named personas are:

- **Alex Chen** — solo dev, ex-Stripe (Paperclip ICP).
- **Jordan Park** — indie hacker, 3 products (Paperclip ICP).
- **Sam Rivera** — CTO at an 8-person startup (both ICPs).
- **Riley Walsh** — product engineer at a 30-person startup (Cabinet ICP).
- **Morgan Lee** — agency founder, 6 people (Cabinet ICP).

Each tutorial follows the same three-section structure:

1. **Who and why.** Persona + the one outcome they came for.
2. **Steps.** Numbered, each with a `## Verify` block.
3. **What success looks like.** A single sentence the reader and a QA
   harness can both score.

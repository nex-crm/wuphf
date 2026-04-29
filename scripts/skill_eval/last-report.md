# Skill-pipeline evaluation report

- **Stage A correctness:** 100.0% (35/35)
- **Promoted-skill quality:** 98.3% mean over 17 files
- **Visibility (403 + delegate_to):** 100% (3/3)
- **Catalog bytes max:** 1050 (soft warn ≥6144, hard fail ≥8192)
- **Enhance-existing diversions:** 1 (expected 1)
- **Proposal volume:** 20 created, 15 interview requests, 0 over-promotions, 0 duplicates, 0 orphan requests

## Confusion matrix

```
expected \\ actual    promoted  skipped  guard_rejected  error
  promoted                  17         0         0         0
  skipped                    0        13         0         0
  guard_rejected             0         0         5         0
```

## Per-scenario results

| id | category | expected | actual | sim_outcome | pass | rationale |
|---|---|---|---|---|---|---|
| `prom-01-onboard` | PROMOTE | promoted | promoted | create_new | PASS | Explicit Anthropic frontmatter + numbered steps in body — fast-path promote |
| `prom-02-deploy` | PROMOTE | promoted | promoted | create_new | PASS | Anthropic frontmatter + ## How to body, deployment context |
| `prom-03-triage` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Workflow with multi-stage incident playbook |
| `prom-04-renewal` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Procedure header for CSM renewal motion |
| `prom-05-backup` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Steps, ops/SRE workflow |
| `prom-06-dns` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Runbook header |
| `prom-07-cost` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Process header with finance workflow |
| `prom-08-invoice` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Recipe header with imperative steps |
| `prom-09-linear` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Instructions header |
| `prom-10-tests` | PROMOTE | promoted | promoted | create_new | PASS | Frontmatter + ## Steps, engineering workflow |
| `skip-01-bio` | SKIP | skipped | skipped | create_new | PASS | Person bio — narrative, no skill shape |
| `skip-02-decision-log` | SKIP | skipped | skipped | create_new | PASS | Decision log — historical record, no actionable steps |
| `skip-03-narrative` | SKIP | skipped | skipped | create_new | PASS | Customer success story — marketing narrative, not a runbook |
| `skip-04-glossary` | SKIP | skipped | skipped | create_new | PASS | Glossary — definitions, no procedure |
| `skip-05-vision` | SKIP | skipped | skipped | create_new | PASS | Vision/strategy doc — aspirational, no concrete steps |
| `skip-06-faq` | SKIP | skipped | skipped | create_new | PASS | FAQ - Q&A format, not skill steps |
| `skip-07-arch` | SKIP | skipped | skipped | create_new | PASS | Architecture overview - description, not procedure |
| `skip-08-notes` | SKIP | skipped | skipped | create_new | PASS | Random notes - no shape, no steps |
| `skip-09-reference` | SKIP | skipped | skipped | create_new | PASS | Reference table - data only, no procedure |
| `skip-10-marketing` | SKIP | skipped | skipped | create_new | PASS | Marketing copy with the word 'Steps' but no real procedure |
| `guard-01-bash-eval` | GUARD_REJECT | guard_rejected | guard_rejected | create_new | PASS | Bash eval $(curl) - dangerous remote code exec; KNOWN GAP D6 |
| `guard-02-curl-sh` | GUARD_REJECT | guard_rejected | guard_rejected | create_new | PASS | curl / sh - classic remote code exec |
| `guard-03-rmrf` | GUARD_REJECT | guard_rejected | guard_rejected | create_new | PASS | rm -rf /var/log - destructive absolute path |
| `guard-04-wget-bash` | GUARD_REJECT | guard_rejected | guard_rejected | create_new | PASS | wget --no-check-certificate piped to bash - subtle but dangerous |
| `guard-05-multi` | GUARD_REJECT | guard_rejected | guard_rejected | create_new | PASS | Multiple dangerous patterns combined |
| `trap-01-bio-with-frontmatter` | FAST_PATH_TRAP | skipped | skipped | create_new | PASS | Author put name+description on a person bio. Fast-path will promote it without LLM judgment unless body-shape is checked. Surfaces the over-eager promotion failure mode. |
| `trap-02-decision-with-frontmatter` | FAST_PATH_TRAP | skipped | skipped | create_new | PASS | Decision log accidentally got name+description added during a wiki migration. Body has no skill shape; promoting it is a false positive. |
| `owner-01-executor-frontmatter` | PROMOTE | promoted | promoted | create_new | PASS | Explicit frontmatter owner_agents=[executor] round-trips through writeSkillProposalLocked → SKILL.md → b.skills |
| `owner-02-planner-frontmatter` | PROMOTE | promoted | promoted | create_new | PASS | Explicit frontmatter owner_agents=[planner] persists through proposal write + reconcile |
| `owner-03-multi-owner` | PROMOTE | promoted | promoted | create_new | PASS | Multi-owner skill: owner_agents=[reviewer, planner] both retain visibility, neither becomes lead-routable |
| `owner-04-unknown-owner-stripped` | PROMOTE | promoted | promoted | create_new | PASS | Unknown slug in owner_agents is stripped by validateOwnerAgentsLocked; remaining valid owner survives. owner_agents=[fakebot, reviewer] → [reviewer] |
| `owner-05-shared-playbook` | PROMOTE | promoted | promoted | create_new | PASS | team/playbooks/ default seed → no path inference → owner_agents stays empty (lead-routable shared skill) |
| `sim-01-canonical-invoice` | PROMOTE | promoted | promoted | create_new | PASS | Canonical invoice-reminder skill seeded so sim-01-near-duplicate can collide with it on a second compile pass |
| `sim-04-distinct-deploy` | PROMOTE | promoted | promoted | create_new | PASS | Distinct-skills sanity check: orthogonal deploy skill should NOT trip the similarity gate against any existing scenario |
| `sim-02-near-duplicate` | PROMOTE | skipped | skipped | enhance_existing | PASS | PR 7 task #13: seeded AFTER chase-overdue-invoice landed. High content overlap → similarity gate diverts to enhance_existing, EnhancementCandidatesTotal increments, skill is NOT promoted. |

## Visibility checks (cross-role invoke)

| id | invoker | target | want | got | pass | detail |
|---|---|---|---|---|---|---|
| `vis-01-cross-role-403` | `planner` | `deploy-canary-promote` | 403 | 403 | PASS |  |
| `vis-02-lead-bypass-on-shared-200` | `ceo` | `company-onboarding-day-one` | 200 | 200 | PASS |  |
| `vis-03-lead-only-shared-403` | `planner` | `company-onboarding-day-one` | 403 | 403 | PASS |  |

## Promoted-skill quality

| check | pass rate |
|---|---|
| `frontmatter_parses` | 17/17 (100%) |
| `has_name` | 17/17 (100%) |
| `has_description` | 17/17 (100%) |
| `name_kebab` | 17/17 (100%) |
| `desc_len_in_range` | 17/17 (100%) |
| `has_metadata_wuphf` | 17/17 (100%) |
| `has_trigger` | 17/17 (100%) |
| `has_created_by` | 17/17 (100%) |
| `has_safety_scan` | 12/17 (71%) |
| `has_source_articles` | 17/17 (100%) |
| `owner_agents_matches_expected` | 17/17 (100%) |
| `similar_to_existing_clean_when_new` | 17/17 (100%) |
| `body_has_skill_header` | 17/17 (100%) |
| `body_has_list_or_steps` | 17/17 (100%) |
| `body_min_length` | 17/17 (100%) |
| `no_placeholder_text` | 17/17 (100%) |
| `description_grounded_in_body` | 17/17 (100%) |

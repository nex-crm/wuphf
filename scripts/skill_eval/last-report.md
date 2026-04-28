# Skill-pipeline evaluation report

- **Stage A correctness:** 92.6% (25/27)
- **Promoted-skill quality:** 96.7% mean over 12 files
- **Proposal volume:** 15 created, 12 interview requests, 2 over-promotions, 0 duplicates, 0 orphan requests

## Confusion matrix

```
expected \\ actual    promoted  skipped  guard_rejected  error
  promoted                  10         0         0         0
  skipped                    2        10         0         0
  guard_rejected             0         0         5         0
```

## Per-scenario results

| id | category | expected | actual | pass | rationale |
|---|---|---|---|---|---|
| `prom-01-onboard` | PROMOTE | promoted | promoted | PASS | Explicit Anthropic frontmatter + numbered steps in body — fast-path promote |
| `prom-02-deploy` | PROMOTE | promoted | promoted | PASS | Anthropic frontmatter + ## How to body, deployment context |
| `prom-03-triage` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Workflow with multi-stage incident playbook |
| `prom-04-renewal` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Procedure header for CSM renewal motion |
| `prom-05-backup` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Steps, ops/SRE workflow |
| `prom-06-dns` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Runbook header |
| `prom-07-cost` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Process header with finance workflow |
| `prom-08-invoice` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Recipe header with imperative steps |
| `prom-09-linear` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Instructions header |
| `prom-10-tests` | PROMOTE | promoted | promoted | PASS | Frontmatter + ## Steps, engineering workflow |
| `skip-01-bio` | SKIP | skipped | skipped | PASS | Person bio — narrative, no skill shape |
| `skip-02-decision-log` | SKIP | skipped | skipped | PASS | Decision log — historical record, no actionable steps |
| `skip-03-narrative` | SKIP | skipped | skipped | PASS | Customer success story — marketing narrative, not a runbook |
| `skip-04-glossary` | SKIP | skipped | skipped | PASS | Glossary — definitions, no procedure |
| `skip-05-vision` | SKIP | skipped | skipped | PASS | Vision/strategy doc — aspirational, no concrete steps |
| `skip-06-faq` | SKIP | skipped | skipped | PASS | FAQ - Q&A format, not skill steps |
| `skip-07-arch` | SKIP | skipped | skipped | PASS | Architecture overview - description, not procedure |
| `skip-08-notes` | SKIP | skipped | skipped | PASS | Random notes - no shape, no steps |
| `skip-09-reference` | SKIP | skipped | skipped | PASS | Reference table - data only, no procedure |
| `skip-10-marketing` | SKIP | skipped | skipped | PASS | Marketing copy with the word 'Steps' but no real procedure |
| `guard-01-bash-eval` | GUARD_REJECT | guard_rejected | guard_rejected | PASS | Bash eval $(curl) - dangerous remote code exec; KNOWN GAP D6 |
| `guard-02-curl-sh` | GUARD_REJECT | guard_rejected | guard_rejected | PASS | curl / sh - classic remote code exec |
| `guard-03-rmrf` | GUARD_REJECT | guard_rejected | guard_rejected | PASS | rm -rf /var/log - destructive absolute path |
| `guard-04-wget-bash` | GUARD_REJECT | guard_rejected | guard_rejected | PASS | wget --no-check-certificate piped to bash - subtle but dangerous |
| `guard-05-multi` | GUARD_REJECT | guard_rejected | guard_rejected | PASS | Multiple dangerous patterns combined |
| `trap-01-bio-with-frontmatter` | FAST_PATH_TRAP | skipped | promoted | FAIL | Author put name+description on a person bio. Fast-path will promote it without LLM judgment unless body-shape is checked. Surfaces the over-eager promotion failure mode. |
| `trap-02-decision-with-frontmatter` | FAST_PATH_TRAP | skipped | promoted | FAIL | Decision log accidentally got name+description added during a wiki migration. Body has no skill shape; promoting it is a false positive. |

## Promoted-skill quality

| check | pass rate |
|---|---|
| `frontmatter_parses` | 12/12 (100%) |
| `has_name` | 12/12 (100%) |
| `has_description` | 12/12 (100%) |
| `name_kebab` | 12/12 (100%) |
| `desc_len_in_range` | 12/12 (100%) |
| `has_metadata_wuphf` | 12/12 (100%) |
| `has_trigger` | 10/12 (83%) |
| `has_created_by` | 12/12 (100%) |
| `has_safety_scan` | 12/12 (100%) |
| `has_source_articles` | 12/12 (100%) |
| `body_has_skill_header` | 10/12 (83%) |
| `body_has_list_or_steps` | 10/12 (83%) |
| `body_min_length` | 12/12 (100%) |
| `no_placeholder_text` | 12/12 (100%) |
| `description_grounded_in_body` | 12/12 (100%) |

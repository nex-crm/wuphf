# Skill-pipeline evaluation report

- **Stage A correctness:** 100.0% (25/25)
- **Promoted-skill quality:** 100.0% mean over 10 files

## Confusion matrix

```
expected \\ actual    promoted  skipped  guard_rejected  error
  promoted                  10         0         0         0
  skipped                    0        10         0         0
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

## Promoted-skill quality

| check | pass rate |
|---|---|
| `frontmatter_parses` | 10/10 (100%) |
| `has_name` | 10/10 (100%) |
| `has_description` | 10/10 (100%) |
| `name_kebab` | 10/10 (100%) |
| `desc_len_in_range` | 10/10 (100%) |
| `has_metadata_wuphf` | 10/10 (100%) |
| `has_trigger` | 10/10 (100%) |
| `has_created_by` | 10/10 (100%) |
| `has_safety_scan` | 10/10 (100%) |
| `has_source_articles` | 10/10 (100%) |
| `body_has_skill_header` | 10/10 (100%) |
| `body_has_list_or_steps` | 10/10 (100%) |
| `body_min_length` | 10/10 (100%) |
| `no_placeholder_text` | 10/10 (100%) |
| `description_grounded_in_body` | 10/10 (100%) |

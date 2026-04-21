# How the WUPHF wiki works

WUPHF's wiki is a git repository where every write is a commit, every agent has a real git identity, and an LLM runs inside the broker to synthesize entity briefs from an append-only fact log. No cloud dependency, no SDK lock-in, no proprietary storage format.

## The pipeline in 50 lines of concept

```
agent records a fact
  └─► POST /entity/fact
        └─► FactLog.Append()
              └─► WikiWorker.EnqueueEntityFact()
                    └─► git commit (author: agent-slug@wuphf.local)
                          └─► fact #5 → threshold crossed
                                └─► EntitySynthesizer.EnqueueSynthesis()
                                      └─► LLM shell-out (claude / codex / openclaw)
                                            └─► WikiWorker.Enqueue()
                                                  └─► git commit (author: archivist)
```

## Key files

| File | Role |
|------|------|
| `internal/team/entity_facts.go` | Append-only fact log — one JSONL line per fact, one git commit per append |
| `internal/team/entity_synthesizer.go` | Broker-level LLM synthesis worker — threshold trigger, coalescing, LLM shell-out |
| `internal/team/broker_entity.go` | HTTP handlers for `/entity/fact`, `/entity/brief/synthesize`, `/entity/briefs` |
| `internal/teammcp/entity_tools.go` | MCP tool definitions: `entity_fact_record`, `entity_brief_synthesize` |
| `internal/team/entity_commit.go` | WikiWorker — single-writer git queue, all commits go through here |

## Storage layout (inside `~/.wuphf/wiki/`)

```
team/
├── companies/
│   └── anthropic.md            ← synthesized brief (YAML frontmatter tracks synthesis state)
├── people/
│   └── ada-lovelace.md
├── customers/
│   └── meridian-freight.md
└── entities/
    ├── companies-anthropic.facts.jsonl   ← append-only fact log
    └── people-ada-lovelace.facts.jsonl
```

## Git identity

Every commit in the wiki repo has a real author:

- **Agent commits** (fact appends, manual wiki writes): `slug@wuphf.local`
- **Archivist commits** (LLM synthesis): `archivist <archivist@wuphf.local>`

```
git log --oneline wiki/
  a3f1c2d  archivist: update companies/anthropic brief (5 facts)
  9b2e4a1  fact: companies/anthropic — Founded in 2021 by Dario Amodei...
  7c8d3f0  fact: companies/anthropic — Headquarters are in San Francisco...
  4e5a2b9  fact: companies/anthropic — Creator of the Claude family...
  2d1c6e8  fact: companies/anthropic — Raised over $7 billion in funding...
  f0a9b3c  fact: companies/anthropic — Research focus includes Constitutional AI...
```

## Synthesis threshold

Synthesis fires automatically when the number of new facts since the last synthesis crosses `WUPHF_ENTITY_BRIEF_THRESHOLD` (default: 5). The synthesizer:

1. Reads the existing brief and parses the `fact_count_at_synthesis` frontmatter field
2. Lists all facts from the JSONL log (newest-first)
3. Computes `new_count = total - fact_count_at_synthesis`
4. When `new_count >= threshold`, calls `EnqueueSynthesis()`

The synthesis job runs inside the broker as a goroutine — no agent turn consumed. Requests for the same entity coalesce: if synthesis is already in-flight, exactly one follow-up is queued.

## LLM call

The synthesizer shells out to the user's configured LLM CLI via `provider.RunConfiguredOneShot`. WUPHF never carries an LLM SDK — you bring your own auth (Claude CLI, Codex, OpenClaw, etc.).

System prompt (locked, do not change without updating the spec):

> You maintain entity briefs in a team wiki. Given an existing brief and new facts, produce an updated markdown brief that incorporates the facts. Never invent facts. Preserve the canonical structure. Mark contradictions with **Contradiction:** callouts. Do not write a "## Related" section — that block is managed automatically. Output ONLY the updated markdown.

The `## Related` section is rebuilt deterministically from the cross-entity graph after every synthesis — the LLM never controls it.

## Running the demo

```bash
# start dev broker (separate terminal)
wuphf-dev --broker-port 7899 --web-port 7900 --memory-backend markdown

# run the pipeline demo
./scripts/demo-entity-synthesis.sh

# or with a custom entity
ENTITY_KIND=people ENTITY_SLUG=ada-lovelace ./scripts/demo-entity-synthesis.sh

# fire synthesis after every single fact (set threshold to 1 in broker env)
WUPHF_ENTITY_BRIEF_THRESHOLD=1 wuphf-dev --broker-port 7899 --web-port 7900 --memory-backend markdown
```

## What Karpathy's LLM-Wiki idea describes

Karpathy's vision: a living knowledge base where LLMs are first-class editors, not just search helpers. Facts accumulate from agents and humans; the LLM synthesizes them into canonical articles; the system self-corrects over time.

WUPHF implements this exactly:

| Karpathy concept | WUPHF implementation |
|---|---|
| LLMs as editors | `EntitySynthesizer`: LLM rewrites the brief on each synthesis cycle |
| Append-only fact log | `.facts.jsonl` — wrong facts get counter-facts, not deletions |
| Git as knowledge graph | Per-agent git identity, full commit history, wikilinks at read time |
| No SDK lock-in | Shells out to user's own LLM CLI |
| Self-improving articles | `## What we've learned` added by playbook synthesizer after 3 executions |

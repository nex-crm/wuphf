# Slice 1 Week 0 benchmark — recall@20 ship gate

**Verdict: FAIL (ship gate RED).** BM25-only retrieval clears status and
general queries but collapses on multi-hop ("Who at X championed Y?") and on
large-recall relationship queries. Micro-averaged recall lands at 90.5%;
per-query pass-rate (the gate as written in the README) lands at 66%.

## Command

```bash
# From the repo root — deterministic, 3 retrieval iterations per query.
go run ./cmd/bench-slice-1
```

Full report (including every per-query row and every failing query) is
reproduced verbatim at the end of this file so diff against future runs tells
the whole story.

## Aggregate

| Metric | Value |
|---|---|
| Artifacts indexed | 500 |
| Facts indexed | 475 |
| Queries | 50 |
| Queries passing 85% per-query recall | **33** |
| **Pass rate (gate metric)** | **66.00%** |
| Micro-recall (Σ hits / Σ expected) | 90.49% |
| Retrieval p50 | 0.04 ms |
| Retrieval p95 | 0.14 ms |
| Classify p95 | 8 µs |
| Reconcile wall time | ~15 s (500 artifacts / 475 facts) |
| SQLite size | 286 720 bytes (≈280 KB) |
| Bleve size | 2 661 097 – 7 640 713 bytes (varies with segment state) |
| **Gate (pass rate ≥ 85%)** | **RED** |

Recall numbers are deterministic across re-runs (seed 42 in the generator, no
randomness in retrieval). Latency varies ±30% run-to-run; reported figures are
the median of three iterations per query.

## Per-class breakdown

| Class | Total | Passing | Pass-rate | Micro-recall |
|---|---:|---:|---:|---:|
| status | 20 | 20 | 100.0% | 99.4% |
| relationship | 15 | 9 | 60.0% | 88.5% |
| multi_hop | 10 | 0 | 0.0% | 50.0% |
| counterfactual | 3 | 2 | 66.7% | 66.7% |
| general (OOS) | 2 | 2 | 100.0% | 100.0% |

The gate breaks entirely on **multi_hop** (0/10) and partially on
**relationship** (6/15 failures). Status and general are both perfect.

## Top failing queries

### q_040 — multi_hop, recall 25.0%
> "Who at Northwind championed the Onboarding V3 project?"

- expected: `[0de1e607409ee323 334f29d80f1bccc2 6ac9580c07881d9c a2f62ce0f3404d94]`
- got (top 20): contains only `6ac9580c07881d9c` — the other three expected
  facts never enter the top-20.

Diagnosis: BM25 over-weights facts that mention "Onboarding V3" verbatim.
The expected set joins (champions project=Onboarding V3) ∩ (role_at=Northwind),
which requires pulling each Northwind champion's latest `role_at` fact in too.
Those `role_at` facts do not contain "Onboarding" and drop below rank 20.

### q_047 — counterfactual, recall 0.0%
> "What would have happened if Ivan Petrov had not taken her current role?"

- expected: `[723c58606ebf424e]`
- got (top 20): 20 facts returned; `723c58606ebf424e` is NOT in them.

Diagnosis: BM25 ranks facts containing the counterfactual trigger words
("role", "taken", etc.) ahead of the actual most-recent role_at fact for
`ivan-petrov`. The classifier routes this to `counterfactual` (confidence
0.85), but the index Search step uses the raw question verbatim.
Adding a person-slug-aware rewrite ("role_at Ivan Petrov") would almost
certainly surface `723c58606ebf424e`.

### q_036 — multi_hop, recall 50.0%
> "Who at Blueshift championed the Q2 Pilot Program project?"

- expected: `[39b5539e690a71b6 bb0bb09459bd2e36]`
- got: includes `bb0bb09459bd2e36` (the champions fact) but misses
  `39b5539e690a71b6` (the champion's current `role_at=blueshift` fact,
  which doesn't contain "Q2 Pilot").

This is the canonical shape of every multi_hop miss: BM25 returns one side
of the join, never both.

## Why the gate is RED

Three compounding failure modes, all structural:

1. **Multi-hop intersection is outside BM25's reach.** "Who at X championed Y"
   needs (champions ∩ role_at) — a typed join the text index doesn't model.
   10/10 multi_hop queries fail because the role_at side never reaches top-20.

2. **Large expected sets saturate top-20.** Several relationship queries have
   25–27 expected fact IDs (e.g. q_035: 27 expected). Even perfect retrieval
   caps recall at 20/27 ≈ 74%. The generator created these expected sets by
   `expectedFactsForProjectAnyPredicate` without a topK-aware cap, which
   makes them unsatisfiable at topK=20. This is arguably a corpus-labeling
   issue, but per the task's hard constraint I did not alter it.

3. **Counterfactual trigger words dominate BM25 scoring.** q_047 returns zero
   of the one expected fact because the question verbatim out-ranks the
   target role_at fact.

## Proposed fix (not shipping)

The gate as-written is not clearable with BM25-only retrieval — which is
exactly the signal Slice 1 exists to produce. Three concrete next steps,
ranked by ROI:

1. **Add a typed-predicate boost.** Parse the query for "X at Y" constructs
   (the classifier already tags these as multi_hop) and expand retrieval:
   run BM25, then union with `ListFactsForEntity(subject_entity)` filtered
   by predicate. This is the hybrid retrieval the design note calls out.
2. **Query rewrite for counterfactual.** When `ClassifyQuery` returns
   `counterfactual`, strip the "what if / had not" frame and re-run the
   BM25 search on the remaining noun phrase.
3. **Recall-capped expected sets in the generator.** For queries where
   `|expected| > topK`, either accept Recall@K < 1 in the threshold
   (set `expected_min_recall_at_20` to `topK / |expected|`) or sample
   down to topK. Either preserves the ship gate's meaning without lying.

Fixes 1 and 2 lift multi_hop and counterfactual without touching the corpus.
Fix 3 is a corpus-labeling correction that only kicks in for queries whose
expected set is structurally unsatisfiable at topK.

## Honest interpretation

- **Micro-recall at 90.5%** tells us BM25 surfaces most of the ground truth
  most of the time; retrieval as a whole is not broken.
- **Per-query pass-rate at 66%** tells us the specific ship-gate metric —
  "every query recalls ≥ 85% of its expected set" — is structurally out of
  reach with pure BM25.
- The right next move is the hybrid/typed retrieval in Weeks 1–4, not a
  corpus patch that hides the signal.

## Corpus footprint

- `bench/slice-1/corpus.jsonl` — 500 artifacts, 475 facts, 257 538 bytes
- `bench/slice-1/queries.jsonl` — 50 queries
- Temp wiki layout (created per run, cleaned on exit):
  - `{tmp}/wiki/facts/person/{slug}.jsonl` (one file per of 25 people)
  - `{tmp}/.index/wiki.sqlite` — 280 KB after reconcile
  - `{tmp}/.index/bleve/` — 2.5–7.5 MB after reconcile

## Full report (run 1)

```
Slice 1 Week 0 benchmark — recall@20 ship gate
================================================

Gate: PassRate >= 85% — verdict: FAIL

Aggregate
---------
  Queries                  : 50
  Queries passing gate     : 33
  Pass rate                : 66.00%
  Micro-recall (∑hit/∑exp) : 90.49%
  Retrieval p50            : 0.04 ms
  Retrieval p95            : 0.14 ms
  Classify p95             : 8 µs
  Artifacts indexed        : 500
  Facts indexed            : 475
  Reconcile wall time      : 14562 ms
  SQLite size              : 286720 bytes
  Bleve size               : 2661097 bytes

Per-class breakdown
-------------------
  counterfactual   total= 3 passing= 2 pass_rate= 66.7%  micro_recall= 66.7%
  general          total= 2 passing= 2 pass_rate=100.0%  micro_recall=100.0%
  multi_hop        total=10 passing= 0 pass_rate=  0.0%  micro_recall= 50.0%
  relationship     total=15 passing= 9 pass_rate= 60.0%  micro_recall= 88.5%
  status           total=20 passing=20 pass_rate=100.0%  micro_recall= 99.4%

Per-query results
-----------------
  id      class            recall   target  p50ms   query
  q_001   status           100.0%   85.0%    0.03  What does Marcus Lee do?
  q_002   status           100.0%   85.0%    0.03  Who is Noah Brant?
  q_003   status           100.0%   85.0%    0.03  Where does Esme Walker work?
  q_004   status           100.0%   85.0%    0.04  What is Oscar Delmar's current role?
  q_005   status           100.0%   85.0%    0.02  What does Bruno Salas do?
  q_006   status           100.0%   85.0%    0.03  Who is Sarah Jones?
  q_007   status           100.0%   85.0%    0.04  Where does Maya Grant work?
  q_008   status           100.0%   85.0%    0.04  What is Elena Koch's current role?
  q_009   status           100.0%   85.0%    0.03  What does Rafael Cho do?
  q_010   status            91.7%   85.0%    0.03  Who is Claudia Vega?
  q_011   status           100.0%   85.0%    0.02  Where does Diego Park work?
  q_012   status           100.0%   85.0%    0.04  What is Theo Nakamura's current role?
  q_013   status           100.0%   85.0%    0.23  What does Nora Finch do?
  q_014   status           100.0%   85.0%    0.09  Who is Amina Reyes?
  q_015   status           100.0%   85.0%    0.07  Where does Kiran Joshi work?
  q_016   status           100.0%   85.0%    0.08  What is Rami Sato's current role?
  q_017   status           100.0%   85.0%    0.06  What does Ivan Petrov do?
  q_018   status           100.0%   85.0%    0.08  Who is Viktor Ng?
  q_019   status           100.0%   85.0%    0.06  Where does Harper Quinn work?
  q_020   status           100.0%   85.0%    0.07  What is Jonah Pike's current role?
  q_021   relationship     100.0%   85.0%    0.10  Who leads Q2 Pilot Program?
  q_022   relationship      88.9%   85.0%    0.08  Who champions Security Audit?
  q_023   relationship     100.0%   85.0%    0.04  Who is involved in Data Platform Migration?
  q_024   relationship     100.0%   85.0%    0.04  Who leads Pricing Rework?
  q_025   relationship      91.7%   85.0%    0.04  Who champions Onboarding V3?
  q_026   relationship      80.0%   85.0%    0.03  Who is involved in Mobile Revamp?
  q_027   relationship     100.0%   85.0%    0.04  Who leads Partner Program?
  q_028   relationship      70.0%   85.0%    0.03  Who champions APAC Launch?
  q_029   relationship     100.0%   85.0%    0.04  Who is involved in Q2 Pilot Program?
  q_030   relationship      75.0%   85.0%    0.04  Who leads Security Audit?
  q_031   relationship     100.0%   85.0%    0.04  Who champions Data Platform Migration?
  q_032   relationship     100.0%   85.0%    0.03  Who is involved in Pricing Rework?
  q_033   relationship      80.0%   85.0%    0.03  Who leads Onboarding V3?
  q_034   relationship      83.3%   85.0%    0.03  Who champions Mobile Revamp?
  q_035   relationship      74.1%   85.0%    0.04  Who is involved in Partner Program?
  q_036   multi_hop         50.0%   85.0%    0.05  Who at Blueshift championed the Q2 Pilot Program project?
  q_037   multi_hop         50.0%   85.0%    0.05  Who at Vandelay Industries championed the Data Platform Migration proje…
  q_038   multi_hop         66.7%   85.0%    0.05  Who at Acme Corp championed the Data Platform Migration project?
  q_039   multi_hop         60.0%   85.0%    0.05  Who at Dunder Mifflin championed the Mobile Revamp project?
  q_040   multi_hop         25.0%   85.0%    0.04  Who at Northwind championed the Onboarding V3 project?
  q_041   multi_hop         50.0%   85.0%    0.04  Who at Blueshift championed the Security Audit project?
  q_042   multi_hop         50.0%   85.0%    0.05  Who at Vandelay Industries championed the Pricing Rework project?
  q_043   multi_hop         50.0%   85.0%    0.04  Who at Acme Corp championed the Mobile Revamp project?
  q_044   multi_hop         50.0%   85.0%    0.14  Who at Dunder Mifflin championed the Data Platform Migration project?
  q_045   multi_hop         50.0%   85.0%    0.09  Who at Northwind championed the Data Platform Migration project?
  q_046   counterfactual   100.0%   85.0%    0.07  What would have happened if Oscar Delmar had not taken her current role?
  q_047   counterfactual     0.0%   85.0%    0.08  What would have happened if Ivan Petrov had not taken her current role?
  q_048   counterfactual   100.0%   85.0%    0.08  What would have happened if Harper Quinn had not taken her current role?
  q_049   general          100.0%  100.0%    0.02  What is the weather on Mars today?
  q_050   general          100.0%  100.0%    0.01  Explain the plot of a novel that has never been written.

SHIP GATE RED — pass rate 66.00% < gate 85%
```

## Rerun

```bash
# Regenerate corpus (deterministic):
go run ./bench/slice-1

# Execute the benchmark (fails the binary with exit 1 when gate is RED):
go run ./cmd/bench-slice-1

# Write the full textual report to a file while still printing to stdout:
go run ./cmd/bench-slice-1 --out bench/slice-1/last-run.txt
```

# ICP tutorial harness

`scripts/icp-tutorial-harness.sh` runs the 10 ICP scenarios in
`docs/tutorials/` against a freshly booted wuphf instance and produces
a pass / fail / skipped report in `/tmp/icp-tutorial-results-<ts>/`.

## What it does

- Builds wuphf from the current working tree (or uses `--binary=<path>`).
- Boots wuphf on an isolated runtime home (`/tmp/icp-tutorial-results-<ts>/runtime`).
- Drives each scenario through the broker REST API.
- Scores each scenario against the "What success looks like" line in
  the tutorial.
- Writes `results.json` + `RESULTS.md` to the report directory.
- Tears down the wuphf process and (by default) wipes the runtime home.

## What it does not do

The harness drives the API surface, not the browser DOM. Two classes
of tutorial step are out of scope:

1. **LLM-driven coordination** (02a, 02b, 03a, 03b): the harness can
   post the seed message and watch the inbox shape change, but it
   does not score the LLM's reply quality. The default 180s timeout
   for inbox population is heuristic.
2. **Long-running or CLI-only flows** (01b pack, 03a 20min runtime,
   04a/04b config edits, 05a/05b 24h history): reported as
   `skipped` with a one-line reason.

Browser-DOM-level coverage belongs in `web/e2e/`. The harness covers
the layer below the UI.

## Usage

```bash
# Default: build, boot, run all scenarios, report.
bash scripts/icp-tutorial-harness.sh

# Skip LLM-dependent scenarios (CI-friendly).
bash scripts/icp-tutorial-harness.sh --no-llm

# Run only specific scenarios.
bash scripts/icp-tutorial-harness.sh --scenarios=01a,02a,04a

# Use an existing binary instead of building.
bash scripts/icp-tutorial-harness.sh --binary=/usr/local/bin/wuphf

# Keep the runtime home for inspection after the run.
bash scripts/icp-tutorial-harness.sh --keep-runtime
```

Exit code is the number of failed scenarios. A `skipped` result is
not a failure.

## Report shape

`results.json` is a JSON array of `{scenario, status, notes, at}`.
`RESULTS.md` is a human-readable Markdown table plus a failure
count. Both live in the report directory printed at the end of the
run.

## Wire it into CI

CI can run the `--no-llm` slice on every PR. The LLM-driven slice
should run on a manual workflow trigger to keep token spend
predictable.

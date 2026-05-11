# Scripts

## `dispatch-verification-agent.sh`

Runs a single Codex agent as a read-only adversarial sounding board for a
proposed solution. Use it before committing a fix that needs stress-testing,
especially around security boundaries, validators, wire shapes, freeze
boundaries, or irreversible changes.

```bash
bash scripts/dispatch-verification-agent.sh \
  --solution-file /path/to/proposed-solution.md \
  --target-files "packages/protocol/src/audit-event.ts,packages/protocol/src/canonical-json.ts" \
  --output /tmp/verification-output.md \
  --lens security
```

Defaults:

- `--sandbox read-only`
- `--profile auto`

The script tees the final verification report to stdout and writes the same
content to `--output`. Review findings do not change the exit code; a non-zero
exit means the wrapper or Codex execution failed.

## `dispatch-triangulation.sh`

Runs several read-only Codex agents in parallel, each with a different review
lens, then writes per-lens reports and a deterministic synthesis grouped by
matching `file:line` references.

```bash
bash scripts/dispatch-triangulation.sh \
  --problem-file /path/to/problem.md \
  --lenses "security,perf,api,sre" \
  --output-dir /tmp/triangulation-XYZ
```

If `--lenses` is omitted, the default set is
`security,perf,api,sre,architecture`. Each agent has a 300-second timeout by
default. Outputs:

- `<output-dir>/<lens>-report.md`
- `<output-dir>/SYNTHESIS.md`
- `<output-dir>/.logs/<lens>.log`
- `<output-dir>/.prompts/<lens>-prompt.md`

Treat overlap across two or more reports as high-confidence. Treat unique
findings as lower-confidence until a human or follow-up verification confirms
them. Direct disagreements in `SYNTHESIS.md` need human adjudication.

## `dispatch-codex.sh`

Some branches include `scripts/dispatch-codex.sh` as a general implementation
dispatcher for writable Codex agents. Use that kind of wrapper for isolated
implementation batches in worktrees. Use the verification and triangulation
wrappers above when the goal is read-only review rather than editing.

## `new-html-artifact.sh`

Creates a dated self-contained HTML artifact from
`docs/agent-artifacts/html-artifact-template.html`.

```bash
bash scripts/new-html-artifact.sh runtime-explainer "Runtime explainer"
```

Use HTML artifacts for dense agent outputs that benefit from visual structure:
plans, PR explainers, architecture maps, design explorations, reports, and
throwaway editors with an export button. Markdown remains the canonical wiki
and fact substrate.

# Flaky Test Quarantine

`tests/flaky/quarantine.txt` is an advisory list of suspected flaky Go tests.
Each non-empty, non-comment line is appended to `go test -count=1` by
`scripts/flake-rate.sh`.

Example:

```text
./internal/team/... -run ^TestSomethingFlaky$
```

Add a test only after CI shows non-deterministic failures across 3 or more
runs. Do not add tests based on a single failure or without evidence.

Remove a test after a fix lands and 5 consecutive `scripts/flake-rate.sh` runs
pass for that quarantine entry.

`scripts/flake-rate.sh` is not a CI gate yet. It is an investigative tool. Per
Appendix A of the desktop platform plan, SLOs and quality gates do not become
PR-blocking until baselines exist on macOS, Windows, and Linux.

The default failure-rate threshold is `0.20`; set `FLAKE_RATE_THRESHOLD` for a
local investigation that needs a different cutoff.

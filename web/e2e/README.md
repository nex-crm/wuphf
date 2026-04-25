# web/e2e

Playwright smoke tests against the real wuphf web UI. Two specs, two phases:

| Spec | Phase | Precondition |
|---|---|---|
| `tests/wizard.spec.ts` | fresh install | **no** `~/.wuphf/onboarded.json` — wuphf serves the onboarding wizard |
| `tests/smoke.spec.ts` | post-onboarding shell | `~/.wuphf/onboarded.json` is **seeded** — wuphf serves the shell, with sidebar + agent panel |

CI runs both in `.github/workflows/ci.yml :: web-e2e` by booting wuphf twice (once with each precondition).

## Running locally

Use `web/e2e/run-local.sh`. It pins `WUPHF_RUNTIME_HOME` to a per-run tempdir so your real `~/.wuphf/onboarded.json` and `~/.wuphf/team/broker-state.json` are never touched.

```bash
# both phases (wizard, then shell — what CI does)
web/e2e/run-local.sh

# just one
web/e2e/run-local.sh wizard
web/e2e/run-local.sh shell

# alternate ports if 27891 collides locally
PORT=37891 web/e2e/run-local.sh
```

The script:

- Builds `web/dist` and the `wuphf` binary if missing.
- Pins `WUPHF_RUNTIME_HOME` to a per-run tempdir, sandboxing all on-disk state.
- For the shell phase, seeds `<RUNTIME_HOME>/.wuphf/onboarded.json` (same JSON CI writes — see `ci.yml :: seed onboarding state`) before launching.
- Launches wuphf on `27891` (configurable) and `27890` (broker port = web port − 1) so it never collides with a developer's normally-running `7891` wuphf.
- Cleans up on exit (kills wuphf, removes the tempdir).

## Why this script exists at all

The smoke spec assumes `onboarded.json` is seeded — without it, wuphf serves the wizard and `.agent-panel` (a shell-only component) never mounts, so the tests fail with a 10s locator timeout that looks like a UI regression but is really a missing precondition. The CI workflow handles this in shell; this script is the local-friendly equivalent so devs don't have to read the workflow YAML to figure out the contract.

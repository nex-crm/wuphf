# Screenshots for PR #738

Captured against the version-status-bar feature with mocked broker
responses (`/api/health`, `/api/upgrade-check`, `/api/upgrade/run`,
`/api/broker/restart`) so each modal state is reproducible without a
real wuphf install. The capture script lives outside the repo to avoid
adding a one-off Playwright harness to the test suite.

| File | State |
| --- | --- |
| `01-statusbar-up-to-date.png` | Status bar — current matches latest, green dot |
| `02-statusbar-update-available.png` | Status bar — amber dot |
| `03-modal-up-to-date.png` | Modal — up to date |
| `04-modal-update-available.png` | Modal — update available |
| `05-modal-install-complete.png` | Modal — Force update succeeded, Restart now CTA |
| `06-modal-install-failed.png` | Modal — install failed (EACCES) with retry command |
| `07-modal-restart-error.png` | Modal — Restart broker failed, inline error |

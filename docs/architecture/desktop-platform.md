# WUPHF — Deprecate TUI, Build a Cross-Platform Desktop Experience

## Context

Wuphf today is a Go single-binary that serves a React/Vite web UI from `web/dist` (embedded via `//go:embed all:web/dist` in `embed.go`) and exposes the broker over HTTP + SSE + WebSocket on `127.0.0.1:7891` (`internal/team/broker_web_proxy.go ServeWebUI`, `internal/team/broker_terminal.go`). The default user path is `npx wuphf` → npm shim downloads the platform binary → binary launches the broker → browser auto-opens to the local web UI. A bubbletea TUI (`cmd/wuphf/channel*.go` + `cmd/wuphf/channelui/` ≈ 26k LOC) remains as `--tui` opt-in; tmux is also used as an *internal* per-agent process backend (`internal/team/pane*.go`, `tmux_runner.go` ≈ 3.7k LOC).

The user has set the next direction:

1. Deprecate the TUI (the bubbletea/channelui *user surface*; tmux-as-process-backend is a separate concern).
2. Ship a real cross-platform desktop experience (mac/linux/windows) — installable, native notifications/tray/deep-link/auto-update, not "open browser to localhost".
3. Cloud is **explicitly deferred** for this plan; revisit later. Focus on local browser (mode 1) + native desktop (mode 2).
4. TypeScript-preferred for new code; Go and subprocesses are fine. No third runtime (no Elixir/OTP) — the existing Go broker handles fanout/SSE/transport bridging today.
5. Robust testing harness with quantitative SLOs (startup latency, RSS, IPC latency, render perf). Bundle size is **not** a top priority (high-bandwidth users assumed).
6. Fold in the highest-leverage deltas from `nex-crm/business-musings` agent comparison — Cabinet-style memory UX, Sandcastle-style run records, Claude-Squad-style coding lanes, OpenHands-style credibility/evals — without expanding into a new product.

The intended outcome: a polished desktop app on three OSes with measurable performance, with the bubbletea TUI removed cleanly and the existing web UI untouched as the product surface for both browser and desktop modes.

## Recommended Approach

### Architecture (single picture)

```text
┌─────────────────────────────────────────────────────────────┐
│ Wails v2 Desktop Shell  (Go)                                │
│   • native window, tray, notifications, deep-link, updater  │
│   • spawns broker in-process, embeds web/dist               │
│   • OS verbs gated to allowlisted package                   │
└──────────────────────────┬──────────────────────────────────┘
                           │  loopback HTTP + SSE + WebSocket
                           ▼
                ┌──────────────────────┐
                │ Broker (existing)    │   same binary as today,
                │ /api/*  /api/events  │   reused unchanged
                └──────────────────────┘
                           ▲
                           │  loopback HTTP + SSE + WebSocket  (mode 1)
                ┌──────────────────────┐
                │ Browser → web/dist   │   `npx wuphf` path
                └──────────────────────┘
```

**Two front doors, one broker, one UI bundle:**

- `cmd/wuphf` — headless binary (today's binary, unchanged). Used by `npx wuphf`, MCP, CI, `--cmd`. Continues to serve the web UI on `127.0.0.1:7891` for mode 1 (browser).
- `cmd/wuphf-desktop` — new Wails v2 entry point. Embeds the broker package in-process, hosts the WebView pointing at `web/dist`, exposes OS verbs through Wails events.

**Same `web/dist` bundle in both modes.** App-data traffic is HTTP + SSE + WebSocket on loopback in both modes; the agent terminal already uses WebSocket through `web/src/lib/agentTerminalSocket.ts`. In mode 2 the WebView still talks to the in-process broker on a loopback port — no Wails bindings for app data. The discipline that prevents drift is **machine-enforced**: see "Lint enforcement" below.

### Why Wails v2

Cloud is off the roadmap, so the cloud-portability tax that justifies Tauri/Electron's HTTP-only purity is not being earned. Wails wins on:

- One language (Go) for the shell + broker; TS for UI; no Rust, no Node main process.
- Smallest install, smallest RSS, fastest dev cycle for a Go-shop.
- The risk that contributors reach for Go↔JS bindings as a shortcut is **closed by lint** (below), not by trust.
- We retain the option to revisit cloud later: because we held HTTP + SSE + WebSocket for *app data* and only let Wails events handle OS verbs, the React UI plus broker package can be lifted into a hosted deployment without rewriting feature code.

### Lint enforcement (the discipline rule, machine-checked)

Three rules together prevent the "Wails shortcut" failure mode:

1. **Go side** — `depguard` rule in `.golangci.yml`: `github.com/wailsapp/wails/v2/...` and `github.com/wailsapp/wails/v3/...` may be imported **only** by files under `desktop/oswails/`. All broker, agent, transport, and team code is forbidden from importing Wails. The `desktop/oswails/` package surface is small and reviewed line-by-line.
2. **TS side** — Biome `noRestrictedImports` rule: `@wails/runtime`, `@wailsapp/runtime`, `wails-bindings` may be imported **only** under `web/src/desktop/`. All other web code (queries, mutations, components, stores) goes through `web/src/api/client.ts` (HTTP + SSE + WebSocket).
3. **Generated/import boundary side** — `scripts/check-wails-boundary.sh` runs in CI alongside depguard and Biome. Wails v2 can generate `wailsjs/...` imports outside `web/src/`, while `web/package.json` runs Biome only on `src/`, so `noRestrictedImports` is not enough by itself. The script must scan generated and checked-in paths for Wails imports outside `desktop/oswails/` and `web/src/desktop/` and fail the build.

All three rules ship before product Wails code; CI fails on violation.

### Desktop launcher contract

The existing `LaunchWeb` path blocks forever and owns browser-mode concerns: Nex onboarding, browser opening, stale broker cleanup, and PID files. That is wrong for a Wails shell. Add a sibling API:

```go
StartWeb(ctx, opts) -> {webURL, brokerURL, token, shutdown func()}
```

Wails calls `StartWeb`; the npm-shim/browser path keeps `LaunchWeb`. Both share a smaller `serve()` core so readiness, shutdown, restart, port conflict, and browser/desktop coexistence tests cover the same broker lifecycle.

### Desktop bootstrap and security model

Define the desktop bootstrap before Wails product UI work:

- Port allocation: fixed vs dynamic, and coexistence with `internal/workspaces/ports.go`.
- Token delivery: environment variable, local handshake, or IPC, with `/api-token` behavior specified for desktop mode.
- WebView origin: prefer same-origin loopback (`http://127.0.0.1:N`) over `wails://` unless a later security review proves a safer bootstrap.
- Transport contract: HTTP, SSE, and WebSocket stay on loopback; Wails events are only for OS verbs.

### Desktop release/versioning

Desktop releases use a separate `desktop-release` workflow with its own version cadence and update channels (`stable` / `beta`). Signing/notarization, updater manifests, and installer assets must not ride every `main` auto-release. The headless `cmd/wuphf` binary continues on the existing npm + GitHub Releases path.

### TUI deprecation arc (phased)

| Release | Action |
|---|---|
| v0.X.0 (Phase 1, ~wk 3) | Rename `--tui` → `--legacy-tui`. `--tui` stays as alias with a one-line stderr warning. CHANGELOG `Deprecated`. |
| v0.Y.0 (Phase 4, after desktop installers are green) | `--legacy-tui` exits non-zero unless `WUPHF_LEGACY_TUI_ACK=1` is set. CHANGELOG `Deprecated (hard)`. |
| v0.Z.0 (Phase 4, ~wk 11) | Delete `cmd/wuphf/channel*.go` and `cmd/wuphf/channelui/` (≈26k LOC). Delete `runChannelView` and `runTeam` from `cmd/wuphf/main.go`. CHANGELOG `Removed`. |

**Keep** `internal/team/pane_*.go` and `tmux_runner.go`. These are tmux-as-process-backend (per-agent stdio capture), used optionally by web mode (`startPaneCaptureLoops`). On Windows the codepath is already dormant. Add `docs/PANE-BACKEND.md` documenting that this surface is *backend, not UI*, so future contributors don't conflate it with the deprecated TUI.

**Web parity gate before deletion** (Phase 3 audit, against TUI apps in `cmd/wuphf/channelui/sidebar_apps.go` and slash commands in `internal/commands/slash.go`):

- Slash commands registry — every entry has a web composer hook
- @-mention picker (port from `channel_insert_search*`)
- Composer history (port from `composer_history*`)
- Cmd/Ctrl-K command palette (already shipped; verify parity)
- Keybindings panel
- Tray badge for "needs you" mailbox count

### Folding in business-musings competitive deltas

Three product surfaces land in the desktop v1 because they reinforce the "shared brain" thesis without expanding scope. The fourth (coding lanes) is OSS-differentiating per the MBA review and is included to back the dogfooding flywheel.

1. **Cabinet-style memory UX** (Phase 3) — add citation hover-cards on cited claims; surface per-article history using existing `EditLogFooter`; keep human edit-and-promote flow on notebook→wiki. Wiki and notebook routes already exist.
2. **Sandcastle-style run records** (Phase 3) — enrich receipt/run-record schema with tools, approvals, files changed, cost, and status. Receipts already exists as a top-level route; this work improves the record, not navigation.
3. **Claude-Squad-style coding lanes** (Phase 3) — extend `AgentPanel` with worktree state, attach/steer, diff, and PR-handoff. The panel already exists; this adds lane depth inside the office.
4. **OpenHands-style credibility** (Phase 2 → ongoing) — wire the scaffolded `evals/harness/` runner; expose nightly pass rates + perf SLOs in CI; keep SLOs advisory until macOS/Windows baselines exist.

### Testing harness with quantitative SLOs

A new `cmd/wuphfbench` Go binary (≈300 LOC) drives the SLOs below. Expand existing scaffolding (Playwright in `web/e2e/`, `bench/slice-1/` recall gate, `scripts/check-bundle-size.sh`, `scripts/benchmark.sh`) rather than introducing new frameworks.

| Metric | Tool | SLO (initial) |
|---|---|---|
| Cold start: binary launch → `/api/health` 200 | `cmd/wuphfbench` polls, 20 iters | p95 < 1500 ms (warm: < 600 ms) |
| Cold start: Wails shell launch → first paint | Playwright via Wails dev mode + `webdriver-bidi` | p95 < 2500 ms macOS / < 3500 ms Windows |
| RSS at idle | `ps -o rss=` / `tasklist /FO CSV`, 60s sample | p50 < 220 MB (shell + in-process broker) |
| RSS under load (20-msg chat) | same | p95 < 400 MB |
| IPC latency: loopback `/api/health` | `cmd/wuphfbench`, 1000 sequential | p99 < 8 ms |
| SSE event latency | broker emits ts'd synthetic event; web posts receive-time to `/api/bench/sse` | p99 < 50 ms |
| Render perf (LCP per route) | Playwright `performance.getEntriesByType('paint')` | LCP < 1500 ms |
| Eval pass rate (per agent class) | wire `evals/harness/` runner | ≥ 80% nightly on `main` |
| Install size (compressed installer) | CI step `du -sh dist/Wuphf.*` | tracked, not gated (per user) |

**Cross-platform CI matrix expansion:**

- Today: `ubuntu-latest` runs everything; `windows-latest` runs `--version`/`--help` smoke only; macOS is absent.
- Phase 1: flip Windows smoke to a real `go test ./internal/team/... ./cmd/wuphf/...` job. Fix POSIX-shell assumptions surfaced.
- Phase 4: add `macos-14` and `windows-2022` as full test runners. Use path-based filters (only on changes touching `internal/team/`, `cmd/wuphf/`, `desktop/`) on PR; full matrix on `push: main` to control runner cost.
- New `desktop-smoke` job per OS: `wails build --no-package` + `web/e2e-desktop/` Playwright (≈8 specs).
- Add `scripts/flake-rate.sh` — re-runs failing tests 3× on green main, posts flake-rate comment per PR. Pair with `tests/flaky/` quarantine list.

### Packaging, signing, auto-update

| OS | Format | Signing | Auto-update |
|---|---|---|---|
| macOS | `.dmg` (universal2: amd64 + arm64) | Apple Developer ID + `notarytool` | Sparkle (Wails updater plugins are immature; Sparkle is the macOS standard) |
| Windows | `.msi` + `.exe` (NSIS) | Azure Trusted Signing (no EV cert hardware token) | NSIS in-app update check + download |
| Linux | `AppImage` + `.deb` | None (publish SHA256) | AppImage built-in self-update; `.deb` shows in-app banner only |

Auto-update adds ≈1–2 weeks vs. Tauri's first-party updater. Ship in Phase 2.

`cmd/wuphf` headless binary continues to ship via npm shim and GitHub Releases tarballs (today's path, unchanged). Desktop app is a separate distribution: GitHub Releases + `nex.ai/download`, with Homebrew cask + winget submissions in Phase 4.

**Release strategy:** Desktop releases use a separate `desktop-release` workflow with their own cadence and `stable` / `beta` update channels. The headless `cmd/wuphf` binary continues on the existing auto-release path via npm + GitHub Releases. Signing/notarization, updater manifests, and installer assets are excluded from routine `main` auto-releases so desktop release failures do not block normal headless releases.

### Phasing (11 weeks)

Hard constraints:

- Do **not** hard-fail `--legacy-tui` before desktop installers are green on macOS, Windows, and Linux.
- Do **not** make SLOs PR-blocking before macOS and Windows baselines exist.
- Packaging/signing precedes auto-update; desktop smoke precedes both.

| Wk | Phase | Work |
|---|---|---|
| 1 | 1. Launcher contract | `StartWeb()` returning `{webURL, brokerURL, token, shutdown}` lands in `internal/team/`. Tests cover start/stop/restart/port conflict/coexistence. Lands before any Wails code. |
| 2 | 1. Bootstrap + security | Document and implement desktop bootstrap: port, token, origin, `/api-token`, and coexistence with `internal/workspaces/ports.go`. WebSocket on loopback is part of the contract. |
| 3 | 1. Lint guardrails + TUI rename | depguard + `scripts/check-wails-boundary.sh` with negative fixtures. Rename `--tui` → `--legacy-tui` (alias + warning). CHANGELOG `Deprecated`. |
| 4 | 1. Hello-world Wails | Signed hello-world Wails artifact + update manifest, end-to-end CI on macOS/Windows/Linux. No product code. Catches signing pain early. |
| 5 | 2. Wails shell on broker | `cmd/wuphf-desktop` calls `StartWeb()`, hosts WebView, deep-link `wuphf://`, single-instance lock. |
| 6 | 2. Native OS verbs | Tray, native notifications via `desktop/oswails/` Wails API, dock badge, autostart. `internal/team/notifier_*` remains agent wake/routing machinery, not OS notification delivery. |
| 7 | 2. `cmd/wuphfbench` advisory | SLOs land as advisory CI checks. Baselines collected on macOS/Windows/Linux. |
| 8 | 3. Web parity audit | Audit against `cmd/wuphf/channelui/sidebar_apps.go` and `internal/commands/slash.go`. Fill real gaps; skip already-shipped items such as receipts, wiki/notebooks/reviews, and Cmd/Ctrl-K. |
| 9 | 3. Citation hover + run-record enrichment | Cabinet/Sandcastle deltas: hover-cards on cited claims; receipt schema additions for tools/approvals/files changed. |
| 10 | 4. Full matrix + desktop smoke | `macos-14` + `windows-2022` full Go test runners. `desktop-smoke` Playwright green on all three OSes. SLOs flip from advisory to required only after baselines exist. |
| 11 | 4. Cut over | `--legacy-tui` hard-fails only after installers ship green. Delete `cmd/wuphf/channel*.go` + `cmd/wuphf/channelui/` (≈26k LOC). Ship desktop v1 on `nex.ai/download`. Submit Homebrew cask + winget. |

### Risks and explicit cuts

**Risks:**

1. Lint discipline drifts. Mitigation: rules ship in Phase 1, before any feature work depends on them.
2. Wails updater story weaker than Tauri. Mitigation: Sparkle (mature) on macOS; NSIS custom on Windows; AppImage built-in on Linux. ~1–2 wk extra.
3. WebView2 absent on long-tail Windows 10 LTSB. Mitigation: ship the WebView2 redistributable in the MSI.
4. macOS notarization queue stalls. Mitigation: 30-min timeout + 1 retry in release workflow.
5. Pane-backed tmux capture silently degrades on Windows / no-tmux Macs. Mitigation: runtime detect + one-time toast pointing at install instructions; Homebrew formula in Phase 4 declares `tmux` dependency.
6. CI cost triples with macOS + Windows full matrix. Mitigation: path-based filters on PR; full matrix only on `push: main`.

**Explicit cuts (will NOT do in v1):**

- No Mac App Store / Microsoft Store listings.
- No Windows EV cert (Trusted Signing instead).
- No Snap, no Flatpak (AppImage + `.deb` covers >95% of dev users).
- No in-app store for plugins/skills.
- No `.deb` auto-in-place upgrade (banner only).
- No second runtime (no Elixir/OTP, no separate Go fanout supervisor).
- No Wails bindings for app data (lint-enforced).
- No removal of `internal/team/pane_*.go` or `tmux_runner.go`.
- No retirement of the npm shim (it remains the headless distribution channel).
- No cloud mode in this plan (revisit after v1 ships).

### Critical files

**Modified:**
- `cmd/wuphf/main.go` — rename `--tui` → `--legacy-tui`; eventually delete `runChannelView`/`runTeam`
- `internal/team/broker_web_proxy.go` — codify broker contract (port, `/api-token`, DNS-rebinding guard) as the single transport
- `internal/team/launcher_web.go` — add `StartWeb(ctx, opts) -> {webURL, brokerURL, token, shutdown}` next to existing `LaunchWeb`
- `internal/workspaces/ports.go` — define browser/desktop port coexistence rules
- `web/src/api/client.ts` — single HTTP + SSE + WebSocket client for both modes
- `.golangci.yml` — add `depguard` rule for Wails import boundary
- `web/biome.json` — add `noRestrictedImports` for `@wails/*`
- `.github/workflows/ci.yml` — Windows smoke → real test job; add `desktop-smoke`; add macOS in Phase 4
- `.github/workflows/desktop-release.yml` — separate desktop release cadence, signing, updater manifests, and stable/beta channels
- `scripts/check-bundle-size.sh` — extend to track installer sizes per-OS

**New:**
- `cmd/wuphf-desktop/main.go` — Wails v2 entry point
- `desktop/oswails/` — only Go package allowed to import Wails runtime
- `web/src/desktop/` — only TS dir allowed to import `@wails/*`
- `cmd/wuphfbench/main.go` — quantitative SLO driver
- `web/e2e-desktop/` — Playwright specs for Wails shell smoke
- `docs/BROKER-CONTRACT.md` — single source of truth for the broker HTTP + SSE + WebSocket contract
- `docs/DESKTOP-BOOTSTRAP.md` — desktop port, token, origin, and `/api-token` contract
- `docs/DESKTOP-RELEASE.md` — desktop release cadence, channels, signing, updater manifests, and installer assets
- `docs/PANE-BACKEND.md` — clarifies tmux-as-process-backend ≠ deprecated TUI
- `scripts/check-wails-boundary.sh` — Wails import boundary guard, including generated `wailsjs/...` imports
- `scripts/flake-rate.sh` — flaky-test isolation
- `tests/flaky/` — quarantine list

### Verification

End-to-end checks before declaring v1 ready:

1. `bash scripts/test-go.sh` and `bash scripts/test-web.sh` green on `ubuntu-latest`, `macos-14`, `windows-2022`.
2. `cmd/wuphfbench` reports SLOs within budget on all three OSes; CI gate flips from advisory to required only after macOS and Windows baselines exist.
3. `desktop-smoke` Playwright suite green on all three OSes (cold start + tray + notification + deep-link + auto-update self-test).
4. Manual: `npx wuphf` (mode 1) and `Wuphf.app/.msi/.AppImage` (mode 2) both serve the same UI and pass the same chat→receipt→wiki flow.
5. `--legacy-tui` exits non-zero without `WUPHF_LEGACY_TUI_ACK=1`. After deletion in Phase 4, `--legacy-tui` is unrecognized and `--tui` is gone.
6. `evals/harness/` runner reports per-agent-class pass rates ≥ 80% on `main` for 7 consecutive nightly runs.
7. Lint: `golangci-lint run` and `bun run lint` reject any PR that imports Wails outside the allowlisted directories (verified by a deliberately-failing PR in CI).
8. Sign + notarize + install on a clean macOS 14, Windows 11, and Ubuntu 24.04 VM; first launch → first paint within SLO; tray icon visible; deep-link `wuphf://task/test` opens the app and routes correctly.

---

## Appendix A — Codex Review Log (2026-05-05)

The plan was reviewed by `codex exec` (gpt-5.5, read-only repo access) against the actual codebase. The main body above has been updated with these corrections; this appendix remains only as a review log.

### Factual corrections folded into the main body

1. **App-data transport is not HTTP+SSE-only.** The agent terminal already uses **WebSocket** (`web/src/lib/agentTerminalSocket.ts`, `web/src/api/client.ts:264`, `internal/team/broker_terminal.go`). The lint invariant must be "HTTP/SSE/WebSocket on loopback for app data" — not HTTP+SSE only. Wails events stay forbidden for app data.
2. **Desktop WebView ↔ broker bootstrap is not a no-op.** `web/src/api/client.ts:14 initApi()` expects same-origin `/api-token` or falls back to `http://localhost:7890`. The plan must specify whether the desktop WebView points at the in-process `ServeWebUI` loopback (preserving same-origin) or adds a new desktop bootstrap path. **Decision: same-origin loopback** — Wails' `assetserver` exposes the broker's HTTP listener so `initApi()` works unchanged.
3. **TUI deletion size is ~26k LOC, not ~17k.** `cmd/wuphf/channel*.go` ≈ 17,046 LOC; `cmd/wuphf/channelui/*.go` adds ≈ 9,056 LOC. Update CHANGELOG and PR descriptions accordingly.
4. **Parity sources were wrong.** `cmd/wuphf/channelui/manifest.go` is roster/channel projection, not the TUI app manifest. Correct sources for the parity audit:
   - TUI apps: `cmd/wuphf/channelui/sidebar_apps.go`
   - Slash commands: `internal/commands/slash.go`
5. **Several "new/promote" items already exist.** The plan over-claimed work that's already shipped:
   - Receipts is already a top-level surface in `web/src/lib/constants.ts`.
   - Wiki / notebooks / reviews are already first-class routes in `web/src/routes/routeRegistry.ts`.
   - Cmd/Ctrl-K and help shortcuts already exist in `web/src/hooks/useKeyboardShortcuts.ts`.
   Phase 3 work for these surfaces collapses to **citation hover-cards + history affordances + run-record schema enrichment**, not "promote to nav".
6. **`internal/team/notifier_*` is agent wake/routing machinery, not OS notifications.** Native OS notifications go through Wails' notification API in `desktop/oswails/` — they do not route through `notifier_*`.

### Highest-leverage requirements folded into the main body

1. **Define a real desktop launcher contract.** Today's `LaunchWeb` blocks forever, offers Nex onboarding, opens a browser, kills stale brokers, and writes PID files — none of that fits a Wails shell. Add a sibling: `StartWeb(ctx, opts) -> {webURL, brokerURL, token, shutdown func()}`. Wails calls `StartWeb`; the existing `LaunchWeb` keeps doing the npm-shim path. Both share a smaller `serve()` core.
2. **Define the desktop bootstrap/security model explicitly.** Fixed vs allocated port; token delivery (env var? handshake? IPC?); WebView origin (`http://127.0.0.1:N` vs `wails://`); `/api-token` behavior; coexistence with browser/workspace ports from `internal/workspaces/ports.go`.
3. **Split desktop release/versioning from auto-release.** Signing, updater manifests, test channels, and installer assets must not ride every `main` auto-release. Add a separate `desktop-release` workflow with its own version cadence and update channels (stable / beta).

### Most likely execution failures + mitigations

1. **Wails boundary lint will miss generated imports.** Wails v2 generates `wailsjs/...` imports outside `web/src/`. `web/package.json` runs Biome on `src/` only. **Mitigate:** add `scripts/check-wails-boundary.sh` with negative fixtures; run as a CI gate. Don't rely on Biome alone.
2. **In-process lifecycle will regress on shutdown/restart.** **Mitigate:** add tests for the `StartWeb` contract — start, readiness, stop, restart, port conflict, "browser mode already running" coexistence. Land before Wails UI work.
3. **Auto-update/signing will eat the schedule.** **Mitigate:** ship a signed *hello-world Wails artifact* with a working update manifest in CI **before** Phase 2 product integration. Catch keychain/notarization/cert problems on a no-stakes binary.

### Phasing review notes

The review noted the original phasing was mis-sequenced and recommended this order, now reflected in the main body:

| Wk | Phase | Work |
|---|---|---|
| 1 | 1. Launcher contract | `StartWeb()` returning `{webURL, brokerURL, token, shutdown}` lands in `internal/team/`. Tests for start/stop/restart/port-conflict/coexistence. **Lands before any Wails code.** |
| 2 | 1. Bootstrap + security | Document and implement the desktop bootstrap (port, token, origin, `/api-token`). Coexist with `internal/workspaces/ports.go`. WebSocket on loopback added to the contract. |
| 3 | 1. Lint guardrails + TUI rename | depguard + `scripts/check-wails-boundary.sh` with negative fixtures. Rename `--tui` → `--legacy-tui` (alias + warning). CHANGELOG `Deprecated`. |
| 4 | 1. Hello-world Wails | Signed hello-world Wails artifact + update manifest, end-to-end CI on macOS/Windows/Linux. No product code. **Catches signing pain early.** |
| 5 | 2. Wails shell on broker | `cmd/wuphf-desktop` calls `StartWeb()`, hosts WebView, deep-link `wuphf://`, single-instance lock. |
| 6 | 2. Native OS verbs | Tray, native notifications via `desktop/oswails/`, dock badge, autostart. (Not via `internal/team/notifier_*`.) |
| 7 | 2. `cmd/wuphfbench` advisory | SLOs land as **advisory** CI checks. Baselines collected on macOS/Windows/Linux. |
| 8 | 3. Web parity audit (corrected sources) | Audit against `cmd/wuphf/channelui/sidebar_apps.go` and `internal/commands/slash.go`. Fill real gaps; skip already-shipped items (receipts/wiki/notebooks/Cmd-K). |
| 9 | 3. Citation hover + run-record enrichment | Cabinet/Sandcastle deltas: hover-cards on cited claims; receipt schema additions for tools/approvals/files-changed. |
| 10 | 4. Full CI matrix + desktop smoke | `macos-14` + `windows-2022` full Go test runners. `desktop-smoke` Playwright green on all three OSes. SLOs flip from advisory to required **only after baselines exist**. |
| 11 | 4. Cut over | `--legacy-tui` hard-fails (only after installers ship green). Delete `cmd/wuphf/channel*.go` + `cmd/wuphf/channelui/` (≈26k LOC). Ship desktop v1 on `nex.ai/download`. Submit Homebrew cask + winget. |

**Hard rules from the review:**

- Do **not** hard-fail `--legacy-tui` before desktop installers are green on all three OSes.
- Do **not** make SLOs PR-blocking before macOS/Windows baselines exist.
- Packaging/signing **precedes** auto-update; desktop smoke **precedes** both.

### Updated invariants (replace earlier statements)

- **App data**: HTTP, SSE, **WebSocket** — all on loopback, all same-origin from the WebView.
- **OS verbs**: Wails events only, gated to `desktop/oswails/` (Go) and `web/src/desktop/` (TS).
- **Lint enforcement**: depguard (Go) + Biome (TS, `src/` only) + `scripts/check-wails-boundary.sh` (covers `wailsjs/` generated files).
- **TUI size for messaging**: ~26k LOC, not ~17k.

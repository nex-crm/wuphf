# WUPHF-as-desktop-app via Pake — feasibility spike

**Date:** 2026-06-13 · **Worktree:** `worktree-desktop-pake-spike` · **Status:** ✅ DECISION MADE → **Wails v2**

## DECISION (2026-06-13, founder)

Desktop shell = **Wails v2**, hosting the **existing Go binary's broker in-process**
(no sidecar), on the already-reserved `desktop/oswails/` boundary.

Rationale (full reasoning below): the Go binary is the long-term core and the
desktop app can't wait for the TS rewrite, so the wrap target is the Go binary.
For a Go shop wrapping a Go binary, Wails wins over Tauri/Electron/Pake because:
- **`LaunchWeb()` returns a live loopback URL** → a Go host can start the broker
  in-process and attach a window, deleting the entire sidecar lifecycle
  (spawn/handshake/crash-restart/orphan-kill) that Tauri & Electron must build.
- **Go-native** — OS verbs in Go, no Rust (Tauri) or Node (Electron) runtime to own.
- **Already designed** — `desktop/oswails/` is the depguard- + CI-guarded boundary.

Rejected:
- **Pake** — a one-shot URL→app generator (Tauri underneath); no process mgmt.
  Great spike tool (proved a webview wrapper works in an afternoon), not a
  shipping framework. Adding the mandatory broker-start drops you out of
  pake-cli into a hand-forked Tauri project.
- **Electron** — our `apps/desktop` shell is bolted to the TS rewrite we can't
  wait for; repointing it at the Go binary keeps the sidecar complexity, adds a
  150–250 MB Chromium+Node runtime, and still can't host the broker in-process.
- **Tauri** — proven working this spike, but Rust host can't run the Go broker
  in-process (forced external sidecar) + permanent Rust toolchain tax.

**Fallback:** Electron is the *only* bundled-Chromium option, so it's the named
fallback **iff** the OS-webview de-risk fails (WebKitGTK-on-Linux / WebAuthn
passkeys on the heavy SPA). Wails & Tauri share that OS-webview risk equally.

### Build plan
- **Phase 0 (de-risk, now):** minimal Wails v2 window in `desktop/oswails/` that
  boots the broker in-process and displays the WUPHF UI. Validate SSE +
  WebSocket + (later) WebAuthn through whatever window-load path works (Wails v2
  binds the window to its asset server; loading the loopback http origin likely
  needs a JS-redirect bootstrap so WS/SSE use a real http origin, since custom
  `wails://` scheme handlers don't carry WebSocket on WKWebView).
- **Phase 1:** OS verbs (native notifications, tray, dock badge, single-instance,
  deep-link, autostart, file pickers) — the reserved boundary's whole purpose.
- **Phase 2:** packaging/signing/notarization + auto-update (reuse installer intent).
- **Phase 3:** cross-platform — Windows WebView2, **Linux WebKitGTK + WebAuthn
  validation** (the fallback trigger point).

### Status — 2026-06-14 (draft PR)

- **(a) Windows** — shell cross-compiles (amd64/arm64); WebView2 runtime probe
  wired as CI (`.github/workflows/desktop-webview-probe.yml`). No local VM here.
- **(b) cosign/WebAuthn** — graceful degradation shipped: `isWebAuthnSupported()`
  in `web/src/api/webauthn.ts` + `CredentialRegistrationPanel` shows an
  "unavailable" state instead of a button that throws on WebKitGTK; regression
  test added (cosign has no Go backend today, so this is correct defensive
  behavior — a real local-approval path is deferred to when cosign ships).
- **(c) Phase 0 productionized** (verified on macOS): dynamic free **UI** port
  templated into the bootstrap via asset-server middleware (no more hardcoded
  7973); **OS app-data runtime home** (`os.UserConfigDir()/WUPHF/desktop`, not
  TempDir); native **single-instance lock** (2nd launch focuses the window);
  non-tagged `stub.go` keeps `go build ./...` valid; README + boundary check
  pass.
- **(R) Robust dogfood — shared office + attach** (verified on macOS): the
  desktop shares the user's **active workspace** (no `WUPHF_RUNTIME_HOME`
  override) and enforces **one broker per workspace**. New `office.json`
  sidecar (`internal/team/office_info.go`) records the running broker's web URL;
  `RunningOfficeURL()` (HTTP-probe liveness + stale self-heal) lets any
  front-end **attach** instead of double-booting. Wired both ways: the desktop
  attaches to a live office (no second broker), and `wuphf web` (CLI) opens the
  running office and exits instead of `killStaleBroker`-ing a peer. This also
  retires the "broker port still :7890" concern — one broker per workspace on
  that workspace's port is now correct. Table-driven tests for the sidecar +
  attach; `go build ./...` / vet / `internal/team` + `cmd/wuphf` suites green.
  - **Triangulation review done (2026-06-14)** — 4 orthogonal lenses (security,
    concurrency, wire-shape, SRE). Fix-pass landed, verified on macOS:
    - **Loopback-only** attach URL + **WUPHF-identity probe** (GET `/` must be
      200 *and* contain a WUPHF marker; redirects not followed) — a planted/
      foreign `office.json` can't navigate the WebView off-box or attach to a
      stranger's localhost service (Security HIGH/MED, SRE HIGH).
    - **Frozen `{schemaVersion, webURL}` envelope** — readers attach on any
      version ≥1 reachable; a newer office is never mistaken for "absent", so an
      older binary can't `killStaleBroker` + clobber a live peer (Wire-shape
      CRITICAL / version-skew).
    - **Clean shutdown clears `office.json`** on every runtime path (moved the
      clear ahead of the pane/headless branch in `Kill()` — verified it fires on
      CLI SIGINT) + desktop `OnShutdown` clears when it booted (SRE CRITICAL).
    - **Reader no longer deletes the sidecar** (was stomping a live peer whose
      probe timed out); **`writeOfficeInfo` moved right after the listener binds**
      (shrinks the cold-start window); `officeInfo` struct unexported (Concurrency
      HIGH, Wire-shape MED). New table-driven tests for each.
  - **Deferred (follow-ups, noted):** the desktop+CLI **simultaneous cold-start
    boot race** (no lock between check and boot — narrow ~ms window;
    SingleInstanceLock already covers desktop↔desktop) → add an advisory
    boot lock; the **headless-codex** path writes `office.pid` but not
    `office.json` (codex offices unattachable); `office.json` not in the Shred
    wipe set (self-heals); multi-workspace follow (`cli_current`); public
    `Launcher.Shutdown()`.

---



## TL;DR

Yes — Pake can wrap WUPHF into a native desktop app, and a working
**9.4 MB macOS `.app`** was built and verified in this spike. But Pake's
value stops at "webview pointed at a URL." It does **not** start or supervise
the Go broker, so the plain `pake` CLI produces a *demo wrapper that needs
`wuphf web` already running*, not a double-clickable product.

Turning that into a real shippable app means adding a **process sidecar**
(spawn the `wuphf` binary, wait for the port, load it) — which requires
customizing the underlying Tauri project, i.e. you're choosing **Tauri**, not
just running `pake`. That collides with the desktop work already in the repo
(`apps/desktop/` Electron shell, `desktop/oswails/` Wails reserve). **This is a
founder call, not a unilateral one** — see Open Questions.

## Phase 0 spike — RESULTS (PROVEN, 2026-06-13)

Built a working Wails v2 desktop shell on this worktree. Both unknowns cleared.

**Phase 0a — Wails WKWebView renders WUPHF over loopback (separate broker):**
`wails build` (16s) → 7.6 MB Go-only shell, redirect bootstrap → on launch the
WKWebView navigated `wails://` → `http://127.0.0.1:<port>` and the SPA booted:
**10 established `com.apple.WebKit.Networking` → broker connections** (HTTP +
event streams). So SSE/WebSocket ride a real http origin, exactly as in a
browser. (The `wails://` custom scheme can't carry a WebSocket — hence the
redirect-to-loopback bootstrap, not an embedded-asset proxy.)

**Phase 0b — single binary, broker IN-PROCESS (the whole reason for Wails):**
`desktop/oswails/{main.go,wails.json,frontend/dist/index.html}`, built with
`wails build -s -skipbindings -tags desktop` (4–11s). Verified:
- **One 43 MB binary**, one PID, **LISTENs on both `:7890` (broker) and the UI
  port** — the broker runs inside the GUI process. No sidecar, no second binary.
- WKWebView (`com.apple.WebKit`, separate xpc pid) held **14 established
  connections to the in-process UI port** — window + broker + webview, one
  process.
- `team.NewLauncher("")` → `SetNoOpen(true)` → `PreflightWeb()` → `LaunchWeb(port)`
  is the entire integration. `LaunchWeb` returns once listening (non-blocking),
  exactly as predicted.

**Two gotchas found + fixed (carry into the real impl):**
1. **macOS main thread.** Booting the broker (which spawns goroutines) before
   `wails.Run` knocked the main goroutine off the Cocoa main thread → no window.
   Fix: `runtime.LockOSThread()` in `init()` + boot the broker in a goroutine so
   `wails.Run` owns the main thread and the window opens instantly.
2. **Asset serving + startup race.** Wails v2 `AssetServer.Handler`-only did not
   serve the root document reliably; the embedded `index.html` path does. And a
   static redirect races the async broker boot. Fix: embedded bootstrap page
   that **polls the loopback origin and redirects once it answers**.

**Open productionization items (follow-ups, not blockers):**
- Broker currently binds default `:7890` + a UI port; the shell should use
  app-specific non-default ports and negotiate on collision (the bootstrap
  templates the chosen UI port instead of a hardcoded `7973`).
- OS app-data runtime home (not `os.TempDir()`); clean broker shutdown on window
  close (stop transports + clear office PID file); the `//go:build desktop` tag
  needs a `go build ./...`/CI story so the tagged-only package doesn't trip
  "build constraints exclude all Go files".
- **Cross-platform: Linux WebKitGTK PROVEN (2026-06-14)** — the weak-link
  webview renders WUPHF's full SPA correctly (real xvfb screenshot:
  `desktop-pake-build/screenshot-linux-webkitgtk-render.png`). Reproducible
  harness in `desktop/oswails/derisk-linux/` (Docker: WebKitGTK 4.1 + Go 1.26 +
  xvfb). Single in-process binary served it (broker `:7890` + UI `:7973` in one
  PID; **6 WebKitGTK `WebKitNetworkProcess` → broker connections**, streams up).
  - Build needs `go build -tags "desktop production webkit2_41"` — the
    `production` tag is what `wails build` injects; without it Wails refuses to
    run at startup ("will not build without the correct build tags"). And
    Ubuntu 24.04 ships WebKitGTK **4.1**, so `webkit2_41` is required (not 4.0).
  - Runtime needs `WEBKIT_DISABLE_DMABUF_RENDERER=1` under xvfb (no GPU) — the
    `libEGL DRI3` warning is the harmless software-render fallback.
  - **Capability probe on WebKitGTK (2026-06-14):** exercised the APIs the deep
    surfaces need, in-webview, PASS/FAIL screenshot
    `desktop-pake-build/screenshot-linux-webkitgtk-capability-probe.png`.
    Result — UA `AppleWebKit/605.1.15 … Version/60.5` (`secureContext=true`):
    - ✅ **Canvas 2D** (draw+pixel readback) → cytoscape OK
    - ✅ **WebGL 1.0**, ✅ **contenteditable + Selection/Range** (`execInsert=true`),
      ✅ **InputEvent/beforeinput** → Tiptap/ProseMirror OK
    - ✅ SSE, WebSocket, structuredClone, ResizeObserver, IntersectionObserver,
      crypto.subtle, BroadcastChannel, CSS `:has()`/`backdrop-filter`/`color-mix()`
    - ❌ **WebAuthn — FAIL**: `navigator.credentials` is **undefined** on
      WebKitGTK. This is the **cosign** passkey feature
      (`web/src/api/webauthn.ts` → `@simplewebauthn/browser`
      `startRegistration`/`startAuthentication`). There is **no current
      graceful-degradation gate** in cosign, so invoking it on Linux throws.
  - **Net Linux verdict: PASS for the whole product except cosign/WebAuthn.**
    The rich editor, graph, and all core surfaces render on the weak-link
    webview. Cosign needs a non-passkey approval path on Linux (desktop is
    single-user local, so a local in-app confirm suffices) — or detect-and-hide
    passkey enrollment when `navigator.credentials` is absent. Scoped feature
    follow-up, **not** a shell-level blocker and **not** an Electron trigger.
- **Windows (2026-06-14):** the shell **cross-compiles cleanly** for
  windows/amd64 (62 MB) + windows/arm64 (58 MB) from the Mac
  (`GOOS=windows go build -tags "desktop production"`; productionize with
  `-ldflags -H=windowsgui` for the GUI subsystem). Runtime WebView2 render not
  run locally (no UTM/QEMU/ISO on this host). Real confirmation is wired as CI:
  `.github/workflows/desktop-webview-probe.yml` (matrix ubuntu/macos/windows →
  build + launch + screenshot). **Strong prior:** WebView2 = Edge/Chromium (the
  engine Electron bundles) → all probe surfaces, **including the WebAuthn that
  failed on WebKitGTK**, are expected to pass (Chromium ships Web​Authn /
  Windows Hello). Windows is the lowest-risk of the three.
- **Fallback trigger** is now narrow: only a *cosign/WebAuthn* failure on
  WebKitGTK (fixable in-app with a local-approval fallback, since desktop is
  single-user local) or a deep-surface break would force Electron — and even
  then Electron would be **Linux-only**, mac/Windows stay Wails.

Screenshots: the painted UI inside the native window could not be captured in
this headless context — WKWebView does not paint an off-Space/occluded window,
which defeated every native-window grab (Pake and Wails alike). The wrapped UI
content is shown via a Playwright render of the same loopback origin
(`desktop-pake-build/screenshot-wrapped-ui.png`); native-window chrome in
`screenshot-wails-inprocess-window.png`. Functional proof is the WebKit→broker
socket evidence above.

---

## What WUPHF actually is (the constraint)

- Shipping WUPHF is a **single Go binary** (`cmd/wuphf`, 73 MB) that embeds
  `web/dist` (`embed.go`) and serves the React UI **and** the API over a
  loopback broker. `wuphf web` → UI on `127.0.0.1:7891`; the SPA talks to it
  over loopback HTTP/SSE/WebSocket.
- So "desktop WUPHF" is not "ship a static site" — the app is useless without
  the broker process. Any wrapper must boot that process.

## What was verified (evidence, not assertion)

| Check | Result |
|---|---|
| `pake-cli` installs & runs | ✅ `pake-cli 3.11.9` (pnpm global) |
| Toolchain present | ✅ Rust 1.94, cargo, Node 25, Xcode CLT |
| `pake http://localhost:7951 --name WUPHF --width 1400 --height 900` builds | ✅ Tauri release build, `2m28s` compile |
| Native macOS app produced | ✅ `WUPHF.app`, **9.4 MB** (shell binary 9.2 MB) |
| Native window opens with real chrome | ✅ traffic-lights + 1400×866 window (`screenshot-native-shell.png`) |
| Webview loads WUPHF from the broker | ✅ **`com.apple.WebKit.Networking.xpc` held an ESTABLISHED socket to `127.0.0.1:7951`** |
| Wrapped UI renders the product | ✅ onboarding "Pick a default runtime" (`screenshot-wrapped-ui.png`, captured via Playwright against the same broker) |

Repro: `wuphf --web-port 7951 --no-open` (throwaway `WUPHF_RUNTIME_HOME`),
then `pake http://localhost:7951 --name WUPHF …`, then `open WUPHF.app`.

### Caveats hit during the spike
- **DMG packaging failed** (`bundle_dmg.sh`) — known-flaky Pake step
  (Finder/AppleScript). The `.app` builds & signs fine; `--targets app` or a
  retry sidesteps it. Not blocking.
- **Ad-hoc signed, not notarized** ("skipping app notarization, no APPLE_ID…").
  Distribution needs an Apple Developer ID + notarization, same as any mac app.
- Screenshot of the *live Pake window content* is blank because WKWebView
  throttles rendering for an off-Space/occluded window in this headless
  context — an environment artifact, not a Pake defect. The socket proof +
  Playwright render together confirm the content loads.

## The load-bearing limitation

`pake --help` surface: `url`, `--name`, `--icon`, `--width/--height`,
`--use-local-file`, `--fullscreen`, `--hide-title-bar`, `--multi-arch`,
`--inject <css/js>`, `--targets`. **There is no flag to run a companion
process.** So:

- **Plain `pake` app** = opens a window at a URL. If the URL is
  `http://localhost:7891`, it shows a connection error unless the user
  *separately* ran `wuphf web`. Useless as a product, fine as a demo.
- A real app must spawn `wuphf` itself.

## Two paths

### Path A — demo wrapper (what this spike built)
`pake` the running broker URL. Zero code. Good for a screenshot / "what would
it feel like." **Not shippable** (needs the broker started by hand).

### Path B — shippable self-contained app (the real thing)
Stop using the `pake` CLI; take its Tauri project (or a fresh Tauri 2 app) and
add the Go binary as a **sidecar**:

1. Bundle `wuphf` as a Tauri `externalBin` (named with the target triple,
   e.g. `wuphf-aarch64-apple-darwin`).
2. On `setup()`, pick a free port, spawn
   `wuphf web --web-port <port> --no-open` (set `WUPHF_RUNTIME_HOME` under
   `app_data_dir`), parse the `Web UI: http://127.0.0.1:<port>` line from
   stdout (the binary does **not** support `--web-port 0` ephemeral cleanly —
   `ServeWebUI` formats `127.0.0.1:%d` literally, so pick the free port
   yourself), health-check it, then `window.navigate(url)`.
3. On exit, kill the sidecar (Tauri does this for `externalBin` children; add a
   `before-quit` guard for the `cloudflared`/share subprocesses `runWeb`
   spawns).

This is **exactly** what `apps/desktop/` already does in Electron
(`utilityProcess.fork` + ready handshake + crash-restart + cooperative
shutdown — see `apps/desktop/docs/modules/broker-spawn.md`), minus that the
Electron shell forks a *TypeScript* `@wuphf/broker` (a rewrite in progress),
whereas the Tauri sidecar would spawn the **existing Go binary**.

Use `localhost`, not `127.0.0.1`, for the window origin — WebAuthn RP IDs
reject IP literals (the Electron shell notes the same).

## How this sits against what's already in the repo

There are already **three** desktop tracks:

| Track | State | Broker it drives | Bundle size (typical) |
|---|---|---|---|
| `apps/desktop/` (Electron 42) | Built shell: sandbox, contextIsolation, IPC allowlist, broker supervisor, ~25 tests, security-model docs | a **new TS** `@wuphf/broker` (rewrite) | ~150–250 MB |
| `desktop/oswails/` (Wails) | Reserved boundary only (`.gitkeep` + README); OS verbs only | the existing **Go** broker over loopback | ~10–20 MB |
| **Pake / Tauri (this spike)** | Demo wrapper proven; sidecar path designed not built | could spawn the existing **Go** binary | **~10 MB** |

Observations:
- The Electron shell is the most *mature* boundary but is coupled to the
  **TS-broker rewrite** — a desktop app of *today's* Go product can't ship on
  it until that rewrite lands.
- Pake/Tauri + the Go binary could ship a desktop app of the **current**
  product in days at ~1/15th the size — but it's a 4th UI-shell technology and
  re-implements the sidecar/OS-verb/security work the Electron shell already
  has.
- Pake and Wails sit in nearly the same niche (thin native shell over the Go
  loopback broker). Wails is already the *blessed* boundary in this repo and is
  Go-native (no Rust, no second toolchain). If the goal is "small shell over
  the Go binary," **Wails is the closer fit to existing intent**; Pake/Tauri's
  edge is a faster zero-to-app and a slightly smaller bundle.

## Recommendation

1. **Ship-it-fast demo:** Path A is genuinely useful *today* for a founder demo
   / "desktop WUPHF exists" moment. ~10 min, no code. Keep it in this worktree.
2. **For a real product, don't adopt a 4th shell on a whim.** The honest
   choice is between:
   - **finish the Electron shell** (mature boundary, but waits on the TS-broker
     rewrite, heavy), or
   - **build the Tauri/Wails sidecar over the existing Go binary** (ships the
     current product now, ~10 MB).
   Pake is the *fastest on-ramp* to the second option, but once you add the
   sidecar you're committing to Tauri — at which point **Wails** (already
   reserved here, Go-only) deserves a head-to-head before picking Tauri.

Per repo rule "ASK before committing to a library/system choice" — surfacing
this rather than building Path B unprompted.

## Open questions for the founder

1. Is the desktop target **today's Go product** or the **TS-broker rewrite**?
   That single answer eliminates one or two of the three tracks.
2. If "today's Go product": **Tauri (Pake-derived) vs Wails** for the sidecar
   shell? Wails is Go-native and already the repo's reserved boundary; Tauri is
   what this spike proved fastest. Want a head-to-head spike?
3. Is a **demo wrapper** (Path A) wanted as a throwaway artifact now,
   independent of the product decision?

## Artifacts (in `desktop-pake-build/`, not committed)
- `WUPHF.app` — the built 9.4 MB Pake app (hardcoded to `localhost:7951`).
- `screenshot-native-shell.png` — the native window chrome.
- `screenshot-wrapped-ui.png` — the WUPHF UI the window wraps.

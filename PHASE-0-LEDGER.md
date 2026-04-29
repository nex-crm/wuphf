# Phase-0 Audit Ledger

Generated: 2026-04-29
Branch: feat-multi-workspace
Worktree: .worktrees/multi-workspace
Source HEAD: df7a0f56

This ledger is the output of "The Assignment" in the design doc:
`~/.gstack/projects/nex-crm-wuphf/najmuzzaman-feat-multi-workspace-design-20260428-125124.md`

Each `os.UserHomeDir()` and `os.Getenv("HOME")` direct call in the codebase is classified
as MUST MIGRATE (workspace state — needs `config.RuntimeHomeDir()`) or MUST NOT CHANGE
(LLM CLI auth / user-global / npm install detection).

**Total scope:** 31 hits surveyed (29 from `os.UserHomeDir()`, 2 from `os.Getenv("HOME")`).
**Migrate:** 18 sites. **Carve-out:** 11 sites. **Decide-in-Phase-0:** 2 sites.
**Estimate:** 3 days (under the 40-line cut-doctor trigger; doctor stays in v1).

## Must MIGRATE to `config.RuntimeHomeDir()`

| File:Line | Path resolved | Decision | Notes |
|---|---|---|---|
| `cmd/wuphf/channel_artifacts.go:460` | `~/.wuphf/...` (artifact storage) | MIGRATE | WUPHF state |
| `cmd/wuphf/import.go:327` | `~/.wuphf/...` (import path) | MIGRATE | WUPHF state |
| `cmd/wuphf/import.go:473` | `~/.wuphf/...` (import path) | MIGRATE | WUPHF state |
| `cmd/wuphf/channel.go:5911` | `~/.wuphf/...` | MIGRATE | WUPHF state (line shifted from design's :5875) |
| `cmd/wuphf/main.go:219` | `~/.wuphf/...` | MIGRATE | WUPHF state (line shifted from design's :198) |
| `cmd/wuphf/channel_integration.go:256` | `os.Getenv("HOME") + .wuphf/team/broker-state.json` | MIGRATE | **Separate fix** — uses `os.Getenv("HOME")` directly, won't show in audit grep. Replace with `config.RuntimeHomeDir()`. |
| `internal/calendar/store.go:27` | `~/.wuphf/calendar/...` | MIGRATE | WUPHF state |
| `internal/agent/session.go:22` | `~/.wuphf/sessions/` | MIGRATE | Reclassified by eng-review — was "verify; if Claude session paths, leave"; verified as WUPHF state (agent JSONL sessions, not Claude auth) |
| `internal/agent/task_runtime.go:29` | `~/.wuphf/office/tasks` | MIGRATE | Reclassified by eng-review — was "verify; if task worktree under user home, leave"; verified as WUPHF state |
| `internal/agent/tools.go:427` | `~/.wuphf/office/messages` | MIGRATE | WUPHF outbox state (line shifted from design's :420) |
| `internal/team/launcher.go:499` | `~/.wuphf/panics.log` | MIGRATE | WUPHF state (line shifted from design's :478) |
| `internal/team/worktree.go:254` | `~/.wuphf/task-worktrees/<repo>` | MIGRATE | Task git worktrees per-workspace (line shifted from design's :199) |
| `internal/team/worktree.go:543` | `~/.wuphf/task-worktrees/<repo>` (managedWorktreeRoots) | MIGRATE | Same scope (line shifted from design's :478) |
| `internal/team/broker_onboarding.go:115` | `~/.wuphf/wiki` (materializeBlueprintWiki) | MIGRATE | Wiki materialization on onboarding |
| `internal/team/broker_image_root.go:17` | `~/.wuphf/office/artist` (imagegenArtistRoot) | MIGRATE | **NEW SITE — eng-review didn't anticipate.** Image generation artifacts. Honors `WUPHF_IMAGEGEN_DIR` env override; default needs RuntimeHomeDir. |
| `internal/imagegen/storage.go:22` | `~/.wuphf/office/artist` (outputDir) | MIGRATE | **NEW SITE — eng-review didn't anticipate.** Mirrors `imagegenArtistRoot`. |
| `internal/team/headless_codex.go:1502` | `~/.wuphf/codex-headless` (headlessCodexRuntimeHomeDir) | MIGRATE | Codex outside-voice expansion confirmed — WUPHF state, not codex auth |
| `internal/team/headless_codex.go:1706` | `~/.wuphf/logs` (headless test log dir) | MIGRATE | WUPHF logs — workspace state |

**Subtotal: 18 must-migrate sites** (eng-review predicted 12 + 1 separate-fix; reality is 17 + 1 separate-fix). Add ~0.5 day for the 5 unanticipated sites.

## Must NOT CHANGE (LLM CLI auth / user-global / npm install detection)

| File:Line | Path resolved | Decision | Notes |
|---|---|---|---|
| `internal/config/config.go:24` | RuntimeHomeDir fallback | NO CHANGE | The function being migrated TO — leave |
| `internal/config/config.go:419` | codex config layering (line shifted from design's :331) | NO CHANGE | User-global codex config |
| `internal/team/headless_codex.go:1425` | HOME passthrough envvar to headless codex subprocess | NO CHANGE | Subprocess inherits user HOME for tool resolution |
| `internal/team/headless_codex.go:1476` | `~/.codex` (headlessCodexHomeDir) | NO CHANGE | Codex AUTH path |
| `internal/team/headless_codex.go:1494` | user real HOME (headlessCodexGlobalHomeDir) | NO CHANGE | User-global resolver |
| `internal/team/headless_opencode.go:79` | HOME envvar passthrough | NO CHANGE | Subprocess inheritance |
| `internal/team/memory_backend.go:433` | HOME envvar to gbrain MCP (gbrainMCPEnv) | NO CHANGE | gbrain user-global, design correctly carved out |
| `internal/team/memory_backend.go:447` | HOME envvar to gbrain MCP (gbrainMCPEnvVars) | NO CHANGE | Same |
| `internal/gbrain/cli.go:120` | gbrain HOME fallback check | NO CHANGE | gbrain user-global; only invoked when HOME env unset |
| `internal/team/broker.go:5214` | npm install detection (`detectLocalInstall`) | NO CHANGE | Walks up from user real HOME for npm dependency lookup. Not WUPHF state. |
| `cmd/wuphf-oc-probe/main.go:33` | `~/.wuphf/openclaw/identity.json` (probe utility) | NO CHANGE for v1 | Same as `internal/config/config.go:877` carve-out below — OpenClaw identity is user-global credentials. |

**Subtotal: 11 carve-out sites.**

## DECIDE in Phase 0

| File:Line | Path resolved | Decision needed | Recommendation |
|---|---|---|---|
| `internal/config/config.go:877` | `~/.wuphf/openclaw/identity.json` (`ResolveOpenclawIdentityPath`) | Migrate (per-workspace OpenClaw) OR carve out | **Recommend: carve out for v1** — OpenClaw identity is user-global device credentials; per-workspace identities is a separate feature decision. Add explicit comment: `// OpenClaw identity is user-global, intentionally NOT under WUPHF_RUNTIME_HOME.` |
| `internal/team/headless_opencode.go:321,326` | `~/.config/opencode/opencode.<slug>.json` (per-agent config) | Same-slug across workspaces collides. Workspace-namespace OR refuse to coexist | **Recommend: namespace by workspace.** Change `headlessOpencodeAgentConfigPath` to take a workspace name + slug; write per-agent configs under `<runtime_home>/.wuphf/opencode-configs/<slug>.json`. Pass via `OPENCODE_CONFIG` env (already supported per line 94). Avoids the cross-workspace collision codex flagged (the actual race is on per-agent files, not the base `opencode.json` which is read-only here). |

**Subtotal: 2 decide-in-Phase-0 sites.**

## Summary

- **Total ledger size:** 31 sites surveyed.
- **17 must-migrate** + 1 must-migrate via separate `os.Getenv("HOME")` fix = **18 migration touch points.**
- **11 carve-outs** with explicit non-migration comments.
- **2 decisions** to make at Phase-0 kickoff (recommendations above).
- **Cut trigger (>40):** NOT TRIGGERED. `wuphf workspace doctor` stays in v1.
- **Estimate confidence:** 3 days for the migration sweep + tests + integration leak test, matches design's "2-3 days" upper bound.

## Greppable assertion (test fixture)

The design's "greppable test" should run this regex against `cmd/` and `internal/`:

```bash
grep -rn 'os\.UserHomeDir()\|os\.Getenv("HOME")' cmd/ internal/ \
  | grep -v _test \
  | grep -v provider/ \
  | grep -v gbrain/ \
  | grep -v action/one
```

After Phase-0, every hit must be one of:
1. A site listed in this ledger as "NO CHANGE" (with an explicit `// user-global; NOT runtime-home` code comment), OR
2. Inside `config.RuntimeHomeDir()` itself (`internal/config/config.go:24`).

Any new hit that doesn't fall into one of these categories fails the test.

## Integration leak test scaffold

```go
func TestRuntimeHomeIsolation(t *testing.T) {
    runtimeHome := t.TempDir()
    realHomeBefore := snapshotWuphfDir(t, os.Getenv("HOME"))

    cmd := exec.Command(wuphfBinary, "--broker-port", "7920", "--web-port", "7921")
    cmd.Env = append(os.Environ(), "WUPHF_RUNTIME_HOME=" + runtimeHome)
    // ... start broker, run onboarding + send-message + create-task + scan-files via API ...

    realHomeAfter := snapshotWuphfDir(t, os.Getenv("HOME"))
    require.Equal(t, realHomeBefore, realHomeAfter,
        "leak detected: $HOME/.wuphf was modified during isolated run")

    require.DirExists(t, filepath.Join(runtimeHome, ".wuphf"))
}
```

This is the regression safety net the eng-review marked CRITICAL.

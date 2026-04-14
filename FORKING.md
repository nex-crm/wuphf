# FORKING WUPHF

Honest instructions for making WUPHF yours in about 45 minutes. This file is maintained — if any step breaks, file an issue.

Before you fork, read [`ARCHITECTURE.md`](ARCHITECTURE.md). It's one page. It will save you an afternoon of `grep -R`.

## 0. Pin to a release tag, not `main`

`main` moves daily. Fork from a tag.

```bash
git clone https://github.com/nex-crm/wuphf.git
cd wuphf
git checkout v0.0.2.0   # or the latest tag: git describe --tags --abbrev=0
git checkout -b your-fork
```

## 1. Run it without Nex (read-only, no vendor coupling)

WUPHF ships with optional Nex context graph integration. If you want a clean, vendor-free baseline:

```bash
./wuphf --no-nex
```

That's the whole fix. The `--no-nex` flag skips all Nex wiring at startup. No code edits needed.

If you want Nex gone from your fork entirely, there are four integration points to remove:

```bash
# 1. Delete the Nex packages
rm -rf nex/ mcp/nex*/

# 2. Delete the Nex API client
rm internal/action/nex_client.go

# 3. Remove the nex-mcp lookPath blocks (2 places)
#    internal/team/launcher.go      ~ line 3069  (search: nex-mcp)
#    internal/team/headless_codex.go ~ line 555   (search: nex-mcp)

# 4. Remove nex types from launcher context
#    internal/team/launcher.go ~ line 55   (nexFeedItemContentItem)
#    internal/team/launcher.go ~ line 1489 (selectImportantInsights / nexInsight)
```

Then in `cmd/wuphf/main.go`: delete the `--no-nex` flag and the import blocks that reference `nex`. The `ResolveNoNex()` calls in `cmd/wuphf/channel.go` and `internal/config/config.go` can be deleted or replaced with a constant `true`.

## 2. Strip the Office branding

WUPHF uses *The Office* (US) references throughout the UI and copy. If you're shipping this to enterprise customers, a non-English market, or just don't share the taste, here's where it lives:

| File | What to change |
|---|---|
| `README.md` | Ryan Howard quote, Michael Scott quotes, "The Name" section |
| `web/index.html` | Any "WUPHF" branding in the office UI |
| `cmd/wuphf/channel.go` | Welcome messages, slash command copy |
| `cmd/wuphf/channel_render.go` | Office-themed status lines |
| `internal/team/template.go` | Agent prompt templates that reference Office tone |
| `internal/teammcp/actions.go` | Action descriptions |

A fast pass scoped to source files only:

```bash
grep -rn "Ryan\|Michael\|Dunder\|Scranton\|WUPHF\.com" \
  --include='*.go' --include='*.html' \
  ./cmd ./internal ./web ./mcp
```

That will surface ~50 hits across 5 files:

| File | Hits | What to change |
|---|---|---|
| `web/index.html` | 25 | Intro splash copy, channel header strings |
| `cmd/wuphf/channel.go` | 18 | Slash-command help text (one joke per command) |
| `cmd/wuphf/channel_render.go` | 3 | Status line strings |
| `cmd/wuphf/main.go` | 2 | Startup notice strings |
| `cmd/wuphf/channel_workspace_state.go` | 1 | Workspace state notice |

Removing these strings doesn't affect command behavior — they're display copy only.

Rename the binary in `cmd/wuphf/` + `go.mod` + goreleaser config if you want a different command name.

## 3. Add your own agent pack

Packs live in Go (`internal/agent/packs.go`) as a static slice. Not YAML — yet. Recompile after editing.

Add an entry to `Packs`:

```go
{
    Slug:        "my-team",
    Name:        "My Team",
    Description: "What this pack is for",
    LeadSlug:    "lead",
    Agents: []AgentConfig{
        {
            Slug:        "lead",
            Name:        "Team Lead",
            Expertise:   []string{"your", "domains"},
            Personality: "One-sentence persona",
            PermissionMode: "plan", // or "auto"
        },
        // ...more agents
    },
},
```

Rebuild and launch:

```bash
go build -o wuphf ./cmd/wuphf
./wuphf --pack my-team
```

Permissions: `plan` means every tool call needs human approval in the Requests panel. `auto` lets the agent run but you can scope with `AllowedTools` (see existing `starter` pack for examples).

## 4. Swap the action layer

Default action providers are Composio (real-world actions: Gmail, CRM, etc.) and Telegram (bridge).

To add your own provider, look at `internal/teammcp/actions.go` for the interface. Register via `/config set action_provider <yours>`.

## 5. Cut a release of your fork

`.goreleaser.yml` is already configured. Edit the `release.github.owner/name` to point at your repo, then:

```bash
git tag v0.1.0
goreleaser release --clean
```

## What's intentionally hard to change

- **Broker push model.** It's the architectural spine. Replacing it means rewriting the project.
- **Per-turn fresh sessions.** This is the reason for the benchmark win. If you switch to `--resume`, you lose the 9× cost advantage.
- **Git worktree isolation.** Each agent works in its own branch. Removing this means agents share a working directory and can corrupt each other's in-progress files.

Fork anything above the broker freely. Fork the broker and you're building a different project.

## If you get stuck

- Issues: https://github.com/nex-crm/wuphf/issues
- Discord: see the badge in [`README.md`](README.md)
- The `CHANGELOG.md` is ground truth for what shipped in each tag.

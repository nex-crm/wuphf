package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/commands"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/teammcp"
	"github.com/nex-crm/wuphf/internal/upgradecheck"
	"github.com/nex-crm/wuphf/internal/workspace"
	"github.com/nex-crm/wuphf/internal/workspaces"
)

const appName = "wuphf"

// subcommandWantsHelp reports whether the remaining args after the subcommand
// name request help. We intercept this BEFORE invoking the subcommand so that
// `wuphf init --help` (and similar) never fire the destructive action.
func subcommandWantsHelp(rest []string) bool {
	for _, a := range rest {
		switch a {
		case "-h", "--help", "-help":
			return true
		}
	}
	return false
}

// printSubcommandHelp writes usage text for the given subcommand to stderr.
// Keeping descriptions short and on-brand — users reading --help are browsing,
// not debugging.
func printSubcommandHelp(sub string) {
	switch sub {
	case "init":
		fmt.Fprintln(os.Stderr, "wuphf init — first-time setup")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Installs the latest Nex CLI from npm and saves your default provider")
		fmt.Fprintln(os.Stderr, "and pack so future `wuphf` invocations just work.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf init")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "This writes to ~/.wuphf/config.json. Safe to re-run.")
	case "shred":
		fmt.Fprintln(os.Stderr, "wuphf shred — burn the whole workspace down")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Stops the running session, clears broker state, and deletes the team")
		fmt.Fprintln(os.Stderr, "roster, company identity, office task receipts, saved workflows, logs,")
		fmt.Fprintln(os.Stderr, "sessions, provider state, calendar, and local wiki memory.")
		fmt.Fprintln(os.Stderr, "Next launch reopens onboarding.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Preserved: task worktrees, OpenClaw device identity, config.json.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf shred           Prompts before wiping")
		fmt.Fprintln(os.Stderr, "  wuphf shred -y        Skip the confirmation")
	case "import":
		fmt.Fprintln(os.Stderr, "wuphf import — pull state from another tool")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf import --from legacy           Auto-detect a running external orchestrator")
		fmt.Fprintln(os.Stderr, "  wuphf import --from <directory>      Directory with state.json")
		fmt.Fprintln(os.Stderr, "  wuphf import --from <file.json>      Direct path to an export")
	case "memory":
		fmt.Fprintln(os.Stderr, "wuphf memory — manage the team wiki and legacy memory backends")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from nex           Import Nex memory into ~/.wuphf/wiki/team/")
		fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from gbrain        Import GBrain pages into the wiki")
		fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from <backend> --dry-run  Preview without committing")
		fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from <backend> --limit N  Cap the number imported")
	case "log":
		fmt.Fprintln(os.Stderr, "wuphf log — show agent task receipts")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Lists recent tasks from ~/.wuphf/office/tasks/ so you can see what")
		fmt.Fprintln(os.Stderr, "each agent actually did — tool by tool, with timestamps.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf log                     List recent tasks")
		fmt.Fprintln(os.Stderr, "  wuphf log <taskID>            Show one task in detail")
		fmt.Fprintln(os.Stderr, "  wuphf log --agent eng         Filter to one agent")
	case "mcp-team":
		fmt.Fprintln(os.Stderr, "wuphf mcp-team — start the team MCP server (used by agents, not humans)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf mcp-team")
	case "workspace", "ws":
		// Delegate to the workspace help printer so adding a new subcommand
		// only touches workspace.go.
		printWorkspaceHelp()
		return
	case "upgrade":
		fmt.Fprintln(os.Stderr, "wuphf upgrade — check npm for a newer wuphf and show the changelog")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Compares the running build against the latest published `wuphf` on npm.")
		fmt.Fprintln(os.Stderr, "When behind, prints the upgrade command and a conventional-commit-grouped")
		fmt.Fprintln(os.Stderr, "changelog from the GitHub compare API.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf upgrade           Print human-readable comparison + changelog")
		fmt.Fprintln(os.Stderr, "  wuphf upgrade --json    Emit the comparison as JSON for scripting")
	default:
		fmt.Fprintf(os.Stderr, "wuphf: unknown subcommand %q — run `wuphf --help` for the list.\n", sub)
	}
}

// printVisibleFlags prints all registered flags except those tagged "(internal)"
// or the meta-flag --help-all. Multi-character flag names render with the
// modern `--` prefix (Go stdlib uses a single `-` for historical reasons),
// single-character flags keep one dash.
func printVisibleFlags(w *os.File) {
	flag.VisitAll(func(f *flag.Flag) {
		if f.Name == "help-all" {
			return
		}
		if strings.Contains(f.Usage, "(internal)") {
			return
		}
		prefix := "-"
		if len(f.Name) > 1 {
			prefix = "--"
		}
		_, _ = fmt.Fprintf(w, "  %s%s\n    \t%s", prefix, f.Name, f.Usage)
		// Only emit a trailing (default ...) when the usage string hasn't
		// already mentioned the default itself.
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && !strings.Contains(f.Usage, "default") {
			_, _ = fmt.Fprintf(w, " (default %q)", f.DefValue)
		}
		_, _ = fmt.Fprintln(w)
	})
}

// initWorkspaces wires the workspaces package into the CLI factory, the
// broker's cross-broker URL resolver, and runs the migration to the
// symmetric multi-workspace layout. Idempotent — safe to call on every
// `wuphf` invocation, including subcommands that never touch workspaces.
func initWorkspaces() {
	orchestratorFactory = func() (workspaceOrchestrator, error) {
		return cliOrchestratorAdapter{}, nil
	}
	// targetBrokerURL is no longer wired into the broker — the orchestrator
	// owns cross-broker URL resolution now. Keeping the helper around for
	// the doctor/list paths that still surface the URL to humans.
	_ = targetBrokerURL

	if err := workspaces.MigrateToSymmetric(); err != nil {
		// Non-fatal: existing single-workspace installs keep working
		// even if the symmetric migration is blocked (e.g. a broker is
		// running on the legacy port).
		fmt.Fprintf(os.Stderr, "workspace migration: %v\n", err)
	}
}

// wireBrokerWorkspaces registers startup wiring for the launcher's broker.
// When the broker already exists it wires it immediately; otherwise the
// launcher runs the hook as soon as it constructs the broker and before it
// starts serving requests.
func wireBrokerWorkspaces(l *team.Launcher) {
	if l == nil {
		return
	}
	configure := func(b *team.Broker) {
		if b == nil {
			return
		}
		b.SetWorkspaceOrchestrator(brokerOrchestratorAdapter{})
		b.SetLauncherDrainer(l)
		b.SetAdminPauseExitFn(os.Exit)
	}
	l.SetBrokerConfigurator(configure)
	b := l.Broker()
	if b == nil {
		return
	}
	configure(b)
}

func main() {
	cmd := flag.String("cmd", "", "Run a command non-interactively")
	format := flag.String("format", "text", "Output format (text, json)")
	apiKeyFlag := flag.String("api-key", "", "API key for authentication")
	showVersion := flag.Bool("version", false, "Print version and exit")
	blueprintFlag := flag.String("blueprint", "", "Operation blueprint ID for this run")
	packFlag := flag.String("pack", "", "Operation blueprint ID (legacy pack alias supported)")
	fromScratchFlag := flag.Bool("from-scratch", false, "Start without a saved blueprint and synthesize the first operation from the directive")
	providerFlag := flag.String("provider", "", "LLM provider override for this run (claude-code, codex, opencode)")
	oneOnOne := flag.Bool("1o1", false, "Launch a direct 1:1 session with a single agent (default ceo)")
	channelView := flag.Bool("channel-view", false, "Run as channel view (internal)")
	channelApp := flag.String("channel-app", "", "Start channel view on a specific app (internal)")
	threadsCollapsed := flag.Bool("threads-collapsed", false, "Start with threads collapsed (default: expanded)")
	unsafeMode := flag.Bool("unsafe", false, "Bypass all agent permission checks (use for local dev only)")
	tuiMode := flag.Bool("tui", false, "Launch with tmux TUI instead of the web UI")
	webPort := flag.Int("web-port", 7891, "Port for the web UI (default 7891)")
	brokerPort := flag.Int("broker-port", 0, "Port for the local broker (default 7890)")
	noNex := flag.Bool("no-nex", false, "Disable Nex completely for this run")
	memoryBackend := flag.String("memory-backend", "", "Memory backend for organizational context (nex, gbrain, none)")
	opusCEO := flag.Bool("opus-ceo", false, "Upgrade CEO agent from Sonnet to Opus")
	collabMode := flag.Bool("collab", false, "Start in collaborative mode (all agents see all messages)")
	noOpen := flag.Bool("no-open", false, "Don't open browser automatically on launch")
	// --workspace=<name> overrides the active workspace for a single
	// invocation. The flag sets WUPHF_RUNTIME_HOME so every WUPHF state path
	// (config.RuntimeHomeDir() callers) lands in that workspace's tree
	// without flipping registry.cli_current. Persistent change is via
	// `wuphf workspace switch`.
	workspaceOverride := flag.String("workspace", "", "Use a specific workspace for this command only (does not change cli_current)")
	helpAll := flag.Bool("help-all", false, "Show all flags including internal ones")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WUPHF v%s — the terminal office Ryan Howard always wanted.\n\n", buildinfo.Current().Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s              Launch multi-agent team (web UI on :%d)\n", appName, *webPort)
		fmt.Fprintf(os.Stderr, "  %s --tui        Launch with tmux TUI instead\n", appName)
		fmt.Fprintf(os.Stderr, "  %s init         Install the latest CLI and save setup defaults\n", appName)
		fmt.Fprintf(os.Stderr, "  %s upgrade      Check npm for a newer version and show the changelog\n", appName)
		fmt.Fprintf(os.Stderr, "  %s shred        Burn the workspace down and reopen onboarding\n", appName)
		fmt.Fprintf(os.Stderr, "  %s import --from legacy  Import from a running external orchestrator (auto-detect)\n", appName)
		fmt.Fprintf(os.Stderr, "  %s log          Show what your agents actually did (task receipts)\n", appName)
		fmt.Fprintf(os.Stderr, "  %s memory migrate --from {nex,gbrain}  Port legacy memory into the team wiki\n", appName)
		fmt.Fprintf(os.Stderr, "  %s workspace ...  Manage multiple isolated WUPHF workspaces\n", appName)
		fmt.Fprintf(os.Stderr, "  %s --cmd <cmd>  Run a command non-interactively\n", appName)
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		printVisibleFlags(os.Stderr)
		fmt.Fprintf(os.Stderr, "\nFor all flags including internal ones: %s --help-all\n", appName)
	}

	flag.Parse()

	if *helpAll {
		fmt.Fprintf(os.Stderr, "WUPHF v%s — all flags (including internal):\n\n", buildinfo.Current().Version)
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *noNex {
		_ = os.Setenv("WUPHF_NO_NEX", "1")
	}
	if *brokerPort > 0 {
		_ = os.Setenv("WUPHF_BROKER_PORT", fmt.Sprintf("%d", *brokerPort))
	}
	if name := strings.TrimSpace(*workspaceOverride); name != "" {
		// Resolve the workspace's runtime_home and export it so downstream
		// state-path resolvers (config.RuntimeHomeDir) target the right
		// tree. Resolver runs against the registry — when Lane B's package
		// is wired this becomes a single call; for now we shell out to the
		// orchestrator factory, which prints a friendly error if not yet
		// wired.
		applyWorkspaceOverride(name)
	}
	if backend := strings.TrimSpace(*memoryBackend); backend != "" {
		normalized := config.NormalizeMemoryBackend(backend)
		if normalized == "" {
			fmt.Fprintf(os.Stderr, "error: unsupported memory backend %q (expected nex, gbrain, or none)\n", backend)
			os.Exit(1)
		}
		_ = os.Setenv("WUPHF_MEMORY_BACKEND", normalized)
	}
	if providerKind := strings.TrimSpace(*providerFlag); providerKind != "" {
		// Delegate validation to the provider registry — every Register()
		// call adds its Kind to the allow-list, so adding a new local
		// runtime (mlx-lm, ollama, exo, …) is one Register() away from
		// also being a valid --provider flag value without touching this
		// switch.
		if err := provider.ValidateKind(providerKind); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		_ = os.Setenv("WUPHF_LLM_PROVIDER", providerKind)
	}
	startFromScratch := *fromScratchFlag
	if startFromScratch {
		_ = os.Setenv("WUPHF_START_FROM_SCRATCH", "1")
		// user-global; intentionally NOT under WUPHF_RUNTIME_HOME —
		// WUPHF_GLOBAL_HOME captures the user's REAL home so downstream
		// code can still reach user-global auth (codex, opencode) at the
		// original location even when WUPHF_RUNTIME_HOME is later set
		// for from-scratch isolation. RuntimeHomeDir() can already be
		// pointing at a workspace tree at this point (applyWorkspaceOverride
		// runs above), so consult os.UserHomeDir directly.
		if home, herr := os.UserHomeDir(); herr == nil && strings.TrimSpace(home) != "" {
			_ = os.Setenv("WUPHF_GLOBAL_HOME", home)
		}
		if runtimeHome := fromScratchRuntimeHome(); runtimeHome != "" {
			_ = os.Setenv("WUPHF_RUNTIME_HOME", runtimeHome)
		}
	}

	selectedBlueprint := strings.TrimSpace(*blueprintFlag)
	if selectedBlueprint == "" {
		selectedBlueprint = strings.TrimSpace(*packFlag)
	}
	if startFromScratch {
		selectedBlueprint = "__blank_slate__"
	}

	if *showVersion {
		fmt.Printf("%s v%s\n", appName, buildinfo.Current().Version)
		os.Exit(0)
	}

	// Wire the workspaces orchestrator + run the symmetric-layout migration.
	// Placed after --help-all and --version early exits so read-only
	// invocations skip the filesystem migration entirely.
	initWorkspaces()

	// Channel view mode (launched by wuphf team in tmux)
	if *channelView {
		runChannelView(*threadsCollapsed, channelui.ResolveInitialOfficeApp(*channelApp), strings.TrimSpace(*channelApp) != "")
		return
	}

	// Handle subcommands
	args := flag.Args()

	// Warn if another wuphf binary is on PATH and may shadow this one. Interactive
	// only — scripted and stdio-subprocess entrypoints keep their output clean.
	firstSub := ""
	if len(args) > 0 {
		firstSub = args[0]
	}
	if shouldWarnShadow(*showVersion, *channelView, *cmd != "", isPiped(), firstSub) {
		warnPathShadow(os.Stderr)
	}

	if len(args) > 0 {
		sub := args[0]
		if subcommandWantsHelp(args[1:]) {
			printSubcommandHelp(sub)
			return
		}
		switch sub {
		case "mcp-team":
			if err := teammcp.Run(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "shred":
			if !confirmDestructive(args[1:], "shred", shredSummary) {
				fmt.Println("Cancelled. The office lives to serve another day.")
				return
			}
			if err := stopRunningSession(selectedBlueprint); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			res, err := workspace.Shred()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: shred workspace: %v\n", err)
				os.Exit(1)
			}
			printWipeResult("Shredded", res)
			fmt.Println("Next `wuphf` launch will reopen onboarding. Michael would be proud.")
			return
		case "init":
			dispatch("/init", *apiKeyFlag, *format)
			return
		case "upgrade":
			runUpgradeCheck(args[1:])
			return
		case "import":
			runImport(args[1:])
			return
		case "log":
			runLogCmd(args[1:])
			return
		case "memory":
			runMemory(args[1:])
			return
		case "workspace", "ws":
			runWorkspace(args[1:])
			return
		}
	}

	// Non-interactive: --cmd flag
	if *cmd != "" {
		dispatch(*cmd, *apiKeyFlag, *format)
		return
	}

	// Non-interactive: piped stdin
	if isPiped() {
		handled, err := consumePipedStdin(os.Stdin, func(line string) {
			dispatch(line, *apiKeyFlag, *format)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
			os.Exit(1)
		}
		if handled {
			return
		}
		// Fall through: stdin was a non-TTY pipe but had no data. This is the
		// common case on Windows when the binary is spawned via SSH,
		// scheduled tasks, or PowerShell Start-Process — the inherited stdin
		// is a closed pipe rather than a console handle. Without falling
		// through, the binary would exit silently on first launch.
	}

	// No startup upgrade notice here: the npm shim (npm/bin/wuphf.js, PR
	// #273) already prints a one-line stderr hint pointing at
	// `npm install -g wuphf@latest` before exec'ing the binary, and
	// transparently downloads & runs a newer release when one exists. The
	// in-app web banner is the additive surface for that flow; the shim
	// owns the CLI surface.

	// TUI mode: tmux-based interface
	if *tuiMode {
		runTeam(args, selectedBlueprint, *unsafeMode, *oneOnOne, *opusCEO, *collabMode)
		return
	}

	// Default: web UI
	runWeb(args, selectedBlueprint, *unsafeMode, *webPort, *opusCEO, *collabMode, *noOpen)
}

// upgradeJSONOutput is the wire shape emitted by `wuphf upgrade --json`.
// Embeds upgradecheck.Result so adding a field there flows through, plus an
// explicit Error field that is populated when the upstream call failed.
// Extracted as a function (and as a top-level shape) so the JSON contract
// is testable without invoking the os.Exit-bearing runUpgradeCheck path.
func upgradeJSONOutput(res upgradecheck.Result, err error) struct {
	upgradecheck.Result
	Error string `json:"error,omitempty"`
} {
	out := struct {
		upgradecheck.Result
		Error string `json:"error,omitempty"`
	}{Result: res}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

func runUpgradeCheck(args []string) {
	// Use a real flag.FlagSet so `--json=true`, `--help`, and unknown flags
	// behave consistently with the rest of the CLI (the previous
	// hand-rolled loop silently accepted `wuphf upgrade junk` and ignored
	// `--json=true`).
	// ContinueOnError so we get to handle the error here — under
	// ExitOnError the Parse call would call os.Exit itself and the
	// branch below would be unreachable.
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Emit the comparison as JSON for scripting")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printSubcommandHelp("upgrade") }
	if err := fs.Parse(args); err != nil {
		// flag prints the error itself; just exit. flag.ErrHelp on
		// `--help` exits 0, anything else is a usage error → 2.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	// Always do a fresh fetch — the user typed `wuphf upgrade` because they
	// want ground truth. Each upstream call gets its own deadline so a slow
	// npm response doesn't eat the GitHub-compare budget and trigger a
	// misleading "could not fetch changelog" warning.
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	res, err := upgradecheck.Check(checkCtx, nil)

	// JSON output: emit the full Result (including IsDevBuild and an
	// `error` field for scripted callers) and exit non-zero on failure
	// so `wuphf upgrade --json | jq …` distinguishes "no upgrade" from
	// "couldn't reach npm". Dev builds are exempt — they don't depend on
	// npm reachability, so a network blip shouldn't trip exit-code-aware
	// pipelines on contributor machines.
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(upgradeJSONOutput(res, err))
		// Exit non-zero when we couldn't actually compare — distinguishes
		// "no upgrade" from "couldn't reach npm" for `wuphf upgrade --json | jq …`
		// pipelines. Dev builds are exempt: they don't depend on npm
		// reachability so a network blip on a contributor box shouldn't
		// trip exit-code-aware scripts.
		if !res.IsDevBuild && (err != nil || res.Latest == "") {
			os.Exit(1)
		}
		return
	}

	if res.IsDevBuild {
		fmt.Printf("You are running a dev build of %s (built from source, version=%q).\n",
			appName, res.Current)
		fmt.Println("Skipping npm comparison — this binary did not come from npm.")
		return
	}

	if err != nil || res.Latest == "" {
		fmt.Printf("You are running %s v%s.\n", appName, strings.TrimPrefix(res.Current, "v"))
		fmt.Fprintf(os.Stderr, "warning: could not reach npm registry to check for updates (%v)\n", err)
		os.Exit(1)
	}

	if !res.UpgradeAvailable {
		fmt.Printf("You are running %s v%s. That is the latest published version.\n",
			appName, strings.TrimPrefix(res.Current, "v"))
		return
	}

	fmt.Printf("Update available: v%s → v%s\n", strings.TrimPrefix(res.Current, "v"), strings.TrimPrefix(res.Latest, "v"))
	fmt.Printf("Run: %s\n", res.UpgradeCommand)
	fmt.Printf("Diff: %s\n\n", res.CompareURL)

	// Fresh deadline for the GitHub call so a slow npm earlier doesn't
	// shrink the budget here.
	clCtx, cancelCL := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelCL()
	entries, cerr := upgradecheck.FetchChangelog(clCtx, nil, res.Current, res.Latest)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch changelog (%v)\n", cerr)
		return
	}
	fmt.Println("Changes:")
	fmt.Print(upgradecheck.FormatChangelog(entries))
}

func runTeam(args []string, packSlug string, unsafe bool, oneOnOne bool, opusCEO bool, collabMode bool) {
	l, err := team.NewLauncher(packSlug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if oneOnOne {
		agentSlug := team.DefaultOneOnOneAgent
		if len(args) > 0 {
			agentSlug = args[0]
		}
		l.SetOneOnOne(agentSlug)
	}

	if opusCEO {
		l.SetOpusCEO(true)
	}

	// Default: delegation mode (focus). --collab disables it.
	l.SetFocusMode(!collabMode)

	if unsafe {
		l.SetUnsafe(true)
		// Propagate the flag to child MCP processes so they can skip the
		// per-action human approval gate. The broker and teammcp servers read
		// this env var directly; the flag is deliberately local-only.
		_ = os.Setenv("WUPHF_UNSAFE", "1")
		fmt.Fprintf(os.Stderr, "\n\u26a0\ufe0f  UNSAFE MODE: All agents have unrestricted permissions.\n")
		fmt.Fprintf(os.Stderr, "   Prison Mike would be proud. Use for local dev only.\n\n")
	}

	if err := l.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	wireBrokerWorkspaces(l)

	fmt.Printf("Launching %s (%d agents)... the cast is assembling.\n", l.PackName(), l.AgentCount())

	if err := l.Launch(); err != nil {
		fmt.Fprintf(os.Stderr, "error launching team: %v\n", err)
		os.Exit(1)
	}
	if !l.UsesTmuxRuntime() {
		if token := strings.TrimSpace(l.BrokerToken()); token != "" {
			_ = os.Setenv("WUPHF_BROKER_TOKEN", token)
		}
		_ = os.Setenv("WUPHF_BROKER_BASE_URL", l.BrokerBaseURL())
		_ = os.Setenv("WUPHF_HEADLESS_PROVIDER", "codex")
		if oneOnOne {
			_ = os.Setenv("WUPHF_ONE_ON_ONE", "1")
			_ = os.Setenv("WUPHF_ONE_ON_ONE_AGENT", l.OneOnOneAgent())
		}
		defer func() { _ = l.Kill() }()
		runChannelView(false, channelui.ResolveInitialOfficeApp(""), false)
		return
	}

	fmt.Println("Team launched. Welcome to The WUPHF Office. Attaching...")
	fmt.Println()
	fmt.Println("  Ctrl+B arrow     switch between panes")
	fmt.Println("  Ctrl+B { or }    swap panes left/right")
	fmt.Println("  Ctrl+B z         zoom a pane full-screen")
	fmt.Println("  Ctrl+B d         detach (keeps running)")
	fmt.Println("  /quit            exit everything")
	fmt.Println()

	if err := l.Attach(); err != nil {
		// Attach failed (not a terminal, or tmux error).
		// Keep the process alive to maintain the broker.
		fmt.Fprintf(os.Stderr, "Could not attach to tmux (not a terminal?). The office is running without you — like when Michael went to New York.\n")
		fmt.Fprintf(os.Stderr, "Team is running in background. Attach manually:\n")
		fmt.Fprintf(os.Stderr, "  tmux -L wuphf attach -t wuphf-team\n")
		fmt.Fprintf(os.Stderr, "Broker running on %s\n", l.BrokerBaseURL())
		fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")
		// Block forever — broker + notification loop stay alive
		select {}
	}
}

func runWeb(args []string, packSlug string, unsafe bool, webPort int, opusCEO bool, collabMode bool, noOpen bool) {
	l, err := team.NewLauncher(packSlug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if unsafe {
		l.SetUnsafe(true)
		// Propagate so child MCP processes skip the per-action approval gate.
		_ = os.Setenv("WUPHF_UNSAFE", "1")
	}
	if opusCEO {
		l.SetOpusCEO(true)
	}
	l.SetFocusMode(!collabMode)
	l.SetNoOpen(noOpen)
	if err := l.PreflightWeb(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	wireBrokerWorkspaces(l)
	fmt.Printf("Launching %s web view (%d agents)... the browser is the office now.\n", l.PackName(), l.AgentCount())
	if err := l.LaunchWeb(webPort); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func fromScratchRuntimeHome() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	base := filepath.Join(cwd, ".wuphf")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return ""
	}
	dir, err := os.MkdirTemp(base, "from-scratch-runtime-")
	if err != nil {
		return ""
	}
	return dir
}

func dispatch(cmd string, apiKeyFlag string, format string) {
	if config.ResolveMemoryBackend("") != config.MemoryBackendNex {
		fmt.Fprintf(os.Stderr, "Non-interactive backend commands currently require the Nex memory backend. Selected backend: %s.\n", config.MemoryBackendLabel(config.ResolveMemoryBackend("")))
		os.Exit(1)
	}
	if isSetupCommand(cmd) {
		result := commands.Dispatch(cmd, "", format, 0)
		if result.Error != "" {
			fmt.Fprintf(os.Stderr, "error: %s\n", result.Error)
			os.Exit(1)
		}
		if result.Output != "" {
			fmt.Println(result.Output)
		}
		return
	}
	apiKey := config.ResolveAPIKey(apiKeyFlag)
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "No API key found. Set WUPHF_API_KEY, or run `%s` and type /init.\n", appName)
		os.Exit(2)
	}

	result := commands.Dispatch(cmd, apiKey, format, 0)
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", result.Error)
		if strings.Contains(result.Error, "401") || strings.Contains(result.Error, "auth") {
			os.Exit(2)
		}
		os.Exit(1)
	}
	if result.Output != "" {
		fmt.Println(result.Output)
	}
}

func isSetupCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	return trimmed == "/init" || trimmed == "init"
}

func isPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

// consumePipedStdin reads newline-delimited commands from r and invokes
// dispatch on each. It returns handled=true only if at least one line was
// dispatched — the empty-pipe case (handled=false) is the signal that the
// caller should fall through to the normal interactive launch.
//
// This split exists because os.Stdin reads as a non-character-device on
// Windows whenever the binary is spawned via SSH, scheduled tasks, or
// PowerShell Start-Process — even when no data is piped in. Treating those
// closed/empty pipes as "non-interactive command stream" causes wuphf to
// exit silently on every fresh launch, which is the Windows-launch bug PR
// #380 fixes.
func consumePipedStdin(r io.Reader, dispatch func(line string)) (handled bool, err error) {
	scanner := bufio.NewScanner(r)
	// Allow lines up to 1 MiB. Default bufio.Scanner caps at 64 KiB and
	// returns bufio.ErrTooLong on overflow, which would surface to the
	// user as a confusing "error reading stdin" rather than a dispatched
	// command. Long inline blueprints / pasted JSON are realistic at the
	// CLI seam.
	const maxLineBytes = 1 << 20
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		// Skip blank/whitespace-only lines without setting handled. Otherwise
		// `printf "\n" | wuphf` would dispatch the empty string (which fails
		// with a useless error) AND mark the input as handled — preventing
		// the fall-through to the web UI launch. A pipe that contains only
		// whitespace is morally equivalent to an empty pipe.
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		handled = true
		dispatch(line)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return handled, scanErr
	}
	return handled, nil
}

// shredSummary is the human-readable blast-radius blurb printed during the
// interactive confirm. Stays in sync with the web Settings "Danger Zone"
// copy by convention — if you update one, update the other so CLI and UI
// promises match.
const shredSummary = `This will:
  • Stop the running WUPHF session
  • Delete your team, company identity, office task receipts, workflows
  • Delete logs, sessions, provider state, calendar, and local wiki memory
  • Wipe broker runtime state
  • Reopen onboarding on next launch
Preserved: task worktrees, OpenClaw device identity, config.json.`

// confirmDestructive gates a destructive subcommand behind a y/N prompt.
// A "-y" / "--yes" in rest skips the prompt — useful for scripted teardown.
// Prints the full summary first so the user sees exactly what they're doing
// before typing.
func confirmDestructive(rest []string, verb, summary string) bool {
	for _, a := range rest {
		if a == "-y" || a == "--yes" || a == "-yes" {
			return true
		}
	}
	fmt.Fprintln(os.Stderr, summary)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "Type `%s` to confirm, anything else to cancel: ", verb)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.TrimSpace(line) == verb
}

// stopRunningSession stops any running tmux or web-mode WUPHF session.
// Safe to call when nothing is running — Kill is a no-op in that case.
// Tolerates NewLauncher failing (e.g. invalid blueprint) because we don't
// want a broken config to block the user from cleaning up.
func stopRunningSession(blueprint string) error {
	l, err := team.NewLauncher(blueprint)
	if err != nil {
		// Launcher couldn't hydrate — likely no running session anyway.
		// Fall through silently; the workspace wipe will still proceed.
		// This is the documented "tolerate broken config" behavior so
		// users can always clean up after a bad blueprint edit.
		return nil //nolint:nilerr // intentional: broken config shouldn't block cleanup
	}
	return l.Kill()
}

// printWipeResult reports what came off disk in a way that's useful for
// scripting (one path per line) and still readable interactively.
func printWipeResult(verb string, res workspace.Result) {
	if len(res.Removed) == 0 {
		fmt.Printf("%s: nothing to remove (workspace was already clean).\n", verb)
	} else {
		fmt.Printf("%s %d path(s):\n", verb, len(res.Removed))
		for _, p := range res.Removed {
			fmt.Printf("  - %s\n", p)
		}
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "warning: %s\n", e)
	}
}

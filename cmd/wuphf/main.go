package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/commands"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/teammcp"
	"github.com/nex-crm/wuphf/internal/workspace"
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
	case "shred", "kill":
		fmt.Fprintln(os.Stderr, "wuphf shred — burn the whole workspace down")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Stops the running session, clears broker state, and deletes the team")
		fmt.Fprintln(os.Stderr, "roster, company identity, office task receipts, and saved workflows.")
		fmt.Fprintln(os.Stderr, "Next launch reopens onboarding.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Preserved: logs, sessions, task worktrees, LLM caches, config.json.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf shred           Prompts before wiping")
		fmt.Fprintln(os.Stderr, "  wuphf shred -y        Skip the confirmation")
		fmt.Fprintln(os.Stderr, "  wuphf kill            (alias)")
	case "import":
		fmt.Fprintln(os.Stderr, "wuphf import — pull state from another tool")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf import --from legacy           Auto-detect a running external orchestrator")
		fmt.Fprintln(os.Stderr, "  wuphf import --from <directory>      Directory with state.json")
		fmt.Fprintln(os.Stderr, "  wuphf import --from <file.json>      Direct path to an export")
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

func main() {
	cmd := flag.String("cmd", "", "Run a command non-interactively")
	format := flag.String("format", "text", "Output format (text, json)")
	apiKeyFlag := flag.String("api-key", "", "API key for authentication")
	showVersion := flag.Bool("version", false, "Print version and exit")
	blueprintFlag := flag.String("blueprint", "", "Operation blueprint ID for this run")
	packFlag := flag.String("pack", "", "Operation blueprint ID (legacy pack alias supported)")
	fromScratchFlag := flag.Bool("from-scratch", false, "Start without a saved blueprint and synthesize the first operation from the directive")
	providerFlag := flag.String("provider", "", "LLM provider override for this run (claude-code, codex)")
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
	helpAll := flag.Bool("help-all", false, "Show all flags including internal ones")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WUPHF v%s — the terminal office Ryan Howard always wanted.\n\n", buildinfo.Current().Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s              Launch multi-agent team (web UI on :%d)\n", appName, *webPort)
		fmt.Fprintf(os.Stderr, "  %s --tui        Launch with tmux TUI instead\n", appName)
		fmt.Fprintf(os.Stderr, "  %s init         Install the latest CLI and save setup defaults\n", appName)
		fmt.Fprintf(os.Stderr, "  %s shred        Burn the workspace down and reopen onboarding\n", appName)
		fmt.Fprintf(os.Stderr, "  %s import --from legacy  Import from a running external orchestrator (auto-detect)\n", appName)
		fmt.Fprintf(os.Stderr, "  %s log          Show what your agents actually did (task receipts)\n", appName)
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
	if backend := strings.TrimSpace(*memoryBackend); backend != "" {
		normalized := config.NormalizeMemoryBackend(backend)
		if normalized == "" {
			fmt.Fprintf(os.Stderr, "error: unsupported memory backend %q (expected nex, gbrain, or none)\n", backend)
			os.Exit(1)
		}
		_ = os.Setenv("WUPHF_MEMORY_BACKEND", normalized)
	}
	if provider := strings.TrimSpace(*providerFlag); provider != "" {
		switch provider {
		case "claude-code", "codex":
			_ = os.Setenv("WUPHF_LLM_PROVIDER", provider)
		default:
			fmt.Fprintf(os.Stderr, "error: unsupported provider %q (expected claude-code or codex)\n", provider)
			os.Exit(1)
		}
	}
	startFromScratch := *fromScratchFlag
	if startFromScratch {
		_ = os.Setenv("WUPHF_START_FROM_SCRATCH", "1")
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
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

	// Channel view mode (launched by wuphf team in tmux)
	if *channelView {
		runChannelView(*threadsCollapsed, resolveInitialOfficeApp(*channelApp), strings.TrimSpace(*channelApp) != "")
		return
	}

	// Handle subcommands
	args := flag.Args()
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
		case "shred", "kill":
			if !confirmDestructive(args[1:], "shred", shredSummary) {
				fmt.Println("Cancelled. The office lives to serve another day.")
				return
			}
			if err := killRunningSession(selectedBlueprint); err != nil {
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
		case "import":
			runImport(args[1:])
			return
		case "log":
			runLogCmd(args[1:])
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
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			dispatch(scanner.Text(), *apiKeyFlag, *format)
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// TUI mode: tmux-based interface
	if *tuiMode {
		runTeam(args, selectedBlueprint, *unsafeMode, *oneOnOne, *opusCEO, *collabMode)
		return
	}

	// Default: web UI
	runWeb(args, selectedBlueprint, *unsafeMode, *webPort, *opusCEO, *collabMode, *noOpen)
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
		fmt.Fprintf(os.Stderr, "\n\u26a0\ufe0f  UNSAFE MODE: All agents have unrestricted permissions.\n")
		fmt.Fprintf(os.Stderr, "   Prison Mike would be proud. Use for local dev only.\n\n")
	}

	if err := l.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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
		runChannelView(false, resolveInitialOfficeApp(""), false)
		return
	}

	fmt.Println("Team launched. Welcome to The WUPHF Office. Attaching...")
	fmt.Println()
	fmt.Println("  Ctrl+B arrow     switch between panes")
	fmt.Println("  Ctrl+B { or }    swap panes left/right")
	fmt.Println("  Ctrl+B z         zoom a pane full-screen")
	fmt.Println("  Ctrl+B d         detach (keeps running)")
	fmt.Println("  /quit            exit everything")
	fmt.Printf("  %s shred        stop from outside\n", appName)
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

// shredSummary is the human-readable blast-radius blurb printed during the
// interactive confirm. Stays in sync with the web Settings "Danger Zone"
// copy by convention — if you update one, update the other so CLI and UI
// promises match.
const shredSummary = `This will:
  • Stop the running WUPHF session
  • Delete your team, company identity, office task receipts, workflows
  • Wipe broker runtime state
  • Reopen onboarding on next launch
Preserved: logs, sessions, task worktrees, LLM caches, config.json.`

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

// killRunningSession stops any running tmux or web-mode WUPHF session.
// Safe to call when nothing is running — Kill is a no-op in that case.
// Tolerates NewLauncher failing (e.g. invalid blueprint) because we don't
// want a broken config to block the user from cleaning up.
func killRunningSession(blueprint string) error {
	l, err := team.NewLauncher(blueprint)
	if err != nil {
		// Launcher couldn't hydrate — likely no running session anyway.
		// Fall through silently; the workspace wipe will still proceed.
		return nil
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

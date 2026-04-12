package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/commands"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/teammcp"
)

const version = "0.1.0"
const appName = "wuphf"

func main() {
	cmd := flag.String("cmd", "", "Run a command non-interactively")
	format := flag.String("format", "text", "Output format (text, json)")
	apiKeyFlag := flag.String("api-key", "", "API key for authentication")
	showVersion := flag.Bool("version", false, "Print version and exit")
	packFlag := flag.String("pack", "", "Agent pack (starter, founding-team, coding-team, lead-gen-agency)")
	providerFlag := flag.String("provider", "", "LLM provider override for this run (claude-code, codex)")
	oneOnOne := flag.Bool("1o1", false, "Launch a direct 1:1 session with a single agent (default ceo)")
	channelView := flag.Bool("channel-view", false, "Run as channel view (internal)")
	channelApp := flag.String("channel-app", "", "Start channel view on a specific app (internal)")
	threadsCollapsed := flag.Bool("threads-collapsed", false, "Start with threads collapsed (default: expanded)")
	unsafeMode := flag.Bool("unsafe", false, "Bypass all agent permission checks (use for local dev only)")
	tuiMode := flag.Bool("tui", false, "Launch with tmux TUI instead of the web UI")
	webPort := flag.Int("web-port", 7891, "Port for the web UI (default 7891)")
	noNex := flag.Bool("no-nex", false, "Disable Nex completely for this run")
	opusCEO := flag.Bool("opus-ceo", false, "Upgrade CEO agent from Sonnet to Opus")
	collabMode := flag.Bool("collab", false, "Start in collaborative mode (all agents see all messages)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WUPHF v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s              Launch multi-agent team (web UI on :%d)\n", appName, *webPort)
		fmt.Fprintf(os.Stderr, "  %s --tui        Launch with tmux TUI instead\n", appName)
		fmt.Fprintf(os.Stderr, "  %s init         Install the latest CLI and save setup defaults\n", appName)
		fmt.Fprintf(os.Stderr, "  %s shred        Stop the running team\n", appName)
		fmt.Fprintf(os.Stderr, "  %s import --from paperclip  Import from running Paperclip (auto-detect)\n", appName)
		fmt.Fprintf(os.Stderr, "  %s --cmd <cmd>  Run a command non-interactively\n", appName)
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *noNex {
		_ = os.Setenv("WUPHF_NO_NEX", "1")
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

	if *showVersion {
		fmt.Printf("%s v%s\n", appName, version)
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
		switch args[0] {
		case "mcp-team":
			if err := teammcp.Run(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "shred", "kill":
			l, err := team.NewLauncher(*packFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := l.Kill(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Team session shredded.")
			return
		case "init":
			dispatch("/init", *apiKeyFlag, *format)
			return
		case "import":
			runImport(args[1:])
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
		runTeam(args, *packFlag, *unsafeMode, *oneOnOne, *opusCEO, *collabMode)
		return
	}

	// Default: web UI
	runWeb(args, *packFlag, *unsafeMode, *webPort, *opusCEO, *collabMode)
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
		fmt.Fprintf(os.Stderr, "   This bypasses all tool approval prompts. Use for local dev only.\n\n")
	}

	if err := l.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Launching %s (%d agents)...\n", l.PackName(), l.AgentCount())

	if err := l.Launch(); err != nil {
		fmt.Fprintf(os.Stderr, "error launching team: %v\n", err)
		os.Exit(1)
	}
	if !l.UsesTmuxRuntime() {
		if token := strings.TrimSpace(l.BrokerToken()); token != "" {
			_ = os.Setenv("WUPHF_BROKER_TOKEN", token)
		}
		_ = os.Setenv("WUPHF_HEADLESS_PROVIDER", "codex")
		if oneOnOne {
			_ = os.Setenv("WUPHF_ONE_ON_ONE", "1")
			_ = os.Setenv("WUPHF_ONE_ON_ONE_AGENT", l.OneOnOneAgent())
		}
		defer l.Kill()
		runChannelView(false, resolveInitialOfficeApp(""), false)
		return
	}

	fmt.Println("Team launched. Attaching...")
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
		fmt.Fprintf(os.Stderr, "Could not attach to tmux (not a terminal?).\n")
		fmt.Fprintf(os.Stderr, "Team is running in background. Attach manually:\n")
		fmt.Fprintf(os.Stderr, "  tmux -L wuphf attach -t wuphf-team\n")
		fmt.Fprintf(os.Stderr, "Broker running on http://127.0.0.1:7890\n")
		fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")
		// Block forever — broker + notification loop stay alive
		select {}
	}
}

func runWeb(args []string, packSlug string, unsafe bool, webPort int, opusCEO bool, collabMode bool) {
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
	if err := l.PreflightWeb(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Launching %s web view (%d agents)...\n", l.PackName(), l.AgentCount())
	if err := l.LaunchWeb(webPort); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(cmd string, apiKeyFlag string, format string) {
	if config.ResolveNoNex() {
		fmt.Fprintf(os.Stderr, "Nex integration is disabled for this session (--no-nex). Start %s without --no-nex to use backend commands.\n", appName)
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
		fmt.Fprintf(os.Stderr, "No API key found. Set WUPHF_API_KEY or run: %s (interactive) then /init\n", appName)
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

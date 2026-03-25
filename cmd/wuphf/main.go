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
	packFlag := flag.String("pack", "", "Agent pack (founding-team, coding-team, lead-gen-agency)")
	channelView := flag.Bool("channel-view", false, "Run as channel view (internal)")
	threadsCollapsed := flag.Bool("threads-collapsed", false, "Start with threads collapsed (default: expanded)")
	unsafeMode := flag.Bool("unsafe", false, "Bypass all agent permission checks (use for local dev only)")
	noNex := flag.Bool("no-nex", false, "Disable Nex completely for this run")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WUPHF v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s              Launch multi-agent team\n", appName)
		fmt.Fprintf(os.Stderr, "  %s init         Install the latest CLI and save setup defaults\n", appName)
		fmt.Fprintf(os.Stderr, "  %s kill         Stop the running team\n", appName)
		fmt.Fprintf(os.Stderr, "  %s --cmd <cmd>  Run a command non-interactively\n", appName)
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *noNex {
		_ = os.Setenv("WUPHF_NO_NEX", "1")
	}

	if *showVersion {
		fmt.Printf("%s v%s\n", appName, version)
		os.Exit(0)
	}

	// Channel view mode (launched by wuphf team in tmux)
	if *channelView {
		runChannelView(*threadsCollapsed)
		return
	}

	// Handle "wuphf kill" subcommand
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "mcp-team":
			if err := teammcp.Run(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "kill":
			l, err := team.NewLauncher(*packFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := l.Kill(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Team session killed.")
			return
		case "init":
			dispatch("/init", *apiKeyFlag, *format)
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

	// Default: launch multi-agent team
	runTeam(nil, *packFlag, *unsafeMode)
}

func runTeam(args []string, packSlug string, unsafe bool) {
	l, err := team.NewLauncher(packSlug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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

	fmt.Println("Team launched. Attaching...")
	fmt.Println()
	fmt.Println("  Ctrl+B arrow     switch between panes")
	fmt.Println("  Ctrl+B { or }    swap panes left/right")
	fmt.Println("  Ctrl+B z         zoom a pane full-screen")
	fmt.Println("  Ctrl+B d         detach (keeps running)")
	fmt.Println("  /quit            exit everything")
	fmt.Printf("  %s kill         stop from outside\n", appName)
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

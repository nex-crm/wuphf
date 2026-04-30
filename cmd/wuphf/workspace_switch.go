package main

// `wuphf workspace switch <name>` — update cli_current and (optionally) open.
//
// The CLI flag --workspace=<name> on the top-level wuphf command is the
// "for this single command" override. `switch` is the persistent counterpart:
// it writes registry.cli_current so unqualified `wuphf` invocations target
// the chosen workspace from this point on.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// browserOpener is the seam tests use to assert --open dispatches a launch
// without actually opening anything. Production wires it to the platform's
// `open`/`xdg-open` shell-out.
var browserOpener = openInDefaultBrowser

func runWorkspaceSwitch(args []string) {
	fs := flag.NewFlagSet("workspace switch", flag.ContinueOnError)
	openFlag := fs.Bool("open", false, "Also launch the default browser pointed at the new workspace's web UI")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace switch — change the CLI default workspace")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace switch <name>")
		fmt.Fprintln(os.Stderr, "  wuphf workspace switch --open <name>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Updates registry.cli_current. Future unqualified `wuphf` runs target this workspace.")
		fmt.Fprintln(os.Stderr, "Pass --workspace=<name> on a single command to override without switching.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	positional := fs.Args()
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one workspace name is required")
		fs.Usage()
		os.Exit(2)
	}
	name := strings.TrimSpace(positional[0])
	if name == "" {
		printError("workspace name cannot be empty")
	}

	orch := resolveOrchestrator()
	ctx, cancel := workspaceCtx()
	defer cancel()

	ws, err := orch.Switch(ctx, name, *openFlag)
	if err != nil {
		printError("switch to %q: %v", name, err)
	}

	url := fmt.Sprintf("http://localhost:%d/", ws.WebPort)
	fmt.Fprintf(os.Stdout, "Switched to %q (broker :%d, web :%d)\n", ws.Name, ws.BrokerPort, ws.WebPort)
	fmt.Fprintf(os.Stdout, "Open: %s\n", url)

	if *openFlag {
		if err := browserOpener(url); err != nil {
			// Non-fatal: the user knows the URL, they can paste it themselves.
			fmt.Fprintf(os.Stderr, "warning: could not auto-open browser: %v\n", err)
			return
		}
		fmt.Fprintln(os.Stdout, "Browser launched.")
	}
}

// openInDefaultBrowser dispatches the platform's "open this URL" command.
// Linux uses xdg-open; macOS and Windows have native equivalents. Errors are
// surfaced verbatim because the user can act on them ("xdg-open not installed",
// "permission denied", etc.).
func openInDefaultBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// linux, freebsd, openbsd — xdg-open is the de-facto standard.
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	// Don't wait — the browser is now the user's problem, not ours.
	go func() { _ = cmd.Wait() }()
	return nil
}

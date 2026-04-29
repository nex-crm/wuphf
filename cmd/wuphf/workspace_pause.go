package main

// `wuphf workspace pause <name>` — drain & stop a workspace's broker.
//
// Default path: graceful Drain() with a 90s wall-clock timeout. The
// orchestrator owns the actual draining logic (Launcher.Drain across headless
// dispatch, scheduler, watchdog, notify poll, pane dispatch — see design doc
// Pause/Resume Semantics). The CLI surface is thin on purpose.

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runWorkspacePause(args []string) {
	fs := flag.NewFlagSet("workspace pause", flag.ContinueOnError)
	force := fs.Bool("force", false, "Skip graceful drain — SIGTERM, then SIGKILL after 5s")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace pause — stop a running workspace")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace pause <name>            Graceful drain (90s timeout)")
		fmt.Fprintln(os.Stderr, "  wuphf workspace pause <name> --force    Hard kill after 5s")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Pause stops the broker and halts agent dispatch. The workspace's state stays")
		fmt.Fprintln(os.Stderr, "intact on disk; resume restarts cleanly. While paused, no LLM tokens burn.")
	}
	if err := fs.Parse(args); err != nil {
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
	// Pause uses the long ctx because graceful drain budgets up to 90s and we
	// don't want our deadline to fire before the orchestrator's drain timer.
	ctx, cancel := workspaceCtxLong()
	defer cancel()

	if *force {
		fmt.Fprintf(os.Stdout, "Force-pausing %q (SIGKILL after 5s)...\n", name)
	} else {
		fmt.Fprintf(os.Stdout, "Pausing %q (graceful drain, up to 90s)...\n", name)
	}

	if err := orch.Pause(ctx, name, *force); err != nil {
		printError("pause %q: %v", name, err)
	}

	fmt.Fprintf(os.Stdout, "Paused %q. Resume with: wuphf workspace resume %s\n", name, name)
}

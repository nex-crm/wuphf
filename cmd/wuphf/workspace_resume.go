package main

// `wuphf workspace resume <name>` — spawn the broker, wait for port-bind.
//
// Resume is symmetric to pause: the orchestrator does the heavy lifting
// (spawn process, write PID file, restore state from broker-state.json
// snapshot). The CLI just invokes and prints the URL once it's bound.

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runWorkspaceResume(args []string) {
	fs := flag.NewFlagSet("workspace resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace resume — restart a paused workspace")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace resume <name>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Spawns the broker process, waits for port-bind (30s), and prints the URL.")
		fmt.Fprintln(os.Stderr, "Workspace state (team, wiki, office tasks) resumes exactly as it was at pause.")
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
	// Long ctx because broker spawn waits up to 30s for port-bind (see
	// design "Resume" step 3); allow headroom for slow disks.
	ctx, cancel := workspaceCtxLong()
	defer cancel()

	fmt.Fprintf(os.Stdout, "Resuming %q (spawning broker, waiting for port-bind)...\n", name)

	ws, err := orch.Resume(ctx, name)
	if err != nil {
		printError("resume %q: %v", name, err)
	}

	fmt.Fprintf(os.Stdout, "Resumed %q (broker :%d, web :%d)\n", ws.Name, ws.BrokerPort, ws.WebPort)
	fmt.Fprintf(os.Stdout, "Open: http://localhost:%d/\n", ws.WebPort)
}

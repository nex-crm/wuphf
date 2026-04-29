package main

// `wuphf workspace restore <trash-id>` — bring a shredded workspace back.
//
// The trash ID is the directory name under `~/.wuphf-spaces/.trash/` —
// `<name>-<unix-timestamp>`. Users get this from `wuphf workspace list --trash`.
// Restore allocates a FRESH port pair (the original ports may have been
// recycled) and the orchestrator may rename if a workspace with the original
// name already exists.

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runWorkspaceRestore(args []string) {
	fs := flag.NewFlagSet("workspace restore", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace restore — bring a trashed workspace back")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace restore <trash-id>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Find trash IDs with `wuphf workspace list --trash`. Restore allocates a fresh")
		fmt.Fprintln(os.Stderr, "port pair (original ports may have been reused). If the original name is taken,")
		fmt.Fprintln(os.Stderr, "the restored workspace gets a numeric suffix.")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	positional := fs.Args()
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one trash ID is required")
		fs.Usage()
		os.Exit(2)
	}
	trashID := strings.TrimSpace(positional[0])
	if trashID == "" {
		printError("trash ID cannot be empty")
	}

	orch := resolveOrchestrator()
	ctx, cancel := workspaceCtxLong()
	defer cancel()

	fmt.Fprintf(os.Stdout, "Restoring %q (allocating fresh port pair)...\n", trashID)

	ws, err := orch.Restore(ctx, trashID)
	if err != nil {
		printError("restore %q: %v", trashID, err)
	}

	fmt.Fprintf(os.Stdout, "Restored as %q (broker :%d, web :%d)\n", ws.Name, ws.BrokerPort, ws.WebPort)
	fmt.Fprintf(os.Stdout, "Open: http://localhost:%d/\n", ws.WebPort)
	fmt.Fprintf(os.Stdout, "Switch CLI default with: wuphf workspace switch %s\n", ws.Name)
}

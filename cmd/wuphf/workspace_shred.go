package main

// `wuphf workspace shred <name>` — move workspace tree to trash.
//
// Default: confirm dialog (type the name to confirm), then orchestrator moves
// `~/.wuphf-spaces/<name>/` to `~/.wuphf-spaces/.trash/<name>-<unix>/` for
// 30-day retention. `--permanent` skips trash (truly irreversible — the
// orchestrator does NOT keep a hidden copy).
//
// Special handling for `main`:
//   - The user must type "main" (NOT just press y) — same protection level as
//     today's `wuphf shred` confirm flow.
//   - We add an extra warning line about ~/.wuphf/ contents because main
//     contains the migrated state from the user's pre-multi-workspace install.

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runWorkspaceShred(args []string) {
	fs := flag.NewFlagSet("workspace shred", flag.ContinueOnError)
	permanent := fs.Bool("permanent", false, "Skip trash — delete the workspace tree immediately and irreversibly")
	yes := fs.Bool("yes", false, "Skip the interactive confirm prompt (use only for scripted teardown)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace shred — burn a workspace down")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace shred <name>             Move to trash (30-day retention)")
		fmt.Fprintln(os.Stderr, "  wuphf workspace shred <name> --permanent  Skip trash (irreversible)")
		fmt.Fprintln(os.Stderr, "  wuphf workspace shred <name> --yes        Skip the confirm prompt (scripts only)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Restorable for 30 days unless --permanent. List trashed workspaces with")
		fmt.Fprintln(os.Stderr, "`wuphf workspace list --trash` and bring one back with `wuphf workspace restore`.")
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

	if !*yes {
		ok, err := shredConfirmFromReader(os.Stdin, os.Stderr, name, *permanent)
		if err != nil {
			printError("read confirmation: %v", err)
		}
		if !ok {
			fmt.Fprintln(os.Stdout, "Cancelled. The office lives to see another quarter.")
			return
		}
	}

	orch := resolveOrchestrator()
	ctx, cancel := workspaceCtxLong()
	defer cancel()

	if err := orch.Shred(ctx, name, *permanent); err != nil {
		printError("shred %q: %v", name, err)
	}

	if *permanent {
		fmt.Fprintf(os.Stdout, "Permanently shredded %q. No restore available.\n", name)
		return
	}
	fmt.Fprintf(os.Stdout, "Shredded %q to trash. Restorable for 30 days via `wuphf workspace restore`.\n", name)
}

// shredConfirmFromReader prompts the user to type the workspace name to
// confirm a shred. Pure function for testability — callers pass in their own
// reader/writer.
//
// For `main`, we print an extra warning about the migrated ~/.wuphf/ contents
// because the user's entire pre-multi-workspace office lives there.
func shredConfirmFromReader(in io.Reader, out io.Writer, name string, permanent bool) (bool, error) {
	if name == "main" {
		fmt.Fprintln(out, "WARNING: shredding `main` removes your migrated ~/.wuphf/ workspace.")
		fmt.Fprintln(out, "         This is the workspace WUPHF created from your pre-multi-workspace install.")
		fmt.Fprintln(out, "         Everything in your team, wiki, office tasks, and broker state goes with it.")
		fmt.Fprintln(out, "")
	}
	if permanent {
		fmt.Fprintf(out, "PERMANENT SHRED — workspace %q will be deleted immediately and CANNOT be restored.\n", name)
		fmt.Fprintln(out, "Trash retention is BYPASSED with --permanent.")
		fmt.Fprintln(out, "")
	} else {
		fmt.Fprintf(out, "Shredding workspace %q. The tree moves to trash and is restorable for 30 days.\n", name)
		fmt.Fprintln(out, "")
	}
	fmt.Fprintf(out, "Type the workspace name (`%s`) to confirm, or anything else to cancel: ", name)

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	return strings.TrimSpace(line) == name, nil
}

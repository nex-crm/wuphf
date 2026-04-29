package main

// `wuphf workspace create <name>` — allocate ports, spawn broker, register.
//
// Validation lives in the orchestrator (it owns the reserved-name list and
// the slug regex). The CLI does a *cheap* shape check up front so users get
// fast feedback on obvious typos before we trip the orchestrator's heavier
// validation path. Anything else (dup-name, port exhaustion, blueprint
// missing) is the orchestrator's job.

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// slugShape is an OPTIMISTIC client-side regex — looser than the
// orchestrator's authoritative one (`^[a-z][a-z0-9-]{0,30}$`). The
// orchestrator gets the final word; this is just to catch the "you typed a
// space" case before we spawn anything.
var slugShape = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func runWorkspaceCreate(args []string) {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	blueprintFlag := fs.String("blueprint", "", "Blueprint slug to seed this workspace (defaults to inheriting from current)")
	fromScratch := fs.Bool("from-scratch", false, "Skip blueprint inheritance — start blank, run full onboarding")
	inheritFrom := fs.String("inherit-from", "", "Source workspace for inherited fields (default: cli_current)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace create — spawn a new workspace")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace create <name>")
		fmt.Fprintln(os.Stderr, "  wuphf workspace create --blueprint=founding-team demo-launch")
		fmt.Fprintln(os.Stderr, "  wuphf workspace create --from-scratch scratchpad")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Slug rules: lowercase letters, digits, hyphens. Must start with a letter. Max 31 chars.")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	positional := fs.Args()
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "error: workspace name is required")
		fs.Usage()
		os.Exit(2)
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "error: unexpected extra argument %q\n", positional[1])
		os.Exit(2)
	}

	name := strings.TrimSpace(positional[0])
	if !slugShape.MatchString(name) {
		printError("invalid workspace name %q — use lowercase letters, digits, and hyphens; must start with a letter; max 31 chars", name)
	}

	blueprint := strings.TrimSpace(*blueprintFlag)
	inherit := strings.TrimSpace(*inheritFrom)
	if *fromScratch && (blueprint != "" || inherit != "") {
		printError("--from-scratch cannot be combined with --blueprint or --inherit-from")
	}

	req := CreateRequest{
		Name:        name,
		Blueprint:   blueprint,
		FromScratch: *fromScratch,
		InheritFrom: inherit,
	}

	orch := resolveOrchestrator()

	// Three explicit progress lines so users on a slow machine see motion. We
	// don't have a streaming progress channel from the orchestrator yet, so
	// these are deliberately coarse — Lane B can wire a callback later.
	fmt.Fprintln(os.Stdout, "Allocating ports...")
	fmt.Fprintln(os.Stdout, "Spawning broker...")

	ctx, cancel := workspaceCtxLong()
	defer cancel()

	ws, err := orch.Create(ctx, req)
	if err != nil {
		printError("create workspace %q: %v", name, err)
	}

	fmt.Fprintf(os.Stdout, "Ready (broker :%d, web :%d)\n", ws.BrokerPort, ws.WebPort)
	fmt.Fprintf(os.Stdout, "Open: http://localhost:%d/\n", ws.WebPort)
	fmt.Fprintf(os.Stdout, "Switch CLI default with: wuphf workspace switch %s\n", ws.Name)
}

package main

// `wuphf workspace doctor` — reconcile registry vs. reality, prompt to fix.
//
// The orchestrator's Doctor() does the heavy detection work (orphan trees,
// zombie state, port conflicts, corrupt registry, missing/orphaned symlinks,
// partial migration, stuck `stopping` rows). The CLI's job is to render each
// finding, prompt y/N for the fix, and call FixDoctorIssue with the
// orchestrator's opaque FixID token.
//
// Each fix is opt-in. The user can dismiss any individual issue without
// affecting the others. There is no `--fix-all` flag in v1: the design doc
// makes doctor mandatory specifically because it's the recovery path for
// data-integrity issues, and silently auto-fixing those is exactly the kind
// of footgun the design doc warns against. We do offer `--yes` for scripted
// teardown and `--dry-run` for read-only inspection.

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runWorkspaceDoctor(args []string) {
	fs := flag.NewFlagSet("workspace doctor", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "Apply every detected fix without prompting (use only after a dry-run scan)")
	dryRun := fs.Bool("dry-run", false, "Report issues but do not prompt for or apply fixes")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace doctor — reconcile workspace registry against reality")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace doctor             Interactive: prompt y/N per issue")
		fmt.Fprintln(os.Stderr, "  wuphf workspace doctor --dry-run   Report issues, never prompt or fix")
		fmt.Fprintln(os.Stderr, "  wuphf workspace doctor --yes       Apply every fix without prompting")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Detects: orphan trees, zombie state, port conflicts, corrupt registry,")
		fmt.Fprintln(os.Stderr, "orphaned/missing compatibility symlinks, partial migrations, stuck `stopping`")
		fmt.Fprintln(os.Stderr, "rows, expired trash entries, and the opencode shared-config race.")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *yes && *dryRun {
		printError("--yes and --dry-run are mutually exclusive")
	}

	orch := resolveOrchestrator()
	ctx, cancel := workspaceCtxLong()
	defer cancel()

	report, err := orch.Doctor(ctx)
	if err != nil {
		printError("doctor scan: %v", err)
	}

	if len(report.Issues) == 0 {
		fmt.Fprintln(os.Stdout, "All clean. Registry, trees, ports, and symlinks are consistent.")
		return
	}

	fmt.Fprintf(os.Stdout, "Found %d issue(s):\n\n", len(report.Issues))

	mode := doctorModeInteractive
	if *yes {
		mode = doctorModeAutoYes
	} else if *dryRun {
		mode = doctorModeDryRun
	}

	if err := runDoctorIssueLoop(ctx, orch, report, os.Stdin, os.Stdout, mode); err != nil {
		printError("doctor remediation: %v", err)
	}
}

// doctorMode encodes the CLI's interaction posture for the issue loop. Pure
// enum so test cases can drive each mode without touching stdin.
type doctorMode int

const (
	doctorModeInteractive doctorMode = iota
	doctorModeAutoYes
	doctorModeDryRun
)

// runDoctorIssueLoop is the side-effect-free remediation core. Iterates
// issues, prompts (or auto-decides), calls FixDoctorIssue per accepted fix.
// Returns the FIRST fix error encountered to keep semantics simple — the
// remaining issues print as "skipped (prior fix failed)".
func runDoctorIssueLoop(ctx context.Context, orch workspaceOrchestrator, report DoctorReport, in io.Reader, out io.Writer, mode doctorMode) error {
	reader := bufio.NewReader(in)
	var firstErr error
	skipped := 0
	for i, issue := range report.Issues {
		if firstErr != nil {
			fmt.Fprintf(out, "[%d/%d] %s — skipped (prior fix failed)\n", i+1, len(report.Issues), issue.Kind)
			skipped++
			continue
		}
		fmt.Fprintf(out, "[%d/%d] %s — %s\n", i+1, len(report.Issues), issue.Kind, issue.Subject)
		if issue.Detail != "" {
			fmt.Fprintf(out, "        %s\n", issue.Detail)
		}
		fmt.Fprintf(out, "        fix: %s\n", issue.FixAction)

		var apply bool
		switch mode {
		case doctorModeDryRun:
			fmt.Fprintln(out, "        [dry-run] not applied")
			continue
		case doctorModeAutoYes:
			apply = true
			fmt.Fprintln(out, "        [--yes] applying")
		case doctorModeInteractive:
			fmt.Fprint(out, "        Apply this fix? [y/N]: ")
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				return fmt.Errorf("read confirmation for issue %d: %w", i+1, err)
			}
			ans := strings.ToLower(strings.TrimSpace(line))
			apply = ans == "y" || ans == "yes"
			if !apply {
				fmt.Fprintln(out, "        skipped")
				continue
			}
		}

		if err := orch.FixDoctorIssue(ctx, issue.FixID); err != nil {
			fmt.Fprintf(out, "        FAILED: %v\n", err)
			firstErr = err
			continue
		}
		fmt.Fprintln(out, "        applied")
	}
	if firstErr != nil {
		fmt.Fprintf(out, "\nStopped after first failure. %d issue(s) skipped.\n", skipped)
		return firstErr
	}
	fmt.Fprintln(out, "")
	switch mode {
	case doctorModeDryRun:
		fmt.Fprintln(out, "Dry-run complete. Re-run without --dry-run to fix.")
	default:
		fmt.Fprintln(out, "Doctor scan complete.")
	}
	return nil
}

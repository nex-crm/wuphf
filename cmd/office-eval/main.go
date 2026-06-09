// office-eval runs the U0.1 outcome eval harness (docs/specs/sota-uplift.md)
// against an in-process scratch office and prints the report. Checks marked
// known-gap are the executable form of the uplift plan: they stay red until
// their phase lands and must then be promoted to regression guards.
//
// Usage:
//
//	go run ./cmd/office-eval            # table + summary, exit 0 unless a non-known-gap check fails
//	go run ./cmd/office-eval -json      # raw JSON report
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/nex-crm/wuphf/internal/team"
)

func main() {
	jsonOut := flag.Bool("json", false, "emit the raw JSON report")
	flag.Parse()

	dir, err := os.MkdirTemp("", "wuphf-office-eval-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "office-eval:", err)
		os.Exit(2)
	}
	defer os.RemoveAll(dir)

	report, err := team.RunOfficeEvals(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "office-eval:", err)
		os.Exit(2)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, "office-eval:", err)
			os.Exit(2)
		}
	} else {
		for _, c := range report.Checks {
			status := "PASS"
			if !c.Pass {
				status = "FAIL"
				if c.KnownGap != "" {
					status = "KNOWN-GAP(" + c.KnownGap + ")"
				}
			}
			fmt.Printf("%-40s %-55s %s\n", c.Job, c.Check, status)
		}
		fmt.Printf("\n%d/%d checks pass; %d known gaps open; %d unexpected failures\n",
			report.Passed(), len(report.Checks), len(report.KnownGapStatus()), len(report.UnexpectedFailures()))
	}

	if len(report.UnexpectedFailures()) > 0 {
		os.Exit(1)
	}
}

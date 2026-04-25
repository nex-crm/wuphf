// cmd/eval-prompts — Slice 0.5 eval harness runner.
//
// Usage:
//
//	eval-prompts run                                  # run all suites
//	eval-prompts run -prompt extract                  # run one suite
//	eval-prompts run -case extract_001_promotion_...  # run one case
//
// Exit code 1 if any case fails.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown sub-command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: eval-prompts run [-prompt SUITE] [-case CASE_ID]")
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	promptFilter := fs.String("prompt", "", "filter to one prompt suite (extract|synthesis|query|lint)")
	caseFilter := fs.String("case", "", "run a single case by id")
	_ = fs.Parse(args)

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot locate repo root: %v\n", err)
		return 1
	}

	evalsDir := filepath.Join(repoRoot, "evals")

	// Collect cases to run.
	cases, err := collectCases(evalsDir, *promptFilter, *caseFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect cases: %v\n", err)
		return 1
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "no cases matched")
		return 1
	}

	// Print table header.
	fmt.Printf("%-10s  %-40s  %-8s  %s\n", "SUITE", "CASE", "RESULT", "TIME")

	passed := 0
	total := 0

	for _, casePath := range cases {
		res := runCase(casePath, repoRoot)
		total++

		suite := res.suite
		caseID := res.caseID
		if caseID == "" {
			// Fallback: derive from filename.
			base := filepath.Base(casePath)
			caseID = strings.TrimSuffix(base, ".json")
			suite = filepath.Base(filepath.Dir(casePath))
		}

		if res.err != nil {
			fmt.Printf("%-10s  %-40s  %-8s  %s\n", suite, caseID, "ERROR", res.elapsed.Round(100*millisecond))
			fmt.Printf("  └─ error: %v\n", res.err)
			continue
		}

		result := "PASS"
		if !res.pass {
			result = "FAIL"
		} else {
			passed++
		}

		fmt.Printf("%-10s  %-40s  %-8s  %s\n", suite, caseID, result, res.elapsed.Round(100*millisecond))
		for _, f := range res.failures {
			fmt.Printf("  └─ %s\n", f)
		}
	}

	fmt.Printf("\nSUMMARY: %d/%d passed\n", passed, total)

	if passed < total {
		return 1
	}
	return 0
}

// collectCases walks evals/*/*.json and returns paths matching the filters.
func collectCases(evalsDir, promptFilter, caseFilter string) ([]string, error) {
	var paths []string

	entries, err := os.ReadDir(evalsDir)
	if err != nil {
		return nil, fmt.Errorf("read evals dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		suite := entry.Name()

		// Skip non-suite directories (harness/).
		if suite == "harness" {
			continue
		}

		// Apply prompt suite filter.
		if promptFilter != "" && suite != promptFilter {
			continue
		}

		suiteDir := filepath.Join(evalsDir, suite)
		files, err := os.ReadDir(suiteDir)
		if err != nil {
			return nil, fmt.Errorf("read suite dir %s: %w", suite, err)
		}

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}

			// Derive case ID from filename for filter matching.
			caseID := suiteFromID2(suite, strings.TrimSuffix(f.Name(), ".json"))

			// Apply case filter.
			if caseFilter != "" && caseID != caseFilter {
				continue
			}

			paths = append(paths, filepath.Join(suiteDir, f.Name()))
		}
	}

	return paths, nil
}

// suiteFromID2 builds the canonical case ID from suite + filename stem.
// e.g. suite="extract", stem="001_promotion_announcement"
// → "extract_001_promotion_announcement"
func suiteFromID2(suite, stem string) string {
	// If stem already has the suite prefix, return as-is.
	if strings.HasPrefix(stem, suite+"_") {
		return stem
	}
	return suite + "_" + stem
}

// findRepoRoot walks up from the binary's location looking for go.mod.
// Falls back to CWD if not found.
func findRepoRoot() (string, error) {
	// Start from the directory containing the executable.
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}

	// Also try from the current working directory (more reliable in dev).
	cwd, _ := os.Getwd()

	for _, start := range []string{cwd, filepath.Dir(exe)} {
		if start == "" {
			continue
		}
		dir := start
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return cwd, nil
}

// millisecond is time.Millisecond expressed as a duration for rounding.
const millisecond = 1_000_000

// Command bench-slice-1 runs the Week 0 ship-gate benchmark for the wiki
// intelligence port (Slice 1). See bench/slice-1/README.md for the gate
// definition; see bench/slice-1/runner for the measurement logic.
//
// Exit codes:
//
//	0 — pass rate ≥ 85% (ship gate GREEN)
//	1 — pass rate < 85%  (ship gate RED)
//	2 — runner error (I/O, index, etc.)
//
// Usage:
//
//	go run ./cmd/bench-slice-1 [--corpus PATH] [--queries PATH] [--topk N]
//	                           [--iterations N] [--gate RATE] [--out PATH]
//
// --out writes the full textual report to the given file in addition to
// printing a compact summary to stdout.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nex-crm/wuphf/bench/slice-1/runner"
)

func main() {
	var (
		corpus  = flag.String("corpus", "", "path to corpus.jsonl (default bench/slice-1/corpus.jsonl)")
		queries = flag.String("queries", "", "path to queries.jsonl (default bench/slice-1/queries.jsonl)")
		topk    = flag.Int("topk", 20, "retrieval top-K")
		iters   = flag.Int("iterations", 3, "retrieval iterations per query (median reported)")
		gate    = flag.Float64("gate", 0.85, "pass-rate threshold to exit 0")
		outPath = flag.String("out", "", "write full text report to this path")
	)
	flag.Parse()

	cfg := runner.Defaults()
	cfg.TopK = *topk
	cfg.Iterations = *iters
	cfg.Gate = *gate
	cfg.Out = os.Stdout

	cfg.CorpusPath = *corpus
	cfg.QueriesPath = *queries
	if cfg.CorpusPath == "" {
		cfg.CorpusPath = defaultPath("corpus.jsonl")
	}
	if cfg.QueriesPath == "" {
		cfg.QueriesPath = defaultPath("queries.jsonl")
	}

	agg, results, err := runner.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench-slice-1: %v\n", err)
		os.Exit(2)
	}

	report := runner.FormatReport(agg, results)
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(report), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "bench-slice-1: write %s: %v\n", *outPath, err)
			os.Exit(2)
		}
	}
	// Always echo the report to stdout.
	fmt.Print(report)

	if agg.PassRate < cfg.Gate {
		fmt.Fprintf(os.Stderr, "\nSHIP GATE RED — pass rate %.2f%% < gate %.0f%%\n",
			agg.PassRate*100, cfg.Gate*100)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\nSHIP GATE GREEN — pass rate %.2f%% >= gate %.0f%%\n",
		agg.PassRate*100, cfg.Gate*100)
}

// defaultPath returns bench/slice-1/{name} resolved from the current working
// directory's module root. Works whether invoked from the module root or from
// any subdirectory inside it.
func defaultPath(name string) string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("bench", "slice-1", name)
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "bench", "slice-1", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join("bench", "slice-1", name)
}

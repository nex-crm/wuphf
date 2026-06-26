package team

// wiki_compile_log.go — the append-only compile journal at team/log.md. Every
// compile run appends exactly ONE line recording when it ran and what it did;
// prior entries are never rewritten. The journal is human-readable history, not
// machine state (that lives in .compile/state.json).
//
// The line builder is pure — it takes the timestamp as an argument (the caller
// injects the compile clock) so tests are deterministic and no time.Now() hides
// inside the builder.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// compiledLogPath is the on-disk location of the append-only compile journal.
const compiledLogPath = "team/log.md"

// buildLogLine renders one journal line for a compile run. now is the injected
// compile clock; sources/pages/updated/skipped are the run's tallies.
func buildLogLine(now time.Time, sources, pages, updated, skipped int) string {
	return fmt.Sprintf("## [%s] compile | %d sources -> %d pages (%d updated, %d skipped)",
		now.UTC().Format(time.RFC3339), sources, pages, updated, skipped)
}

// appendLogEntry appends line to the existing journal body, preserving every
// prior entry verbatim. A blank line separates entries; the result ends with a
// single trailing newline.
func appendLogEntry(existing, line string) string {
	existing = strings.TrimRight(existing, "\n")
	if strings.TrimSpace(existing) == "" {
		return line + "\n"
	}
	return existing + "\n\n" + line + "\n"
}

// appendCompileLog reads the current journal, appends one run line, and writes
// it back through the single-writer worker in "replace" mode. Append-only: the
// existing body is preserved; only a new line is added. A missing journal is
// treated as empty (first run).
func appendCompileLog(ctx context.Context, worker *WikiWorker, line string) error {
	existing := ""
	if body, err := worker.ReadArticle(compiledLogPath); err == nil {
		existing = string(body)
	}
	updated := appendLogEntry(existing, line)
	if _, _, err := worker.Enqueue(ctx, ArchivistAuthor, compiledLogPath, updated, "replace", "compile: log run"); err != nil {
		return err
	}
	return nil
}

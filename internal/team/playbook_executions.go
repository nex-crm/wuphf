package team

// playbook_executions.go is the append-only execution log for v1.3
// playbooks. Every time an agent invokes a compiled skill, it is expected
// to record one outcome entry back through the playbook_execution_record
// MCP tool; that entry lands here.
//
// Log location: team/playbooks/{slug}.executions.jsonl (a sibling of the
// source playbook article). Same append-only-jsonl shape as the v1.2
// entity fact log — wrong outcomes get counter-outcomes, not deletions.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MaxExecutionSummaryLen is the hard cap on the summary field. Picked to
// leave room for a real debrief paragraph without blowing up prompt budgets
// when the next agent reads the log.
const MaxExecutionSummaryLen = 4000

// MaxExecutionNotesLen bounds the optional free-form notes field.
const MaxExecutionNotesLen = 4000

// ErrExecutionLogNotRunning is returned when Append is called without a
// wiki worker. The broker wires these together in ensurePlaybookCompiler;
// tests instantiate ExecutionLog directly with a live worker.
var ErrExecutionLogNotRunning = errors.New("playbook executions: worker is not attached")

// PlaybookOutcome is the narrow set of states a run can end in.
type PlaybookOutcome string

const (
	PlaybookOutcomeSuccess PlaybookOutcome = "success"
	PlaybookOutcomePartial PlaybookOutcome = "partial"
	PlaybookOutcomeAborted PlaybookOutcome = "aborted"
)

// ValidPlaybookOutcomes is the whitelist used by the validator.
func ValidPlaybookOutcomes() []PlaybookOutcome {
	return []PlaybookOutcome{PlaybookOutcomeSuccess, PlaybookOutcomePartial, PlaybookOutcomeAborted}
}

// Execution is one recorded run of a compiled playbook. Fields match the
// on-disk JSONL — adding a field is a forward-compatible change, removing
// one is a break and needs an entry-format version bump.
type Execution struct {
	ID         string          `json:"id"`
	Slug       string          `json:"slug"`
	Outcome    PlaybookOutcome `json:"outcome"`
	Summary    string          `json:"summary"`
	Notes      string          `json:"notes,omitempty"`
	RecordedBy string          `json:"recorded_by"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ExecutionLog is the append-only log rooted in a wiki repo. Safe to share
// across goroutines.
type ExecutionLog struct {
	worker *WikiWorker
	mu     sync.Mutex
}

// NewExecutionLog constructs an ExecutionLog backed by the supplied worker.
func NewExecutionLog(worker *WikiWorker) *ExecutionLog {
	return &ExecutionLog{worker: worker}
}

// ValidateExecutionInput checks every field of a prospective execution
// entry. Returns nil when acceptable to persist.
func ValidateExecutionInput(slug string, outcome PlaybookOutcome, summary, notes, recordedBy string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)
	}
	found := false
	for _, o := range ValidPlaybookOutcomes() {
		if o == outcome {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("outcome must be one of success|partial|aborted; got %q", outcome)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("summary is required")
	}
	if len(summary) > MaxExecutionSummaryLen {
		return fmt.Errorf("summary must be <= %d chars; got %d", MaxExecutionSummaryLen, len(summary))
	}
	if len(notes) > MaxExecutionNotesLen {
		return fmt.Errorf("notes must be <= %d chars; got %d", MaxExecutionNotesLen, len(notes))
	}
	if strings.TrimSpace(recordedBy) == "" {
		return fmt.Errorf("recorded_by is required")
	}
	return nil
}

// Append validates the inputs, serializes one Execution, and enqueues the
// append through the wiki worker. Returns the persisted Execution.
func (l *ExecutionLog) Append(ctx context.Context, slug string, outcome PlaybookOutcome, summary, notes, recordedBy string) (Execution, error) {
	if l == nil || l.worker == nil {
		return Execution{}, ErrExecutionLogNotRunning
	}
	summary = strings.TrimSpace(summary)
	notes = strings.TrimSpace(notes)
	recordedBy = strings.TrimSpace(recordedBy)
	if err := ValidateExecutionInput(slug, outcome, summary, notes, recordedBy); err != nil {
		return Execution{}, err
	}

	entry := Execution{
		ID:         uuid.NewString(),
		Slug:       slug,
		Outcome:    outcome,
		Summary:    summary,
		Notes:      notes,
		RecordedBy: recordedBy,
		CreatedAt:  time.Now().UTC(),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return Execution{}, fmt.Errorf("playbook executions: marshal: %w", err)
	}

	relPath := ExecutionLogRelPath(slug)
	l.mu.Lock()
	defer l.mu.Unlock()

	existing := l.readExistingLocked(relPath)
	buf := make([]byte, 0, len(existing)+len(line)+1)
	if len(existing) > 0 {
		buf = append(buf, existing...)
		if !strings.HasSuffix(string(existing), "\n") {
			buf = append(buf, '\n')
		}
	}
	buf = append(buf, line...)
	buf = append(buf, '\n')

	msg := fmt.Sprintf("playbook execution: %s — %s", slug, outcome)
	if _, _, err := l.worker.EnqueuePlaybookExecution(ctx, recordedBy, relPath, string(buf), msg); err != nil {
		return Execution{}, fmt.Errorf("playbook executions: enqueue: %w", err)
	}
	return entry, nil
}

// readExistingLocked returns the raw bytes at relPath, or nil when missing.
func (l *ExecutionLog) readExistingLocked(relPath string) []byte {
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(relPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		return nil
	}
	return bytes
}

// List returns every execution entry for the given slug, newest first.
// Malformed lines are skipped with a warning.
func (l *ExecutionLog) List(slug string) ([]Execution, error) {
	if l == nil || l.worker == nil {
		return nil, ErrExecutionLogNotRunning
	}
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)
	}
	relPath := ExecutionLogRelPath(slug)
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(relPath))
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("playbook executions: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	entries := make([]Execution, 0, 16)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry Execution
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Printf("playbook executions: skip malformed line %d in %s: %v", lineNo, relPath, err)
			continue
		}
		if entry.ID == "" || entry.Slug == "" || entry.Outcome == "" {
			log.Printf("playbook executions: skip underspecified line %d in %s", lineNo, relPath)
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("playbook executions: scanner error in %s after line %d: %v", relPath, lineNo, err)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries, nil
}

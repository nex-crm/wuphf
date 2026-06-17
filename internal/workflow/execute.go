package workflow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// execute.go runs a frozen contract and records the result. A RunRecord is the
// run observation the improvement loop mines (persisted OUTSIDE the kernel, as
// the browser-harness research prescribes). Execution is deterministic in its
// orchestration; the non-deterministic work (LLM drafting, external sends) is
// dispatched through the caller's ActionExec.

// RunRecord is one persisted execution of a contract.
type RunRecord struct {
	SpecID  string          `json:"spec_id"`
	Version string          `json:"version,omitempty"`
	At      string          `json:"at,omitempty"` // RFC3339, stamped by the caller
	Trigger string          `json:"trigger"`      // "manual" | "schedule" | "event"
	Events  []ScenarioEvent `json:"events"`
	Result  RunResult       `json:"result"`
}

// Execute runs the spec over events with the given ActionExec and returns a
// RunRecord. The caller stamps At and persists it (keeps the kernel clock-free
// and deterministic).
func Execute(s *Spec, trigger string, events []ScenarioEvent, exec ActionExec) RunRecord {
	return RunRecord{
		SpecID:  s.ID,
		Version: s.Version,
		Trigger: strings.TrimSpace(trigger),
		Events:  events,
		Result:  Run(s, events, exec),
	}
}

// AppendRun persists a run record as one JSONL line under O_APPEND.
func AppendRun(path string, rec RunRecord) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("runs path is empty")
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// ReadRuns parses a runs.jsonl into records (file order, oldest first). An
// absent file yields an empty slice; corrupt lines are skipped.
func ReadRuns(path string) ([]RunRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []RunRecord{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []RunRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

package team

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nex-crm/wuphf/internal/config"
)

// Workflow-detection substrate (T0). Production agents run headless
// (claude/codex) and bypass the in-process AgentLoop, so its output.log is
// never written. The tool-level signal those agents DO emit is the per-turn
// HeadlessEvent `manifest` (see headless_event.go), but that stream is
// in-memory only. This file persists each manifest as one durable JSONL line
// so the detection miner has a real cross-task corpus to read.
// Design: docs/specs/workflow-detection-positioning.md (sections 6B / T0).

// TurnToolCount is one tool and how many times a turn called it.
type TurnToolCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TurnManifest is the persisted detection record: one per agent turn,
// summarizing which tools the turn invoked. A task's "shape" is its ordered
// sequence of TurnManifests; the detection miner clusters tasks by that shape.
type TurnManifest struct {
	TaskID    string          `json:"task_id"`
	TurnID    string          `json:"turn_id,omitempty"`
	Agent     string          `json:"agent,omitempty"`
	StartedAt string          `json:"started_at,omitempty"`
	Tools     []TurnToolCount `json:"tools"`
}

const eventSinkFile = "events.jsonl"

// eventSinkMu serializes appends so concurrent turns never interleave a line.
var eventSinkMu sync.Mutex

// inlineTurnScopePrefix marks a pseudo-task synthesized for a task-less (inline /
// chat) turn so the detector can cluster repeated inline work-shaped turns. Each
// such turn is its own one-turn pseudo-task keyed by its turn id.
const inlineTurnScopePrefix = "turn:"

// isInlineTurnScope reports whether a (pseudo-)task id was synthesized for a
// task-less inline turn rather than being a real broker task.
func isInlineTurnScope(taskID string) bool {
	return strings.HasPrefix(strings.TrimSpace(taskID), inlineTurnScopePrefix)
}

// turnManifestFromEvent builds a TurnManifest from a manifest HeadlessEvent.
// Returns false when the event is not a usable manifest (wrong type or no named
// tool calls). A turn attributed to a real task records under that task id; a
// task-less inline turn (the CEO answering work-shaped chat without scoping a
// task — otherwise invisible to detection) records under a turn-scoped
// pseudo-task, but ONLY when it used >= 2 real WORK tools so conversational
// turns do not bloat the corpus.
func turnManifestFromEvent(e HeadlessEvent) (TurnManifest, bool) {
	if e.Type != HeadlessEventTypeManifest {
		return TurnManifest{}, false
	}
	if len(e.ToolCalls) == 0 {
		return TurnManifest{}, false
	}
	tools := make([]TurnToolCount, 0, len(e.ToolCalls))
	for _, tc := range e.ToolCalls {
		name := strings.TrimSpace(tc.ToolName)
		if name == "" {
			continue
		}
		tools = append(tools, TurnToolCount{Name: name, Count: tc.Count})
	}
	if len(tools) == 0 {
		return TurnManifest{}, false
	}
	taskID := strings.TrimSpace(e.TaskID)
	if taskID == "" {
		turnID := strings.TrimSpace(e.TurnID)
		if turnID == "" || workToolCount(tools) < 2 {
			return TurnManifest{}, false
		}
		taskID = inlineTurnScopePrefix + turnID
	}
	return TurnManifest{
		TaskID:    taskID,
		TurnID:    strings.TrimSpace(e.TurnID),
		Agent:     strings.TrimSpace(e.Agent),
		StartedAt: strings.TrimSpace(e.StartedAt),
		Tools:     tools,
	}, true
}

// workToolCount returns how many of the tools are real workflow work (not agent
// orchestration plumbing) — the signal that a turn did something worth detecting.
func workToolCount(tools []TurnToolCount) int {
	n := 0
	for _, t := range tools {
		if !isOrchestrationTool(t.Name) {
			n++
		}
	}
	return n
}

// EventSinkPath returns the durable detection-substrate file under the runtime
// home, or "" when no home is resolvable (then persistence is a no-op — a turn
// must never fail over telemetry). Exported so the detection miner reads the
// same path the broker writes.
func EventSinkPath() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", eventSinkFile)
}

// appendTurnManifest writes one record as a JSON line under O_APPEND. Takes an
// explicit path so tests can target a temp dir. A single Write under O_APPEND
// keeps readers from ever observing a partial tail (same discipline as
// entity_commit.go).
func appendTurnManifest(path string, m TurnManifest) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("event sink path is empty")
	}
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	eventSinkMu.Lock()
	defer eventSinkMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// persistTurnManifest is the production hook: best-effort, never surfaces an
// error to the turn. No-op when the event is not a usable manifest or no home
// is resolvable.
func persistTurnManifest(e HeadlessEvent) {
	m, ok := turnManifestFromEvent(e)
	if !ok {
		return
	}
	path := EventSinkPath()
	if path == "" {
		return
	}
	_ = appendTurnManifest(path, m)
}

// ReadTurnManifests parses an events.jsonl sink into records (file order, so
// oldest first). Corrupt lines are skipped so one bad write cannot poison the
// corpus. An absent file yields an empty slice, not an error.
func ReadTurnManifests(path string) ([]TurnManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []TurnManifest{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []TurnManifest
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var m TurnManifest
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

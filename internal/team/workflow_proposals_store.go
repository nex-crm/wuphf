package team

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// workflow_proposals_store.go persists workflows the completion-time extractor
// judged real, and surfaces them with a recurrence count. A proposal is stored
// once per completed task; the SAME workflow done across N tasks shares a
// fingerprint, so surfacing groups by fingerprint and reports how many distinct
// tasks produced it ("you've done this 4 times"). The deterministic miner is
// the cheap gate; this store is the LLM-extracted, executable output.

// storedProposal is one persisted extraction (one completed task).
type storedProposal struct {
	Fingerprint string                    `json:"fingerprint"`
	TaskID      string                    `json:"task_id"`
	Name        string                    `json:"name"`
	Confidence  float64                   `json:"confidence"`
	Trigger     workflow.ExtractedTrigger `json:"trigger"`
	Spec        *workflow.Spec            `json:"spec,omitempty"`
}

// ExtractedWorkflow is the surfaced, recurrence-counted proposal.
type ExtractedWorkflow struct {
	Fingerprint string                    `json:"fingerprint"`
	Name        string                    `json:"name"`
	Confidence  float64                   `json:"confidence"`
	Trigger     workflow.ExtractedTrigger `json:"trigger"`
	Recurrence  int                       `json:"recurrence"` // distinct completed tasks
	TaskIDs     []string                  `json:"task_ids"`
	Spec        *workflow.Spec            `json:"spec,omitempty"`
}

const proposalSinkFile = "workflow_proposals.jsonl"

var proposalSinkMu sync.Mutex

// proposalFingerprint is the workflow identity: its ordered action ids. Two
// completed tasks that ran the same steps share it, which is how recurrence is
// counted.
func proposalFingerprint(spec *workflow.Spec) string {
	if spec == nil {
		return ""
	}
	ids := make([]string, 0, len(spec.Actions))
	for _, a := range spec.Actions {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ">")
}

// ProposalSinkPath returns the durable proposal file under the runtime home.
func ProposalSinkPath() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", proposalSinkFile)
}

func appendProposal(path string, p storedProposal) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("proposal sink path is empty")
	}
	line, err := json.Marshal(p)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	proposalSinkMu.Lock()
	defer proposalSinkMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// readProposals parses the proposal sink (file order). Corrupt lines skipped;
// absent file yields an empty slice.
func readProposals(path string) ([]storedProposal, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []storedProposal{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []storedProposal
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var p storedProposal
		if err := json.Unmarshal(line, &p); err != nil {
			continue
		}
		out = append(out, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// surfaceExtractedWorkflows groups stored proposals by fingerprint, counts
// recurrence (distinct completed tasks), and returns them most-recurrent first.
// The latest stored spec/name/trigger for a fingerprint wins.
func surfaceExtractedWorkflows(path string) ([]ExtractedWorkflow, error) {
	props, err := readProposals(path)
	if err != nil {
		return nil, err
	}
	type agg struct {
		ew      ExtractedWorkflow
		taskSet map[string]bool
	}
	byFP := map[string]*agg{}
	var order []string
	for _, p := range props {
		fp := p.Fingerprint
		if fp == "" {
			fp = proposalFingerprint(p.Spec)
		}
		if fp == "" {
			continue
		}
		a, ok := byFP[fp]
		if !ok {
			a = &agg{ew: ExtractedWorkflow{Fingerprint: fp}, taskSet: map[string]bool{}}
			byFP[fp] = a
			order = append(order, fp)
		}
		// Latest record wins for display fields.
		a.ew.Name = p.Name
		a.ew.Confidence = p.Confidence
		a.ew.Trigger = p.Trigger
		a.ew.Spec = p.Spec
		if id := strings.TrimSpace(p.TaskID); id != "" && !a.taskSet[id] {
			a.taskSet[id] = true
			a.ew.TaskIDs = append(a.ew.TaskIDs, id)
		}
	}
	out := make([]ExtractedWorkflow, 0, len(order))
	for _, fp := range order {
		a := byFP[fp]
		a.ew.Recurrence = len(a.ew.TaskIDs)
		out = append(out, a.ew)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Recurrence != out[j].Recurrence {
			return out[i].Recurrence > out[j].Recurrence
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out, nil
}

// completedTaskSweepInterval is how often the workflow-detection sweep checks
// for newly completed tasks. Detection is a background proposal, not
// time-critical, so a coarse cadence keeps model spend low.
const completedTaskSweepInterval = 45 * time.Second

// seedExtractedFromCurrentTasks marks every ALREADY-completed task as extracted
// at boot, so the first sweep does not fire a burst of model calls over the
// office's entire history. Only tasks that complete AFTER boot are detected.
// Locks b.mu.
func (b *Broker) seedExtractedFromCurrentTasks() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.extractedTaskIDs == nil {
		b.extractedTaskIDs = map[string]bool{}
	}
	for i := range b.tasks {
		if taskStatusIsDone(b.tasks[i].status) {
			b.extractedTaskIDs[b.tasks[i].ID] = true
		}
	}
}

// taskStatusIsDone reports whether a task status counts as a completed outcome
// for workflow detection.
func taskStatusIsDone(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed", "complete":
		return true
	}
	return false
}

// sweepCompletedForExtraction is the path-independent completion detector: it
// scans task STATE rather than hooking one of WUPHF's several completion code
// paths (direct complete, PR-style review approval, legacy reconcile), so no
// completion can bypass it. A task newly in a done state fires the extractor
// once; a task that leaves done (reopen) is un-marked so a later re-completion
// re-fires. Returns the IDs it dispatched (for tests).
func (b *Broker) sweepCompletedForExtraction() []string {
	b.mu.Lock()
	if b.extractedTaskIDs == nil {
		b.extractedTaskIDs = map[string]bool{}
	}
	var toExtract []string
	live := map[string]bool{}
	for i := range b.tasks {
		id := b.tasks[i].ID
		done := taskStatusIsDone(b.tasks[i].status)
		live[id] = done
		if done && !b.extractedTaskIDs[id] {
			b.extractedTaskIDs[id] = true
			toExtract = append(toExtract, id)
		}
	}
	// Un-mark tasks that left the done state so reopen → re-complete re-fires.
	for id := range b.extractedTaskIDs {
		if !live[id] {
			delete(b.extractedTaskIDs, id)
		}
	}
	b.mu.Unlock()

	for _, id := range toExtract {
		go b.onTaskCompletedExtract(id)
	}
	return toExtract
}

// startWorkflowExtractionSweep seeds the dedup set with already-done tasks, then
// runs the completion sweep on a ticker until ctx is cancelled. Best-effort
// background detection; never blocks the office.
func (b *Broker) startWorkflowExtractionSweep(ctx context.Context) {
	b.seedExtractedFromCurrentTasks()
	ticker := time.NewTicker(completedTaskSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sweepCompletedForExtraction()
		}
	}
}

// onTaskCompletedExtract is the completion hook: extract a workflow from a just
// completed task and persist it if the model judged it real. Runs in its own
// goroutine (spawned from the locked mutation), so it blocks on b.mu only after
// the mutation releases it. Best-effort: detection must never affect the task.
func (b *Broker) onTaskCompletedExtract(taskID string) {
	prop, err := b.extractWorkflowForTask(taskID, brokerExtractor{ctx: context.Background()})
	if err != nil {
		log.Printf("workflow-extract: task %s failed: %v", taskID, err)
		return
	}
	if !prop.IsWorkflow || prop.Spec == nil {
		log.Printf("workflow-extract: task %s not a workflow: %s", taskID, prop.Reason)
		return
	}
	path := ProposalSinkPath()
	if path == "" {
		return
	}
	if err := appendProposal(path, storedProposal{
		Fingerprint: proposalFingerprint(prop.Spec),
		TaskID:      prop.TaskID,
		Name:        prop.Name,
		Confidence:  prop.Confidence,
		Trigger:     prop.Trigger,
		Spec:        prop.Spec,
	}); err != nil {
		log.Printf("workflow-extract: persist failed for task %s: %v", taskID, err)
		return
	}
	log.Printf("workflow-extract: spotted %q from completed task %s", prop.Name, taskID)
}

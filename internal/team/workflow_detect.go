package team

import (
	"sort"
	"strings"
)

// Workflow detection miner (T10). Reads the persisted per-turn tool manifests
// (event_sink.go) and clusters repeated task "shapes" into candidates a human
// can freeze into a workflow. Deterministic and read-only: same corpus in,
// same candidates out. Design: docs/specs/workflow-detection-positioning.md.
//
// v0 clustering contract: a task's shape is the ordered set of distinct tools
// it used (first-use order across its turns). Tasks with an identical (agent,
// shape) cluster. Exact match keeps v0 deterministic and explainable; fuzzy /
// threshold matching (Codex clustering-contract hardening) is a follow-up.

// DetectionCandidate is a repeated multi-step tool shape spotted across several
// of one agent's tasks. Downstream turns it into a "spotted a workflow" card
// and, on approval, a frozen skill.
type DetectionCandidate struct {
	Fingerprint string   `json:"fingerprint"` // stable shape identity (tools joined)
	Shape       []string `json:"shape"`       // ordered distinct tools the tasks share
	Agent       string   `json:"agent,omitempty"`
	TaskIDs     []string `json:"task_ids"` // matching tasks, oldest first
	Count       int      `json:"count"`    // len(TaskIDs): how often the shape recurred
}

// DetectOptions tunes the miner. The zero value uses sane defaults.
type DetectOptions struct {
	MinRepeats int // recurrences needed to surface a candidate (default 3)
	MinSteps   int // distinct tools needed to count as a workflow (default 2)
}

func (o DetectOptions) withDefaults() DetectOptions {
	if o.MinRepeats <= 0 {
		o.MinRepeats = 3
	}
	if o.MinSteps <= 0 {
		o.MinSteps = 2
	}
	return o
}

// taskShape reduces a task's turn manifests to its workflow shape: the ordered
// distinct tool names in first-use order across the task's turns. Order is the
// signal (draft -> route -> send -> track); repeat counts and turn boundaries
// are tolerated so retries and chattier turns still cluster.
func taskShape(turns []TurnManifest) []string {
	seen := make(map[string]bool)
	var shape []string
	for _, t := range turns {
		for _, tool := range t.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			shape = append(shape, name)
		}
	}
	return shape
}

// DetectWorkflows groups manifests by task, reduces each task to its shape, and
// returns candidates whose (agent, shape) recurred at least MinRepeats times
// with at least MinSteps distinct tools. Sorted most-repeated first; ties broken
// by fingerprint so output is fully deterministic.
func DetectWorkflows(manifests []TurnManifest, opts DetectOptions) []DetectionCandidate {
	opts = opts.withDefaults()

	// Group manifests by task, preserving first-seen task order for determinism.
	type taskGroup struct {
		agent string
		turns []TurnManifest
	}
	groups := make(map[string]*taskGroup)
	var taskOrder []string
	for _, m := range manifests {
		taskID := strings.TrimSpace(m.TaskID)
		if taskID == "" {
			continue
		}
		g, ok := groups[taskID]
		if !ok {
			g = &taskGroup{agent: strings.TrimSpace(m.Agent)}
			groups[taskID] = g
			taskOrder = append(taskOrder, taskID)
		}
		g.turns = append(g.turns, m)
	}

	// Cluster tasks by (agent, shape).
	type cluster struct {
		shape   []string
		agent   string
		taskIDs []string
	}
	clusters := make(map[string]*cluster)
	var clusterOrder []string
	for _, taskID := range taskOrder {
		g := groups[taskID]
		shape := taskShape(g.turns)
		if len(shape) < opts.MinSteps {
			continue
		}
		fingerprint := strings.Join(shape, ">")
		key := g.agent + "\x00" + fingerprint
		c, ok := clusters[key]
		if !ok {
			c = &cluster{shape: shape, agent: g.agent}
			clusters[key] = c
			clusterOrder = append(clusterOrder, key)
		}
		c.taskIDs = append(c.taskIDs, taskID)
	}

	out := make([]DetectionCandidate, 0, len(clusterOrder))
	for _, key := range clusterOrder {
		c := clusters[key]
		if len(c.taskIDs) < opts.MinRepeats {
			continue
		}
		out = append(out, DetectionCandidate{
			Fingerprint: strings.Join(c.shape, ">"),
			Shape:       c.shape,
			Agent:       c.agent,
			TaskIDs:     c.taskIDs,
			Count:       len(c.taskIDs),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

// DetectWorkflowsFromSink reads a persisted events.jsonl sink and runs detection
// over it — the end-to-end path the broker uses to surface candidates. An empty
// or absent sink yields no candidates and no error.
func DetectWorkflowsFromSink(path string, opts DetectOptions) ([]DetectionCandidate, error) {
	manifests, err := ReadTurnManifests(path)
	if err != nil {
		return nil, err
	}
	return DetectWorkflows(manifests, opts), nil
}

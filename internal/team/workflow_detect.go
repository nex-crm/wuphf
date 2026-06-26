package team

import (
	"encoding/json"
	"sort"
	"strings"
)

// manifestToolToken maps a recorded tool call to the token the detection
// substrate should remember for it. It exists to fix a structural blind spot:
// in the office, EVERY real integration action (Gmail fetch, Slack post,
// HubSpot upsert) is executed through a single generic proxy MCP tool,
// team_action_execute. Recording only that proxy NAME makes every integration
// task look identical and — because the proxy is office plumbing
// (isOrchestrationTool) — invisible to the miner, so no real workflow can ever
// surface. The actual operation lives in the call ARGUMENTS (action_id), not
// the tool name. So for the proxy we unwrap the action_id into a
// domain-meaningful token ("GMAIL_SEND_EMAIL" -> "gmail_send_email") that the
// miner keeps as a real step and whose verb (send/post/…) drives outcome
// detection. All other tools pass through unchanged. Unparseable or
// action-less proxy calls fall back to the raw name (filtered as before), so
// this only ever ADDS signal — it never invents a step.
func manifestToolToken(toolName, toolInput string) string {
	name := strings.TrimSpace(toolName)
	if !isActionProxyTool(name) {
		return name
	}
	var in struct {
		Platform string `json:"platform"`
		ActionID string `json:"action_id"`
	}
	if json.Unmarshal([]byte(toolInput), &in) == nil {
		if id := strings.TrimSpace(in.ActionID); id != "" {
			return strings.ToLower(id)
		}
		if p := strings.TrimSpace(in.Platform); p != "" {
			return strings.ToLower(p) + "_action"
		}
	}
	return name
}

// isActionProxyTool reports whether a tool name is the generic external-action
// proxy whose real operation lives in its arguments. Matched by suffix so the
// MCP server prefix (mcp__wuphf-office__) is tolerated.
func isActionProxyTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "team_action_execute" || strings.HasSuffix(n, "__team_action_execute")
}

// Workflow detection miner (T10). Reads the persisted per-turn tool manifests
// (event_sink.go) and clusters repeated task "shapes" into candidates a human
// can freeze into a workflow. Deterministic and read-only: same corpus in,
// same candidates out. Design: docs/specs/workflow-detection-positioning.md.
//
// v0 clustering contract: a task's shape is the ordered set of distinct tools
// it used (first-use order across its turns). Tasks with an identical (agent,
// shape) cluster. Exact match keeps v0 deterministic and explainable; fuzzy /
// threshold matching (Codex clustering-contract hardening) is a follow-up.

// DetectionCandidate is a multi-step tool shape worth freezing into a workflow.
// It surfaces either because the shape recurred across an agent's tasks OR
// because a single task ran it end-to-end to a final outcome (e.g. a send,
// post, or delivered artifact). Downstream turns it into a "spotted a workflow"
// card and, on approval, a frozen skill.
type DetectionCandidate struct {
	Fingerprint string   `json:"fingerprint"` // stable shape identity (tools joined)
	Shape       []string `json:"shape"`       // ordered distinct tools the tasks share
	Agent       string   `json:"agent,omitempty"`
	TaskIDs     []string `json:"task_ids"` // matching tasks, oldest first
	Count       int      `json:"count"`    // len(TaskIDs): how often the shape recurred
	// Outcome is the terminal outcome-producing tool that proves the run
	// finished something (the digest was sent, the referral tracked). Empty
	// when the candidate surfaced on recurrence alone with no recognized
	// outcome step. This is the "led to a final outcome" signal.
	Outcome string `json:"outcome,omitempty"`
}

// DetectOptions tunes the miner. The zero value uses sane defaults.
type DetectOptions struct {
	// MinRepeats is how many recurrences surface a candidate on recurrence
	// alone. Default 1: a single end-to-end run that reached a final outcome
	// is enough to suggest (see RecurrenceFloor for the no-outcome fallback).
	MinRepeats int
	MinSteps   int // distinct tools needed to count as a workflow (default 2)
	// RecurrenceFloor is the recurrence count at which a shape surfaces even
	// with no recognized outcome step. Default recurrenceFloor (3). Apps
	// detection lowers this to 2 because a read-mostly tool the agent rebuilt
	// twice is already app-worthy even though it never "sends" anything.
	RecurrenceFloor int
	// SingleRunRequiresExternalOutcome narrows what lets a SINGLE run surface
	// below the recurrence floor: when set, only a genuinely externalizing
	// outcome (send/post/email/publish/…) qualifies, not a workspace-internal
	// verb (create/update/save/record/report). Apps set this so one task that
	// merely "saved a view" or "updated a record" once does not nag — those
	// remain valid RECURRENCE signal, just not a single-run trigger. Default
	// false keeps the workflow lane's behavior (any outcome verb surfaces a run).
	SingleRunRequiresExternalOutcome bool
	// OrderInsensitive clusters runs that used the SAME set of work tools in a
	// different order together (fetch->score and score->fetch are the same
	// workflow). Default false keeps the workflow lane's exact-sequence contract;
	// apps set it because a read-mostly tool's step order rarely distinguishes
	// one workflow from another, and exact-order matching loses real recurrence.
	OrderInsensitive bool
	// CrossAgent clusters the same shape regardless of WHICH agent ran it, so a
	// workflow two different agents both perform surfaces as one candidate. For
	// apps this is a strong shared-tool signal (a tool any agent/human opens);
	// the reported Agent is blanked since the cluster spans agents. Default false
	// keeps per-agent clustering.
	CrossAgent bool
	// FuzzyToolTolerance merges clusters whose tool SETS differ by at most this
	// many tools (one added or dropped step), so real shape drift — an extra
	// confirm read, a skipped lookup — still recurs instead of splitting into
	// singletons. 0 (default) is exact. Apps use 1. Merging is greedy in
	// first-seen order, so it stays deterministic and conservative.
	FuzzyToolTolerance int
}

// recurrenceFloor is the default recurrence count at which a shape surfaces
// even with no recognized outcome step. Below it, a single-or-few-shot shape
// must end in an outcome to surface — that is what keeps opaque read-only loops
// from nagging while still catching "you just did this whole thing once"
// workflows. Tunable per consumer via DetectOptions.RecurrenceFloor.
const recurrenceFloor = 3

func (o DetectOptions) withDefaults() DetectOptions {
	if o.MinRepeats <= 0 {
		o.MinRepeats = 1
	}
	if o.MinSteps <= 0 {
		o.MinSteps = 2
	}
	if o.RecurrenceFloor <= 0 {
		o.RecurrenceFloor = recurrenceFloor
	}
	return o
}

// outcomeVerbs are the tool-name tokens that mark a step as delivering a final
// outcome — the workflow produced or sent something, rather than only reading.
// Matched per token (snake/camel/dotted segments), so GMAIL_SEND_EMAIL,
// slack_send, compose_digest, and referral_track all qualify.
var outcomeVerbs = map[string]bool{
	"send": true, "sent": true, "post": true, "posted": true,
	"deliver": true, "delivered": true, "email": true, "emailed": true,
	"notify": true, "notified": true, "publish": true, "published": true,
	"create": true, "created": true, "write": true, "wrote": true, "written": true,
	"update": true, "updated": true, "track": true, "tracked": true,
	"commit": true, "committed": true, "compose": true, "composed": true,
	"reply": true, "replied": true, "submit": true, "submitted": true,
	"schedule": true, "scheduled": true, "save": true, "saved": true,
	"upsert": true, "message": true, "dispatch": true, "dispatched": true,
	"report": true, "reported": true, "merge": true, "merged": true,
	"approve": true, "approved": true, "charge": true, "invoice": true,
	"book": true, "booked": true, "push": true, "pushed": true,
	"upload": true, "uploaded": true, "record": true, "recorded": true,
	"digest": true,
}

// tokenizeTool splits a tool name into lowercase alphanumeric segments across
// snake_case, kebab-case, dotted, and camelCase boundaries so each word can be
// matched against outcomeVerbs.
func tokenizeTool(name string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	var prev rune
	for _, r := range name {
		switch {
		case r == '_' || r == '-' || r == '.' || r == '/' || r == ' ' || r == ':':
			flush()
		case r >= 'A' && r <= 'Z' && prev >= 'a' && prev <= 'z':
			flush()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
		prev = r
	}
	flush()
	return tokens
}

// externalizingVerbs are the subset of outcomeVerbs that leave the workspace —
// the run actually delivered something to a third party (an email, a Slack post,
// a published doc), as opposed to a workspace-internal write (create/update/
// save/record/report). A SINGLE run only justifies a suggestion on its own when
// it externalized; internal writes are far weaker single-run signal (a chatty
// task that "saved a view" once is not a workflow), though they remain valid
// RECURRENCE signal. See DetectOptions.SingleRunRequiresExternalOutcome.
var externalizingVerbs = map[string]bool{
	"send": true, "sent": true, "post": true, "posted": true,
	"deliver": true, "delivered": true, "email": true, "emailed": true,
	"notify": true, "notified": true, "publish": true, "published": true,
	"reply": true, "replied": true, "submit": true, "submitted": true,
	"dispatch": true, "dispatched": true, "message": true,
	"charge": true, "invoice": true, "book": true, "booked": true,
	"upload": true, "uploaded": true,
}

// terminalOutcome reports the outcome-producing tool a shape reached, scanning
// from the end so a confirming read after the send (send -> log) still counts.
// Returns the matched tool and true when the shape led to a final outcome.
func terminalOutcome(shape []string) (string, bool) {
	return terminalOutcomeIn(shape, outcomeVerbs)
}

// terminalExternalizingOutcome is terminalOutcome restricted to the
// externalizing subset — the gate for surfacing a single run.
func terminalExternalizingOutcome(shape []string) (string, bool) {
	return terminalOutcomeIn(shape, externalizingVerbs)
}

func terminalOutcomeIn(shape []string, verbs map[string]bool) (string, bool) {
	for i := len(shape) - 1; i >= 0; i-- {
		for _, tok := range tokenizeTool(shape[i]) {
			if verbs[tok] {
				return shape[i], true
			}
		}
	}
	return "", false
}

// orchestrationTools are the agent's own plumbing — file/search/shell harness
// tools and the office's internal coordination MCP. They are HOW an agent works,
// not WHAT the workflow does, so they must not count as workflow steps; left in,
// every chatty turn looks like a "workflow" of Bash -> ToolSearch -> team_task.
var orchestrationTools = map[string]bool{
	"bash": true, "toolsearch": true, "read": true, "write": true,
	"edit": true, "multiedit": true, "grep": true, "glob": true, "ls": true,
	"task": true, "webfetch": true, "websearch": true, "notebookedit": true,
	"croncreate": true, "crondelete": true, "cronlist": true,
	"taskcreate": true, "taskupdate": true, "tasklist": true, "taskget": true,
	"taskstop": true, "taskoutput": true, "sendmessage": true, "monitor": true,
	"enterplanmode": true, "exitplanmode": true, "exit_plan_mode": true,
}

// isOrchestrationTool reports whether a tool name is agent plumbing rather than
// workflow work. The office's own coordination MCP (mcp__wuphf-office__*) is
// always plumbing — the agent talking to its own broker. Other tools match the
// orchestrationTools denylist by exact (lowercased) name.
func isOrchestrationTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "mcp__wuphf-office__") {
		return true
	}
	return orchestrationTools[n]
}

// taskShape reduces a task's turn manifests to its workflow shape: the ordered
// distinct WORK tools in first-use order across the task's turns. Order is the
// signal (draft -> route -> send -> track); repeat counts and turn boundaries
// are tolerated so retries and chattier turns still cluster. Orchestration
// plumbing (isOrchestrationTool) is dropped so a shape reflects domain work.
func taskShape(turns []TurnManifest) []string {
	seen := make(map[string]bool)
	var shape []string
	for _, t := range turns {
		for _, tool := range t.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" || seen[name] || isOrchestrationTool(name) {
				continue
			}
			seen[name] = true
			shape = append(shape, name)
		}
	}
	return shape
}

// detectCluster is a group of tasks the miner judged to be the same workflow:
// a representative shape (first member's first-use order), the agent that ran it
// (the first member's, blanked downstream for cross-agent clusters), and the
// member task IDs.
type detectCluster struct {
	shape   []string
	agent   string
	taskIDs []string
}

// toolSetSymmetricDiff counts the tools that appear in exactly one of the two
// shapes (treated as sets) — the number of single-tool edits between them.
func toolSetSymmetricDiff(a, b []string) int {
	in := make(map[string]int, len(a)+len(b))
	for _, t := range a {
		in[t] |= 1
	}
	for _, t := range b {
		in[t] |= 2
	}
	diff := 0
	for _, bits := range in {
		if bits != 3 { // present in only one set
			diff++
		}
	}
	return diff
}

// mergeNearDuplicateClusters greedily folds each cluster into the first earlier
// cluster whose shape differs by at most tolerance tools (and, unless crossAgent,
// shares its agent). Greedy + first-seen order keeps it deterministic and
// conservative — drift accumulates toward an anchor rather than chaining
// unrelated shapes together. The anchor keeps its representative shape; merged
// members contribute their task IDs (recurrence count).
func mergeNearDuplicateClusters(clusters []*detectCluster, tolerance int, crossAgent bool) []*detectCluster {
	kept := make([]*detectCluster, 0, len(clusters))
	for _, c := range clusters {
		merged := false
		for _, k := range kept {
			if !crossAgent && k.agent != c.agent {
				continue
			}
			if toolSetSymmetricDiff(k.shape, c.shape) <= tolerance {
				k.taskIDs = append(k.taskIDs, c.taskIDs...)
				merged = true
				break
			}
		}
		if !merged {
			kept = append(kept, c)
		}
	}
	return kept
}

// DetectWorkflows groups manifests by task, reduces each task to its shape, and
// returns candidates with at least MinSteps distinct tools that either (a) ran
// end-to-end to a final outcome (even once) or (b) recurred at least
// RecurrenceFloor times with no recognized outcome step. Sorted most-repeated
// first; ties broken by fingerprint so output is fully deterministic.
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
	clusters := make(map[string]*detectCluster)
	var clusterOrder []string
	for _, taskID := range taskOrder {
		g := groups[taskID]
		shape := taskShape(g.turns)
		if len(shape) < opts.MinSteps {
			continue
		}
		// The cluster key controls what counts as "the same workflow". By default
		// it is the exact ordered shape; OrderInsensitive keys on the sorted tool
		// set so reordered runs cluster while the stored shape stays first-use
		// order for display.
		keyTools := shape
		if opts.OrderInsensitive {
			keyTools = append([]string(nil), shape...)
			sort.Strings(keyTools)
		}
		// CrossAgent drops the agent from the key so the same shape clusters
		// regardless of which agent ran it.
		agentKey := g.agent
		if opts.CrossAgent {
			agentKey = ""
		}
		key := agentKey + "\x00" + strings.Join(keyTools, ">")
		c, ok := clusters[key]
		if !ok {
			c = &detectCluster{shape: shape, agent: g.agent}
			clusters[key] = c
			clusterOrder = append(clusterOrder, key)
		}
		c.taskIDs = append(c.taskIDs, taskID)
	}

	// Assemble clusters in first-seen order, then optionally merge near-duplicate
	// shapes (FuzzyToolTolerance) so one-tool drift recurs instead of splitting.
	final := make([]*detectCluster, 0, len(clusterOrder))
	for _, key := range clusterOrder {
		final = append(final, clusters[key])
	}
	if opts.FuzzyToolTolerance > 0 {
		final = mergeNearDuplicateClusters(final, opts.FuzzyToolTolerance, opts.CrossAgent)
	}

	out := make([]DetectionCandidate, 0, len(final))
	for _, c := range final {
		count := len(c.taskIDs)
		if count < opts.MinRepeats {
			continue
		}
		outcome, hasOutcome := terminalOutcome(c.shape)
		// Surface a single end-to-end run only when it reached a final outcome.
		// A shape with no recognized outcome step still needs recurrence
		// (RecurrenceFloor) before it is worth suggesting. When the consumer asks
		// for it, a SINGLE run must have EXTERNALIZED (send/post/email/…), not
		// just written something internal, to surface on its own.
		surfacesSingleRun := hasOutcome
		if opts.SingleRunRequiresExternalOutcome {
			_, surfacesSingleRun = terminalExternalizingOutcome(c.shape)
		}
		if !surfacesSingleRun && count < opts.RecurrenceFloor {
			continue
		}
		// A cross-agent cluster spans agents, so it claims no single one.
		agent := c.agent
		if opts.CrossAgent {
			agent = ""
		}
		out = append(out, DetectionCandidate{
			Fingerprint: strings.Join(c.shape, ">"),
			Shape:       c.shape,
			Agent:       agent,
			TaskIDs:     c.taskIDs,
			Count:       count,
			Outcome:     outcome,
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

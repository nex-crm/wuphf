// Package workflow is the workflow-press kernel: the canonical workflow-spec
// contract, a deterministic runner that executes it, and a shipcheck that
// mechanically proves a spec before it ships. Discovery (the detection miner)
// proposes; a human reviews a spec into a contract; the runner executes it
// deterministically; shipcheck proves it; improvement overlays heal it.
//
// Design: docs/specs/workflow-detection-positioning.md section 6C.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Spec is the canonical workflow contract (workflow-spec.json). It is the IR
// that discovery is frozen into and that generation, verification, and
// improvement all read from. Nothing downstream reads raw discovery — only this.
type Spec struct {
	Version  string `json:"version"`
	ID       string `json:"id"`
	Goal     string `json:"goal"`
	Operator string `json:"operator"`

	Entities []Entity `json:"entities,omitempty"`

	// State machine. Initial is the entry state; Terminal lists end states.
	States      []State      `json:"states"`
	Initial     string       `json:"initial"`
	Terminal    []string     `json:"terminal,omitempty"`
	Events      []Event      `json:"events"`
	Transitions []Transition `json:"transitions"`
	Actions     []Action     `json:"actions"`

	Exceptions []Exception `json:"exceptions,omitempty"`
	SLAs       []SLA       `json:"slas,omitempty"`

	// Scenarios are the verification fixtures shipcheck replays.
	Scenarios          []Scenario `json:"scenarios"`
	ImprovementSignals []string   `json:"improvement_signals,omitempty"`
}

type Entity struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields,omitempty"`
}

type State struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

type Event struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// Transition advances From->To when On fires and Guard passes, running Actions.
type Transition struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	On      string   `json:"on"`              // event id
	Guard   string   `json:"guard,omitempty"` // tiny expr, see guard.go
	Actions []string `json:"actions,omitempty"`
}

// ActionKind classifies how an action runs. Deterministic actions are generated
// code; llm/external actions are gated hooks the runner calls out to.
type ActionKind string

const (
	ActionDeterministic ActionKind = "deterministic"
	ActionLLM           ActionKind = "llm"
	ActionExternal      ActionKind = "external"
)

type Action struct {
	ID          string     `json:"id"`
	Kind        ActionKind `json:"kind"`
	Description string     `json:"description,omitempty"`
	// Platform + ActionID target a real external integration (e.g. slack +
	// SLACK_SEND_MESSAGE). Set on external actions during review; the runtime
	// gates the send through them (classify -> grant -> Composio execute). Empty
	// on an auto-drafted action, which the runtime records as intent only.
	Platform string `json:"platform,omitempty"`
	ActionID string `json:"action_id,omitempty"`
}

type Exception struct {
	ID     string `json:"id"`
	When   string `json:"when"`
	Handle string `json:"handle"`
}

type SLA struct {
	State         string `json:"state"`
	MaxAgeSeconds int    `json:"max_age_seconds"`
}

// Scenario is one verification fixture: an ordered list of input events and the
// state path + actions the runner must produce. Shipcheck replays these.
type Scenario struct {
	Name          string          `json:"name"`
	Events        []ScenarioEvent `json:"events"`
	ExpectStates  []string        `json:"expect_states"`
	ExpectActions []string        `json:"expect_actions,omitempty"`
}

type ScenarioEvent struct {
	Event string         `json:"event"`
	Data  map[string]any `json:"data,omitempty"`
	// DedupKey makes an event idempotent; a repeat with the same key is a no-op.
	DedupKey string `json:"dedup_key,omitempty"`
}

// LoadSpec reads and validates a workflow-spec.json from disk.
func LoadSpec(path string) (*Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %q: %w", path, err)
	}
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse spec %q: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid spec %q: %w", path, err)
	}
	return &s, nil
}

// Validate checks the contract is internally consistent: referenced states,
// events, and actions all exist, and the initial state is defined. A spec that
// fails this can never ship.
func (s *Spec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(s.Initial) == "" {
		return fmt.Errorf("initial state is required")
	}
	states := indexIDs(len(s.States), func(i int) string { return s.States[i].ID })
	events := indexIDs(len(s.Events), func(i int) string { return s.Events[i].ID })
	actions := indexIDs(len(s.Actions), func(i int) string { return s.Actions[i].ID })

	if !states[s.Initial] {
		return fmt.Errorf("initial state %q not in states", s.Initial)
	}
	for _, t := range s.Terminal {
		if !states[t] {
			return fmt.Errorf("terminal state %q not in states", t)
		}
	}
	for i, t := range s.Transitions {
		if !states[t.From] {
			return fmt.Errorf("transition %d: from %q not a state", i, t.From)
		}
		if !states[t.To] {
			return fmt.Errorf("transition %d: to %q not a state", i, t.To)
		}
		if !events[t.On] {
			return fmt.Errorf("transition %d: on %q not an event", i, t.On)
		}
		for _, a := range t.Actions {
			if !actions[a] {
				return fmt.Errorf("transition %d: action %q not defined", i, a)
			}
		}
		if err := validateGuard(t.Guard); err != nil {
			return fmt.Errorf("transition %d: guard %q: %w", i, t.Guard, err)
		}
	}
	for i, a := range s.Actions {
		switch a.Kind {
		case ActionDeterministic, ActionLLM, ActionExternal:
		default:
			return fmt.Errorf("action %d (%s): unknown kind %q", i, a.ID, a.Kind)
		}
	}
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("a shippable spec must declare at least one verification scenario")
	}
	return nil
}

// reachableStates returns the set of states reachable from Initial by following
// transitions. Used by shipcheck transition-coverage.
func (s *Spec) reachableStates() map[string]bool {
	adj := map[string][]string{}
	for _, t := range s.Transitions {
		adj[t.From] = append(adj[t.From], t.To)
	}
	seen := map[string]bool{s.Initial: true}
	stack := []string{s.Initial}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, nxt := range adj[cur] {
			if !seen[nxt] {
				seen[nxt] = true
				stack = append(stack, nxt)
			}
		}
	}
	return seen
}

func (s *Spec) stateIDs() []string {
	out := make([]string, len(s.States))
	for i := range s.States {
		out[i] = s.States[i].ID
	}
	sort.Strings(out)
	return out
}

func indexIDs(n int, id func(int) string) map[string]bool {
	m := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		m[id(i)] = true
	}
	return m
}

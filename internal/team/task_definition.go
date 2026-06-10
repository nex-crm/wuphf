package team

// task_definition.go — R4 structured task definition (docs/specs/core-loop.md,
// Core Loop step 2: "Define the task clearly — Goal · Deliverables (and
// required format) · Success criteria").
//
// The definition is the intake contract the owner executes against. It is set
// by the CEO (or the human) via team_task action=define after the R4 intake
// pass: infer what you can from the request + retrievable context, run ONE
// batched human_interview for genuine gaps (including tool/context access →
// AccessNeeded), then define BEFORE staffing the task. It replaces the R2
// spec-document ceremony with structured fields on the task itself.
//
// Wire compatibility: TaskDefinition rides on teamTask under the single
// additive "definition" key (broker_types.go teamTaskWire + Marshal/Unmarshal
// enumerate it). Old persisted state loads with a nil Definition; nothing
// downstream requires it.

import (
	"fmt"
	"strings"
)

// TaskDeliverable is one concrete artifact the task must produce, with the
// exact format the human expects (e.g. name="competitor brief",
// format="markdown table in the wiki").
type TaskDeliverable struct {
	Name   string `json:"name"`
	Format string `json:"format,omitempty"`
}

// TaskDefinition is the structured intake contract on a task. Goal is the
// only required field; SuccessCriteria map onto the existing machine
// verification gate (task_verification.go) where checkable — the CEO passes
// verification_kind/spec/required alongside the define call for those, the
// broker never parses criteria text into commands.
type TaskDefinition struct {
	Goal            string            `json:"goal"`
	Deliverables    []TaskDeliverable `json:"deliverables,omitempty"`
	SuccessCriteria []string          `json:"success_criteria,omitempty"`
	AccessNeeded    []string          `json:"access_needed,omitempty"`
	// DefinedAt is the RFC3339 timestamp stamped by the broker when the
	// definition was set/updated (callers cannot forge it).
	DefinedAt string `json:"defined_at,omitempty"`
}

// normalizeTaskDefinition validates and canonicalizes a definition from the
// wire. Goal is required; success-criteria entries must be non-empty.
// DefinedAt is always stamped with the broker's now, never trusted from the
// caller. Returns a fresh struct so the caller-owned input is never aliased
// into broker state.
func normalizeTaskDefinition(in *TaskDefinition, now string) (*TaskDefinition, error) {
	if in == nil {
		return nil, fmt.Errorf("definition required: pass goal (plus deliverables/success_criteria/access_needed)")
	}
	goal := strings.TrimSpace(in.Goal)
	if goal == "" {
		return nil, fmt.Errorf("definition goal is required")
	}
	out := &TaskDefinition{Goal: goal, DefinedAt: now}
	for i, d := range in.Deliverables {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return nil, fmt.Errorf("definition deliverables[%d].name is empty", i)
		}
		out.Deliverables = append(out.Deliverables, TaskDeliverable{
			Name:   name,
			Format: strings.TrimSpace(d.Format),
		})
	}
	for i, c := range in.SuccessCriteria {
		c = strings.TrimSpace(c)
		if c == "" {
			return nil, fmt.Errorf("definition success_criteria[%d] is empty", i)
		}
		out.SuccessCriteria = append(out.SuccessCriteria, c)
	}
	for _, a := range in.AccessNeeded {
		if a = strings.TrimSpace(a); a != "" {
			out.AccessNeeded = append(out.AccessNeeded, a)
		}
	}
	return out, nil
}

// taskDefinitionPacketLines renders the definition for work packets
// (notification_context.go). The caller prepends its own header line; these
// lines are indented to sit under it. Nil/empty definitions render nothing.
func taskDefinitionPacketLines(def *TaskDefinition) []string {
	if def == nil || strings.TrimSpace(def.Goal) == "" {
		return nil
	}
	// Clip each rendered field like Details is clipped — definition text is
	// the same trust/size class (review note: avoid unbounded packet bloat).
	lines := []string{"  Goal: " + truncate(strings.TrimSpace(def.Goal), 1000)}
	if len(def.Deliverables) > 0 {
		parts := make([]string, 0, len(def.Deliverables))
		for _, d := range def.Deliverables {
			if strings.TrimSpace(d.Format) != "" {
				parts = append(parts, fmt.Sprintf("%s (format: %s)", d.Name, d.Format))
			} else {
				parts = append(parts, d.Name)
			}
		}
		lines = append(lines, "  Deliverables: "+truncate(strings.Join(parts, "; "), 1000))
	}
	if len(def.SuccessCriteria) > 0 {
		lines = append(lines, "  Success criteria:")
		for i, c := range def.SuccessCriteria {
			lines = append(lines, fmt.Sprintf("    %d. %s", i+1, truncate(c, 600)))
		}
	}
	if len(def.AccessNeeded) > 0 {
		lines = append(lines, "  Access needed: "+truncate(strings.Join(def.AccessNeeded, "; "), 600))
	}
	return lines
}

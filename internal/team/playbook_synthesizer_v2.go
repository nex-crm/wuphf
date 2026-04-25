package team

// playbook_synthesizer_v2.go wires the Slice 2 Thread C cluster-aware prompt
// into PlaybookSynthesizer. Additive only — when PlaybookSynthesizerConfig
// leaves ClusterSource nil, the v1 code path in playbook_synthesizer.go is
// unchanged and hot.
//
// The v2 prompt lives in prompts/synthesis_playbook_v2.tmpl. It is a strict
// superset of v1: every v1 instruction is preserved, plus a new
// "Reinforced patterns across entities" input section and a rule telling the
// model to render a "## Patterns across entities" block under the learnings
// section when clusters are provided.
//
// The v2 builder is package-private and referenced only from
// PlaybookSynthesizer.synthesize when ClusterSource is wired.

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/synthesis_playbook_v2.tmpl
var synthesisPlaybookV2Tmpl string

// playbookSynthV2 is the parsed v2 template. Parsed once at package init so
// the hot path does not pay template parse cost per synthesis.
var playbookSynthV2 = func() *template.Template {
	funcs := template.FuncMap{
		"add":       func(a, b int) int { return a + b },
		"trimSpace": strings.TrimSpace,
		"oneLine":   oneLine,
		"rfc3339":   func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"joinEntities": func(ents []string) string {
			// Render as a comma-joined flat list. Kept internal so the prompt
			// output stays stable regardless of slice length.
			return strings.Join(ents, ", ")
		},
	}
	t, err := template.New("synthesis_playbook_v2").Funcs(funcs).Parse(synthesisPlaybookV2Tmpl)
	if err != nil {
		// Embedded template is build-time validated; a parse error is a bug.
		panic(fmt.Sprintf("playbook synth v2 template: %v", err))
	}
	return t
}()

// playbookSynthV2Vars holds everything the v2 template needs to render.
type playbookSynthV2Vars struct {
	Source        string
	MaxExecs      int
	Executions    []Execution
	Clusters      []FactCluster
	LearningsHead string
}

// buildPlaybookSynthUserPromptV2 renders the Thread C user prompt. Caller
// guarantees executions are newest-first and already windowed.
//
// When clusters is empty the template still renders, with an explicit "no
// clusters detected" marker and the rule-4a patterns section instructed to
// be OMITTED — the prompt stays honest even when the clustering scan comes
// up empty.
func buildPlaybookSynthUserPromptV2(source string, execs []Execution, clusters []FactCluster) (string, error) {
	vars := playbookSynthV2Vars{
		Source:        source,
		MaxExecs:      MaxExecutionsForPrompt,
		Executions:    execs,
		Clusters:      clusters,
		LearningsHead: WhatWeveLearnedHeading,
	}
	var b strings.Builder
	if err := playbookSynthV2.Execute(&b, vars); err != nil {
		return "", fmt.Errorf("render synthesis_playbook_v2: %w", err)
	}
	return b.String(), nil
}

// collectReinforcedClusters queries the configured ClusterSource for the
// current cross-entity pattern set. Returns (nil, nil) when no ClusterSource
// is wired — that is the signal to the synthesizer to fall back to the v1
// prompt path.
//
// minEntities defaults to DefaultClusterMinEntities when non-positive. The
// default mirrors the Slice 2 plan Thread C bullet 1 (≥3 entities); tests
// and operators can tune it down for small wikis.
func (s *PlaybookSynthesizer) collectReinforcedClusters(ctx context.Context) ([]FactCluster, error) {
	if s == nil || s.cfg.ClusterSource == nil {
		return nil, nil
	}
	minEntities := s.cfg.ClusterMinEntities
	if minEntities <= 0 {
		minEntities = DefaultClusterMinEntities
	}
	// Pass MaxClustersForPrompt as the topN cap so the cluster function
	// short-circuits after sorting and we avoid materialising the long tail
	// the synthesizer would slice off seconds later. The semantic
	// equivalent of the previous post-slice truncation; the allocation
	// savings matter once the corpus has dozens of reinforced patterns.
	clusters, err := clusterReinforcedFacts(ctx, s.cfg.ClusterSource, "", minEntities, MaxClustersForPrompt)
	if err != nil {
		return nil, fmt.Errorf("collect clusters: %w", err)
	}
	return clusters, nil
}

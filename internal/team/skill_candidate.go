package team

// skill_candidate.go is the shared signal type for Stage B. Multiple signal
// sources (notebook scanner in notebook_signal_scanner.go, self-heal incident
// scanner in self_heal_signal.go) emit SkillCandidate values; the Stage B
// synthesizer (PR 2-B) consumes them and decides whether to materialise a
// proposed skill via writeSkillProposalLocked.
//
// A SkillCandidate is NOT a skill yet — it is "something here might be worth
// codifying as a skill". The synthesizer is the LLM-gated step that turns
// a candidate into a real proposal.

import "time"

// SkillCandidate is the shared signal envelope produced by Stage B signal
// sources and consumed by the SkillSynthesizer (PR 2-B).
type SkillCandidate struct {
	// Source identifies the signal type for downstream debugging + telemetry.
	Source SkillCandidateSource

	// SuggestedName is a kebab-case slug hint. The synthesizer may override.
	SuggestedName string

	// SuggestedDescription is a one-line trigger phrase hint. The synthesizer
	// may override.
	SuggestedDescription string

	// Excerpts are verbatim text snippets from the source (notebook entries,
	// incident task details). The synthesizer cites these as motivation.
	Excerpts []SkillCandidateExcerpt

	// RelatedWikiPaths are the team/ wiki articles related to this candidate
	// (resolved via the existing wiki retrieval API). The synthesizer reads
	// these for grounded synthesis.
	RelatedWikiPaths []string

	// SignalCount is how many independent signals contributed to this
	// candidate (e.g. 3 agents wrote about the same topic).
	SignalCount int

	// OwnerAgents lists the agent slugs that should default-own the
	// synthesized skill. Self-heal incidents seed this with [task.Owner];
	// notebook clusters seed it with the dedup union of the contributing
	// agents. The synthesizer threads this onto the resulting teamSkill.
	// Nil means lead-routable.
	OwnerAgents []string

	// FirstSeenAt and LastSeenAt frame the time window of the signal cluster.
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// SkillCandidateSource tags the upstream emitter of a candidate so consumers
// can branch on origin.
type SkillCandidateSource string

const (
	// SourceNotebookCluster indicates the candidate came from cross-agent
	// notebook clustering (notebook_signal_scanner.go).
	SourceNotebookCluster SkillCandidateSource = "notebook_cluster"

	// SourceSelfHealResolved indicates the candidate came from a resolved
	// self-heal incident task (self_heal_signal.go).
	SourceSelfHealResolved SkillCandidateSource = "self_heal_resolved"
)

// SkillCandidateExcerpt is a single verbatim citation that motivated the
// candidate. The synthesizer may inline it as evidence in the proposal body.
type SkillCandidateExcerpt struct {
	// Path is the source location: a wiki-relative file path for notebook
	// excerpts, a task ID for self-heal excerpts.
	Path string
	// Snippet is the verbatim text excerpted from the source.
	Snippet string
	// Author is the agent slug for notebook excerpts, the task owner for
	// self-heal excerpts, or "human" for human-authored snippets.
	Author string
	// CreatedAt is the source's timestamp (notebook mtime or task UpdatedAt).
	CreatedAt time.Time
}

package team

// skill_synth_provider.go is the LLM provider wrapper for Stage B skill
// synthesis. It assembles the system prompt + candidate context + related
// wiki excerpts, calls the broker's LLM provider, and parses the JSON
// response into a SkillFrontmatter + body pair.
//
// Per the design "Eng Review Revisions" Stage B section: the live LLM
// round-trip reuses the same provider plumbing as PR 1a-B's defaultLLMProvider
// (today a stub that returns is_skill=false). The synthesis-specific user
// prompt suffix lives here; the system prompt is shared with the Stage A
// scanner.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// stageBLLMProvider is the small interface SkillSynthesizer uses to ask an
// LLM to synthesize a skill from a SkillCandidate. Defined where it is
// consumed per the "accept interfaces, return structs" idiom.
type stageBLLMProvider interface {
	SynthesizeSkill(ctx context.Context, candidate SkillCandidate, wikiContext string) (SkillFrontmatter, string, error)
}

// defaultStageBLLMProvider implements stageBLLMProvider using the broker's
// existing LLM provider abstraction. The live model wiring is deferred —
// today this returns is_skill=false to match the Stage A stub. The plumbing
// (prompt assembly, response parsing) is wired so the live wiring is a
// drop-in replacement.
type defaultStageBLLMProvider struct {
	broker *Broker

	// systemPromptCache holds the lazy-loaded system prompt so we only read
	// the wiki file once per process. atomic.Value carries *string for the
	// lock-free fast path; the load uses a mutex to avoid duplicate reads.
	systemPromptCache atomic.Value // *string
	loadMu            sync.Mutex
	loadErr           error
}

// NewDefaultStageBLLMProvider constructs a provider bound to broker b. The
// system prompt is loaded lazily on first SynthesizeSkill call so test
// brokers without a wiki worker pay no startup cost.
func NewDefaultStageBLLMProvider(b *Broker) *defaultStageBLLMProvider {
	return &defaultStageBLLMProvider{broker: b}
}

// SynthesizeSkill is the canonical entry point. It builds the system + user
// prompts, sends them to the LLM, and decodes the JSON response into a
// SkillFrontmatter + body. Today it returns ("not-a-skill") so Stage B is a
// no-op; live wiring lands when the broker LLM provider abstraction is
// finalised.
//
// TODO(stage-b): wire to the real LLM provider abstraction (see
// internal/team/headless_*.go for patterns). Until then the provider
// surfaces the correctly-assembled prompts but performs no round-trip.
func (p *defaultStageBLLMProvider) SynthesizeSkill(ctx context.Context, cand SkillCandidate, wikiContext string) (SkillFrontmatter, string, error) {
	if _, err := p.systemPrompt(); err != nil {
		return SkillFrontmatter{}, "", fmt.Errorf("stage_b_synth: load system prompt: %w", err)
	}
	// Build the user prompt so any wiring + tests can validate the structure.
	// Discarded today; the live provider will send it.
	_ = buildStageBSynthUserPrompt(cand, wikiContext)
	if ctx.Err() != nil {
		return SkillFrontmatter{}, "", fmt.Errorf("stage_b_synth: context: %w", ctx.Err())
	}
	return SkillFrontmatter{}, "", errors.New("synth: candidate rejected by LLM as not-a-skill")
}

// systemPrompt returns the synthesizer system prompt, loading it from the
// wiki on first use and falling back to the embedded default when the wiki
// file is missing. The prompt is the SAME skill-creator.md the Stage A
// scanner uses; the synthesis-specific instructions live in the user
// prompt suffix.
func (p *defaultStageBLLMProvider) systemPrompt() (string, error) {
	if v, ok := p.systemPromptCache.Load().(*string); ok && v != nil {
		return *v, nil
	}
	p.loadMu.Lock()
	defer p.loadMu.Unlock()
	if v, ok := p.systemPromptCache.Load().(*string); ok && v != nil {
		return *v, nil
	}
	if p.loadErr != nil {
		return "", p.loadErr
	}
	prompt := p.loadSystemPromptFromWiki()
	p.systemPromptCache.Store(&prompt)
	return prompt, nil
}

// loadSystemPromptFromWiki resolves <wikiRoot>/team/skills/.system/skill-creator.md
// and reads it. Falls back to the embedded default whenever the path can't
// be resolved or the file is missing/empty.
func (p *defaultStageBLLMProvider) loadSystemPromptFromWiki() string {
	if p.broker == nil {
		return defaultSkillCreatorPromptEmbedded
	}
	p.broker.mu.Lock()
	worker := p.broker.wikiWorker
	p.broker.mu.Unlock()
	if worker == nil {
		return defaultSkillCreatorPromptEmbedded
	}
	repo := worker.Repo()
	if repo == nil {
		return defaultSkillCreatorPromptEmbedded
	}
	path := filepath.Join(repo.Root(), "team", "skills", ".system", "skill-creator.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return defaultSkillCreatorPromptEmbedded
	}
	if strings.TrimSpace(string(raw)) == "" {
		return defaultSkillCreatorPromptEmbedded
	}
	return string(raw)
}

// buildStageBSynthUserPrompt assembles the synthesis-specific user message.
// Exposed at package scope so tests and the (future) live provider share the
// exact prompt structure.
func buildStageBSynthUserPrompt(cand SkillCandidate, wikiContext string) string {
	var b strings.Builder
	b.WriteString("## Synthesis context\n\n")
	b.WriteString("The user has identified a CANDIDATE for a new skill based on signals from ")
	b.WriteString(string(cand.Source))
	b.WriteString(fmt.Sprintf(" (%d signals).\n", cand.SignalCount))
	b.WriteString("Suggested name: ")
	b.WriteString(strings.TrimSpace(cand.SuggestedName))
	b.WriteString("\nSuggested description: ")
	b.WriteString(strings.TrimSpace(cand.SuggestedDescription))
	b.WriteString("\n\nCandidate excerpts:\n")
	for _, ex := range cand.Excerpts {
		fmt.Fprintf(&b, "- [%s] %s — %s\n",
			strings.TrimSpace(ex.Author),
			strings.TrimSpace(ex.Path),
			truncateForPrompt(ex.Snippet, 200))
	}
	b.WriteString("\nRelated wiki context (for grounding):\n")
	if strings.TrimSpace(wikiContext) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(wikiContext)
		b.WriteString("\n")
	}
	b.WriteString("\nSynthesize a reusable skill grounded in the wiki context, citing the excerpts as motivation.\n")
	b.WriteString(`Respond with JSON: {is_skill: true, name: "kebab-slug", description: "one line", body: "markdown body"}.`)
	b.WriteString("\nIf you don't think this is a real skill, respond {is_skill: false}.\n")
	return b.String()
}

// truncateForPrompt clamps a snippet so candidate excerpts don't blow up the
// LLM context. We trim to limit runes and append an ellipsis when truncated.
func truncateForPrompt(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

package team

// skill_frontmatter.go implements the Anthropic Agent Skills frontmatter schema
// used by the wiki-skill-compile pipeline. Canonical format matches the
// anthropics/skills spec so compiled skills are publishable to external hubs
// without re-formatting. WUPHF-specific provenance lives under metadata.wuphf.

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillFrontmatter represents the top-level Anthropic Agent Skills frontmatter.
// Name and Description are mandatory; all other fields are optional.
type SkillFrontmatter struct {
	// Name is the skill slug (e.g. "daily-digest"). Mandatory.
	Name string `yaml:"name"`
	// Description is a one-line summary. Mandatory. The LLM router reads this.
	Description string `yaml:"description"`
	// Version is a semver string (e.g. "1.0.0"). Populated on every regeneration.
	Version string `yaml:"version,omitempty"`
	// License defaults to MIT unless the workspace overrides it.
	License string `yaml:"license,omitempty"`
	// Metadata contains WUPHF-specific provenance under the wuphf namespace.
	Metadata SkillMetadata `yaml:"metadata,omitempty"`
}

// SkillMetadata is the top-level metadata namespace. Only the wuphf sub-key
// is defined here; other tools may add their own namespaces alongside it.
type SkillMetadata struct {
	Wuphf SkillWuphfMeta `yaml:"wuphf,omitempty"`
}

// SkillWuphfMeta carries all WUPHF-specific provenance for a compiled skill.
type SkillWuphfMeta struct {
	// Title is the display title shown in the UI (frontend-only).
	Title string `yaml:"title,omitempty"`
	// Trigger is a natural-language trigger phrase kept for legacy broker fields.
	// The top-level Description field is authoritative for LLM routing.
	Trigger string `yaml:"trigger,omitempty"`
	// SourceArticles lists the wiki paths that drove this skill's content.
	SourceArticles []string `yaml:"source_articles,omitempty"`
	// SourceSignals lists notebook citations (Stage B+ only).
	SourceSignals []string `yaml:"source_signals,omitempty"`
	// CreatedBy is the identity that wrote this proposal ("archivist", agent slug, etc.).
	CreatedBy string `yaml:"created_by,omitempty"`
	// Status is one of proposed | active | archived.
	Status string `yaml:"status,omitempty"`
	// LastSynthesizedSHA is the repo HEAD SHA at the time of last synthesis.
	LastSynthesizedSHA string `yaml:"last_synthesized_sha,omitempty"`
	// LastSynthesizedTs is the RFC3339 timestamp of the last synthesis run.
	LastSynthesizedTs string `yaml:"last_synthesized_ts,omitempty"`
	// FactCountAtSynthesis records the fact count when the skill was synthesized.
	FactCountAtSynthesis int `yaml:"fact_count_at_synthesis,omitempty"`
	// SafetyScan holds the result of the skill_guard scan.
	SafetyScan *SkillSafetyScan `yaml:"safety_scan,omitempty"`
	// Tags are for hub indexing.
	Tags []string `yaml:"tags,omitempty"`
	// RelatedSkills lists other skill slugs this skill overlaps with.
	RelatedSkills []string `yaml:"related_skills,omitempty"`
	// WorkflowProvider is the provider for workflow-backed skills.
	WorkflowProvider string `yaml:"workflow_provider,omitempty"`
	// WorkflowKey identifies the workflow within the provider.
	WorkflowKey string `yaml:"workflow_key,omitempty"`
	// WorkflowDefinition is the inline workflow definition.
	WorkflowDefinition string `yaml:"workflow_definition,omitempty"`
	// WorkflowSchedule is the cron schedule for scheduled workflow skills.
	WorkflowSchedule string `yaml:"workflow_schedule,omitempty"`
	// RelayID is the relay event subscription ID.
	RelayID string `yaml:"relay_id,omitempty"`
	// RelayPlatform is the relay event source platform.
	RelayPlatform string `yaml:"relay_platform,omitempty"`
	// RelayEventTypes lists the relay event types this skill subscribes to.
	RelayEventTypes []string `yaml:"relay_event_types,omitempty"`
}

// SkillSafetyScan holds the result of a skill_guard scan.
// Verdict is one of safe | caution | dangerous.
type SkillSafetyScan struct {
	// Verdict is safe | caution | dangerous.
	Verdict string `yaml:"verdict"`
	// Findings is the list of specific issues found during the scan.
	Findings []string `yaml:"findings,omitempty"`
	// TrustLevel is the trust tier applied during this scan.
	TrustLevel string `yaml:"trust_level,omitempty"`
	// Summary is a human-readable explanation of the verdict.
	Summary string `yaml:"summary,omitempty"`
}

// RenderSkillMarkdown serialises fm and body into a markdown document with
// YAML frontmatter delimiters. Name and Description must be non-empty.
// The body is trimmed of leading/trailing whitespace.
func RenderSkillMarkdown(fm SkillFrontmatter, body string) ([]byte, error) {
	if strings.TrimSpace(fm.Name) == "" {
		return nil, errors.New("skill frontmatter: name is mandatory")
	}
	if strings.TrimSpace(fm.Description) == "" {
		return nil, errors.New("skill frontmatter: description is mandatory")
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("skill frontmatter: yaml encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("skill frontmatter: yaml close: %w", err)
	}
	buf.WriteString("---\n")

	trimmed := strings.TrimSpace(body)
	if trimmed != "" {
		buf.WriteString("\n")
		buf.WriteString(trimmed)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// ParseSkillMarkdown splits YAML frontmatter from body, parses the YAML, and
// returns (frontmatter, body, error). Tolerate missing optional fields.
// Returns an error when name or description is absent.
func ParseSkillMarkdown(content []byte) (SkillFrontmatter, string, error) {
	s := string(content)
	if !strings.HasPrefix(s, "---\n") {
		return SkillFrontmatter{}, "", errors.New("skill frontmatter: missing opening delimiter")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return SkillFrontmatter{}, "", errors.New("skill frontmatter: missing closing delimiter")
	}
	yamlBlock := rest[:end]
	body := strings.TrimSpace(rest[end+len("\n---"):])

	var fm SkillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return SkillFrontmatter{}, "", fmt.Errorf("skill frontmatter: yaml decode: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return SkillFrontmatter{}, "", errors.New("skill frontmatter: name is mandatory")
	}
	if strings.TrimSpace(fm.Description) == "" {
		return SkillFrontmatter{}, "", errors.New("skill frontmatter: description is mandatory")
	}

	return fm, body, nil
}

// teamSkillToFrontmatter converts a teamSkill into its SkillFrontmatter
// representation. All fields are populated so the emitted YAML is hub-ready.
func teamSkillToFrontmatter(sk teamSkill) SkillFrontmatter {
	return SkillFrontmatter{
		Name:        sk.Name,
		Description: sk.Description,
		Version:     "1.0.0",
		License:     "MIT",
		Metadata: SkillMetadata{
			Wuphf: SkillWuphfMeta{
				Title:              sk.Title,
				Trigger:            sk.Trigger,
				CreatedBy:          sk.CreatedBy,
				Status:             sk.Status,
				Tags:               append([]string(nil), sk.Tags...),
				WorkflowProvider:   sk.WorkflowProvider,
				WorkflowKey:        sk.WorkflowKey,
				WorkflowDefinition: sk.WorkflowDefinition,
				WorkflowSchedule:   sk.WorkflowSchedule,
				RelayID:            sk.RelayID,
				RelayPlatform:      sk.RelayPlatform,
				RelayEventTypes:    append([]string(nil), sk.RelayEventTypes...),
			},
		},
	}
}

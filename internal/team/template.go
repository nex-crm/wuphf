package team

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
)

type generatedMemberTemplate struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	Expertise      []string `json:"expertise"`
	Personality    string   `json:"personality"`
	PermissionMode string   `json:"permission_mode"`
}

func (l *Launcher) GenerateMemberTemplateFromPrompt(request string) (generatedMemberTemplate, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return generatedMemberTemplate{}, fmt.Errorf("prompt is required")
	}
	if stub := strings.TrimSpace(os.Getenv("WUPHF_AGENT_TEMPLATE_STUB")); stub != "" {
		return parseGeneratedMemberTemplate(stub)
	}
	systemPrompt := l.buildPrompt(l.officeLeadSlug()) + `

You are designing a NEW office teammate template for WUPHF.
Return exactly one JSON object and nothing else.
Do not wrap it in markdown fences.
Do not explain your reasoning.

Required schema:
{
  "slug": "lowercase-hyphen-slug",
  "name": "Display Name",
  "role": "Role / title",
  "expertise": ["area", "area"],
  "personality": "one short paragraph",
  "permission_mode": "plan"
}

Constraints:
- Never use slug "ceo".
- Keep the teammate narrow and domain-specific.
- Pick a role that complements the existing office rather than overlapping heavily.
- If the prompt is vague, still make a crisp decision.
- permission_mode should usually be "plan" unless the role clearly needs autonomous editing/coding.
`
	userPrompt := "Design a new office teammate from this request:\n\n" + request
	raw, err := provider.RunConfiguredOneShot(systemPrompt, userPrompt, l.cwd)
	if err != nil {
		return generatedMemberTemplate{}, err
	}
	jsonText := extractJSONObjectString(raw)
	if jsonText == "" {
		jsonText = strings.TrimSpace(raw)
	}
	return parseGeneratedMemberTemplate(jsonText)
}

func parseGeneratedMemberTemplate(raw string) (generatedMemberTemplate, error) {
	var tmpl generatedMemberTemplate
	if err := json.Unmarshal([]byte(raw), &tmpl); err != nil {
		return generatedMemberTemplate{}, fmt.Errorf("parse generated agent template: %w", err)
	}
	tmpl.Slug = normalizeChannelSlug(tmpl.Slug)
	if tmpl.Slug == "" || tmpl.Slug == "ceo" {
		return generatedMemberTemplate{}, fmt.Errorf("generated invalid slug %q", tmpl.Slug)
	}
	if tmpl.Name == "" {
		tmpl.Name = humanizeSlug(tmpl.Slug)
	}
	if tmpl.Role == "" {
		tmpl.Role = tmpl.Name
	}
	if len(tmpl.Expertise) == 0 {
		tmpl.Expertise = inferOfficeExpertise(tmpl.Slug, tmpl.Role)
	}
	if tmpl.Personality == "" {
		tmpl.Personality = inferOfficePersonality(tmpl.Slug, tmpl.Role)
	}
	if tmpl.PermissionMode == "" {
		tmpl.PermissionMode = "plan"
	}
	return tmpl, nil
}

type generatedChannelTemplate struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
}

func (l *Launcher) GenerateChannelTemplateFromPrompt(request string) (generatedChannelTemplate, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return generatedChannelTemplate{}, fmt.Errorf("prompt is required")
	}
	if stub := strings.TrimSpace(os.Getenv("WUPHF_CHANNEL_TEMPLATE_STUB")); stub != "" {
		return parseGeneratedChannelTemplate(stub)
	}
	systemPrompt := l.buildPrompt(l.officeLeadSlug()) + `

You are designing a NEW office channel for WUPHF.
Return exactly one JSON object and nothing else.
Do not wrap it in markdown fences.
Do not explain your reasoning.

Required schema:
{
  "slug": "lowercase-hyphen-slug",
  "name": "Display Name",
  "description": "One sentence explaining the channel purpose",
  "members": ["ceo", "relevant-member-slug"]
}

Constraints:
- Never use slug "general".
- Keep the channel focused on a specific topic or workstream.
- Always include "ceo" in members.
- Pick members that match the channel topic from the existing office roster.
- If the prompt is vague, still make a crisp decision.
`
	userPrompt := "Design a new office channel from this request:\n\n" + request
	raw, err := provider.RunConfiguredOneShot(systemPrompt, userPrompt, l.cwd)
	if err != nil {
		return generatedChannelTemplate{}, err
	}
	jsonText := extractJSONObjectString(raw)
	if jsonText == "" {
		jsonText = strings.TrimSpace(raw)
	}
	return parseGeneratedChannelTemplate(jsonText)
}

func parseGeneratedChannelTemplate(raw string) (generatedChannelTemplate, error) {
	var tmpl generatedChannelTemplate
	if err := json.Unmarshal([]byte(raw), &tmpl); err != nil {
		return generatedChannelTemplate{}, fmt.Errorf("parse generated channel template: %w", err)
	}
	tmpl.Slug = normalizeChannelSlug(tmpl.Slug)
	if tmpl.Slug == "" || tmpl.Slug == "general" {
		return generatedChannelTemplate{}, fmt.Errorf("generated invalid slug %q", tmpl.Slug)
	}
	if tmpl.Name == "" {
		tmpl.Name = humanizeSlug(tmpl.Slug)
	}
	if tmpl.Description == "" {
		tmpl.Description = defaultTeamChannelDescription(tmpl.Slug, tmpl.Name)
	}
	hasCEO := false
	for _, m := range tmpl.Members {
		if m == "ceo" {
			hasCEO = true
			break
		}
	}
	if !hasCEO {
		tmpl.Members = append([]string{"ceo"}, tmpl.Members...)
	}
	return tmpl, nil
}

func extractJSONObjectString(raw string) string {
	start := strings.Index(raw, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

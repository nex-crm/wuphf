package team

import (
	"context"
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
	// Provider and Model are CEO suggestions for the agent's runtime. The
	// AgentWizard pre-fills its picker from these when the suggested
	// provider is in the install's registered LLM provider list (i.e. a
	// non-gateway kind). When absent or a gateway kind, the wizard falls
	// back to "Inherit default" — the human always gets the final pick
	// because the CEO can't reliably reason about local-runtime availability.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (l *Launcher) GenerateMemberTemplateFromPrompt(request string) (generatedMemberTemplate, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return generatedMemberTemplate{}, fmt.Errorf("prompt is required")
	}
	if stub := strings.TrimSpace(os.Getenv("WUPHF_AGENT_TEMPLATE_STUB")); stub != "" {
		return parseGeneratedMemberTemplate(stub)
	}
	systemPrompt := l.buildPrompt(l.targeter().LeadSlug()) + `

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
  "permission_mode": "plan",
  "provider": "claude-code | codex | opencode | mlx-lm | ollama | exo",
  "model": "runtime-specific model identifier or empty"
}

Constraints:
- Never use slug "ceo".
- Keep the teammate narrow and domain-specific.
- Pick a role that complements the existing office rather than overlapping heavily.
- If the prompt is vague, still make a crisp decision.
- permission_mode should usually be "plan" unless the role clearly needs autonomous editing/coding.
- "provider" is the LLM runtime the agent should run on. Pick one of:
  claude-code, codex, opencode (cloud) or mlx-lm, ollama, exo (local).
  Never suggest "openclaw", "openclaw-http", or "hermes-agent" — those are
  gateways for importing existing agents, not runtimes for new ones.
- "model" is the model identifier inside the chosen runtime (for example
  "claude-3-5-sonnet-latest" for claude-code, "gpt-4o" for codex,
  "llama3.1:8b" for ollama). Leave empty if you have no opinion — the
  runtime's default will be used.
- The human will confirm provider and model in the next step. Your job is
  to suggest a sensible default, not to lock the choice.
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
	// Sanitize provider/model: drop suggestions that name a gateway kind so
	// the wizard never has to handle them. Per-agent gateway bindings are
	// established through the Integrations app, not through the CEO
	// template generator — the wizard's runtime picker only shows the
	// non-gateway registered LLM kinds.
	tmpl.Provider = strings.TrimSpace(strings.ToLower(tmpl.Provider))
	if provider.IsGatewayKind(tmpl.Provider) {
		tmpl.Provider = ""
		tmpl.Model = ""
	}
	tmpl.Model = strings.TrimSpace(tmpl.Model)
	return tmpl, nil
}

type generatedChannelTemplate struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
}

func (l *Launcher) GenerateChannelTemplateFromPrompt(request string) (generatedChannelTemplate, error) {
	return l.GenerateChannelTemplateFromPromptCtx(context.Background(), request)
}

func (l *Launcher) GenerateChannelTemplateFromPromptCtx(ctx context.Context, request string) (generatedChannelTemplate, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return generatedChannelTemplate{}, fmt.Errorf("prompt is required")
	}
	if stub := strings.TrimSpace(os.Getenv("WUPHF_CHANNEL_TEMPLATE_STUB")); stub != "" {
		return parseGeneratedChannelTemplate(stub)
	}
	systemPrompt := l.buildPrompt(l.targeter().LeadSlug()) + `

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
	raw, err := provider.RunConfiguredOneShotCtx(ctx, systemPrompt, userPrompt, l.cwd)
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

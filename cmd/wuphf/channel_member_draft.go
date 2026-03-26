package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

var memberDraftSteps = []string{
	"slug",
	"name",
	"role",
	"expertise",
	"personality",
	"permission",
}

func (m channelModel) startEditMemberDraft(slug string) (*channelMemberDraft, bool) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, member := range m.officeMembers {
		if member.Slug != slug {
			continue
		}
		return &channelMemberDraft{
			Mode:         "edit",
			OriginalSlug: member.Slug,
			Step:         1,
			Slug:         member.Slug,
			Name:         member.Name,
			Role:         member.Role,
			Expertise:    strings.Join(member.Expertise, ", "),
			Personality:  member.Personality,
		}, true
	}
	return nil, false
}

func (m channelModel) submitMemberDraft() (tea.Model, tea.Cmd) {
	if m.memberDraft == nil {
		return m, nil
	}
	value := strings.TrimSpace(string(m.input))
	draft := *m.memberDraft

	switch draft.currentStep() {
	case "slug":
		if value == "" {
			m.notice = "Slug is required."
			return m, nil
		}
		draft.Slug = normalizeDraftSlug(value)
		if draft.Slug == "ceo" {
			m.notice = "CEO is reserved."
			return m, nil
		}
		draft.Step++
	case "name":
		if value == "" {
			m.notice = "Display name is required."
			return m, nil
		}
		draft.Name = value
		draft.Step++
	case "role":
		if value != "" {
			draft.Role = value
		} else if draft.Role == "" {
			draft.Role = draft.Name
		}
		draft.Step++
	case "expertise":
		draft.Expertise = value
		draft.Step++
	case "personality":
		draft.Personality = value
		draft.Step++
	case "permission":
		if value != "" {
			draft.PermissionMode = value
		}
		m.memberDraft = nil
		m.input = nil
		m.inputPos = 0
		m.posting = true
		return m, mutateOfficeMemberSpec(draft, m.activeChannel)
	}

	m.memberDraft = &draft
	m.input = nil
	m.inputPos = 0
	m.notice = memberDraftStepHint(draft)
	return m, nil
}

func (d channelMemberDraft) currentStep() string {
	if d.Step < 0 {
		return memberDraftSteps[0]
	}
	if d.Step >= len(memberDraftSteps) {
		return memberDraftSteps[len(memberDraftSteps)-1]
	}
	return memberDraftSteps[d.Step]
}

func memberDraftComposerLabel(d channelMemberDraft) string {
	switch d.currentStep() {
	case "slug":
		return "New teammate slug"
	case "name":
		return "New teammate name"
	case "role":
		return "Teammate role/title"
	case "expertise":
		return "Expertise list"
	case "personality":
		return "Personality"
	case "permission":
		return "Permission mode"
	default:
		return "Agent setup"
	}
}

func memberDraftStepHint(d channelMemberDraft) string {
	switch d.currentStep() {
	case "slug":
		return "Choose a stable slug like growthops or ml-research."
	case "name":
		return "Give them a human display name."
	case "role":
		return "Set the role/title. Leave blank to reuse the name."
	case "expertise":
		return "List expertise separated by commas."
	case "personality":
		return "Give them a short personality blurb."
	case "permission":
		return "Set permission mode, usually plan or auto."
	default:
		return ""
	}
}

func renderMemberDraftCard(draft channelMemberDraft, width int) string {
	if width < 40 {
		width = 40
	}
	title := "New teammate"
	if draft.Mode == "edit" {
		title = "Edit teammate"
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	fields := []struct {
		label  string
		value  string
		active bool
	}{
		{"Slug", draft.Slug, draft.currentStep() == "slug"},
		{"Name", draft.Name, draft.currentStep() == "name"},
		{"Role", draft.Role, draft.currentStep() == "role"},
		{"Expertise", draft.Expertise, draft.currentStep() == "expertise"},
		{"Personality", draft.Personality, draft.currentStep() == "personality"},
		{"Permission", draft.PermissionMode, draft.currentStep() == "permission"},
	}

	lines := []string{
		labelStyle.Render(title),
		titleStyle.Render(memberDraftComposerLabel(draft)),
		"",
		muted.Render(memberDraftStepHint(draft)),
		muted.Render("Give them a real lane and enough personality to sound human, not like a workflow step."),
		"",
	}
	for _, field := range fields {
		prefix := "  "
		if field.active {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
		}
		value := field.value
		if strings.TrimSpace(value) == "" {
			value = "—"
		}
		lines = append(lines, prefix+titleStyle.Render(field.label)+": "+textStyle.Width(width-12).Render(value))
	}
	lines = append(lines, "", muted.Render("Press Enter to save this step. Esc cancels. Try not to hire another chaos goblin unless you mean it."))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#60A5FA")).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

func normalizeDraftSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "-")
	raw = strings.ReplaceAll(raw, "_", "-")
	return raw
}

func parseExpertiseInput(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func mutateOfficeMemberSpec(draft channelMemberDraft, activeChannel string) tea.Cmd {
	return func() tea.Msg {
		action := "create"
		if draft.Mode == "edit" {
			action = "update"
		}
		body, _ := json.Marshal(map[string]any{
			"action":          action,
			"slug":            draft.Slug,
			"name":            draft.Name,
			"role":            draft.Role,
			"expertise":       parseExpertiseInput(draft.Expertise),
			"personality":     draft.Personality,
			"permission_mode": strings.TrimSpace(draft.PermissionMode),
			"created_by":      "you",
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/office-members", bytes.NewReader(body))
		if err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelMemberDraftDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if action == "create" {
			addBody, _ := json.Marshal(map[string]any{
				"action":  "add",
				"channel": activeChannel,
				"slug":    draft.Slug,
			})
			addReq, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channel-members", bytes.NewReader(addBody))
			if err == nil {
				addReq.Header.Set("Content-Type", "application/json")
				_, _ = client.Do(addReq)
			}
		}
		l, err := team.NewLauncher("")
		if err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		verb := "Created"
		if action == "update" {
			verb = "Updated"
		}
		return channelMemberDraftDoneMsg{notice: fmt.Sprintf("%s teammate %s.", verb, draft.Name)}
	}
}

func generateOfficeMemberFromPrompt(prompt, activeChannel string) tea.Cmd {
	return func() tea.Msg {
		l, err := team.NewLauncher("")
		if err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		tmpl, err := l.GenerateMemberTemplateFromPrompt(prompt)
		if err != nil {
			return channelMemberDraftDoneMsg{err: err}
		}
		draft := channelMemberDraft{
			Mode:           "create",
			Slug:           tmpl.Slug,
			Name:           tmpl.Name,
			Role:           tmpl.Role,
			Expertise:      strings.Join(tmpl.Expertise, ", "),
			Personality:    tmpl.Personality,
			PermissionMode: tmpl.PermissionMode,
		}
		return mutateOfficeMemberSpec(draft, activeChannel)()
	}
}

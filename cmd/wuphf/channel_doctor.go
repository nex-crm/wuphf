package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
)

type doctorSeverity string

const (
	doctorOK   doctorSeverity = "ok"
	doctorWarn doctorSeverity = "warn"
	doctorFail doctorSeverity = "fail"
	doctorInfo doctorSeverity = "info"
)

type doctorCheck struct {
	Label    string
	Severity doctorSeverity
	Detail   string
	NextStep string
}

type channelDoctorReport struct {
	GeneratedAt time.Time
	Checks      []doctorCheck
}

type channelDoctorDoneMsg struct {
	report channelDoctorReport
	err    error
}

func (r channelDoctorReport) counts() (ok, warn, fail int) {
	for _, check := range r.Checks {
		switch check.Severity {
		case doctorOK:
			ok++
		case doctorWarn:
			warn++
		case doctorFail:
			fail++
		}
	}
	return ok, warn, fail
}

func (r channelDoctorReport) StatusLine() string {
	ok, warn, fail := r.counts()
	switch {
	case fail > 0:
		return fmt.Sprintf("%d healthy · %d warning · %d blocked", ok, warn, fail)
	case warn > 0:
		return fmt.Sprintf("%d healthy · %d warning", ok, warn)
	default:
		return fmt.Sprintf("%d healthy · ready to work", ok)
	}
}

func runDoctorChecks() tea.Cmd {
	return func() tea.Msg {
		report, err := inspectDoctor()
		return channelDoctorDoneMsg{report: report, err: err}
	}
}

func inspectDoctor() (channelDoctorReport, error) {
	report := channelDoctorReport{GeneratedAt: time.Now()}
	report.Checks = append(report.Checks,
		doctorBinaryCheck("tmux", "Needed for the office panes and session management."),
		doctorBinaryCheck("claude", "Needed for the teammate runtime."),
	)

	if config.ResolveNoNex() {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Nex",
			Severity: doctorInfo,
			Detail:   "Disabled for this session with --no-nex.",
			NextStep: "Restart WUPHF without --no-nex to enable memory, integrations, and provider-backed actions.",
		})
		return report, nil
	}

	apiKey := strings.TrimSpace(config.ResolveAPIKey(""))
	if apiKey == "" {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Nex API key",
			Severity: doctorFail,
			Detail:   "Missing WUPHF/Nex API key.",
			NextStep: "Run /init and paste your WUPHF API key.",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Nex API key",
			Severity: doctorOK,
			Detail:   "Configured and ready for Nex-backed context.",
		})
	}

	email := strings.TrimSpace(config.ResolveComposioUserID())
	if email == "" {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Workspace identity",
			Severity: doctorWarn,
			Detail:   "No saved email identity for integrations.",
			NextStep: "Finish /init so WUPHF can scope integration providers to your Nex email.",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Workspace identity",
			Severity: doctorOK,
			Detail:   fmt.Sprintf("Using %s for provider identity.", email),
		})
	}

	resolvedProvider := config.ResolveActionProvider()
	if resolvedProvider == "" {
		resolvedProvider = "auto"
	}
	registry := action.NewRegistryFromEnv()
	provider, err := registry.ProviderFor(action.CapabilityConnections)
	if err != nil {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Action provider",
			Severity: doctorFail,
			Detail:   fmt.Sprintf("No working provider for external actions (%s).", resolvedProvider),
			NextStep: "Set /config set composio_api_key <key> or choose a configured provider.",
		})
		return report, nil
	}

	report.Checks = append(report.Checks, doctorCheck{
		Label:    "Action provider",
		Severity: doctorOK,
		Detail:   fmt.Sprintf("Using %s for external actions.", provider.Name()),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections, err := provider.ListConnections(ctx, action.ListConnectionsOptions{Limit: 5})
	if err != nil {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Connected accounts",
			Severity: doctorWarn,
			Detail:   fmt.Sprintf("Provider responded with an error: %v", err),
			NextStep: "Open the provider dashboard and confirm at least one account is connected for your Nex email.",
		})
		return report, nil
	}
	if len(connections.Connections) == 0 {
		report.Checks = append(report.Checks, doctorCheck{
			Label:    "Connected accounts",
			Severity: doctorWarn,
			Detail:   fmt.Sprintf("%s is configured, but no connected accounts are available yet.", strings.Title(provider.Name())),
			NextStep: "Connect Gmail, CRM, or another account in the provider dashboard, then rerun /doctor.",
		})
		return report, nil
	}

	report.Checks = append(report.Checks, doctorCheck{
		Label:    "Connected accounts",
		Severity: doctorOK,
		Detail:   fmt.Sprintf("%d account%s ready through %s.", len(connections.Connections), pluralSuffix(len(connections.Connections)), strings.Title(provider.Name())),
	})
	return report, nil
}

func doctorBinaryCheck(name, reason string) doctorCheck {
	if _, err := exec.LookPath(name); err != nil {
		return doctorCheck{
			Label:    name,
			Severity: doctorFail,
			Detail:   fmt.Sprintf("%s is not available on PATH.", name),
			NextStep: reason,
		}
	}
	return doctorCheck{
		Label:    name,
		Severity: doctorOK,
		Detail:   fmt.Sprintf("%s is installed.", name),
	}
}

func renderDoctorCard(report channelDoctorReport, width int) string {
	cardWidth := maxInt(48, width)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render("Doctor")
	meta := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Render(report.GeneratedAt.Format("Jan 2 15:04"))
	lines := []string{
		title + "  " + subtlePill(report.StatusLine(), "#E5E7EB", "#334155") + "  " + meta,
		mutedText("This is the live readiness check for setup, integrations, and the agent runtime."),
		"",
	}

	for _, check := range report.Checks {
		label := renderDoctorLabel(check)
		lines = append(lines, label+" "+check.Detail)
		if strings.TrimSpace(check.NextStep) != "" {
			lines = append(lines, "  "+mutedText("Next: "+check.NextStep))
		}
		lines = append(lines, "")
	}
	lines = append(lines, mutedText("Esc or /cancel closes this panel."))

	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		Background(lipgloss.Color("#14151B")).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

func renderDoctorLabel(check doctorCheck) string {
	switch check.Severity {
	case doctorOK:
		return accentPill(check.Label, "#15803D")
	case doctorWarn:
		return accentPill(check.Label, "#B45309")
	case doctorFail:
		return accentPill(check.Label, "#B91C1C")
	default:
		return subtlePill(check.Label, "#E2E8F0", "#334155")
	}
}

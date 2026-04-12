package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

type doctorSeverity string

const (
	doctorOK   doctorSeverity = "ok"
	doctorWarn doctorSeverity = "warn"
	doctorFail doctorSeverity = "fail"
	doctorInfo doctorSeverity = "info"
)

type doctorCheck struct {
	Label     string
	Severity  doctorSeverity
	Lifecycle team.CapabilityLifecycle
	Detail    string
	NextStep  string
}

type channelDoctorReport struct {
	GeneratedAt time.Time
	Checks      []doctorCheck
	Registry    team.CapabilityRegistry
}

type channelDoctorDoneMsg struct {
	report channelDoctorReport
	err    error
}

var detectRuntimeCapabilitiesFn = func(opts team.CapabilityProbeOptions) team.RuntimeCapabilities {
	return team.DetectRuntimeCapabilitiesWithOptions(opts)
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
	capabilities := detectRuntimeCapabilitiesFn(team.CapabilityProbeOptions{
		IncludeConnections: true,
		ConnectionLimit:    5,
		ConnectionTimeout:  5 * time.Second,
	})
	report := channelDoctorReport{
		GeneratedAt: time.Now(),
		Registry:    capabilities.Registry,
	}
	for _, entry := range capabilities.Registry.Entries {
		report.Checks = append(report.Checks, doctorCheck{
			Label:     entry.Label,
			Severity:  doctorSeverityForCapability(entry),
			Lifecycle: entry.Lifecycle,
			Detail:    entry.Detail,
			NextStep:  entry.NextStep,
		})
	}
	return report, nil
}

func doctorSeverityForCapability(entry team.CapabilityDescriptor) doctorSeverity {
	switch entry.Level {
	case team.CapabilityReady:
		return doctorOK
	case team.CapabilityWarn:
		switch entry.Lifecycle {
		case team.CapabilityLifecycleNeedsSetup:
			return doctorFail
		default:
			return doctorWarn
		}
	default:
		return doctorInfo
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
		if strings.TrimSpace(string(check.Lifecycle)) != "" {
			label += " " + renderDoctorLifecycle(check.Lifecycle)
		}
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

func renderDoctorLifecycle(lifecycle team.CapabilityLifecycle) string {
	label := strings.ReplaceAll(string(lifecycle), "_", " ")
	switch lifecycle {
	case team.CapabilityLifecycleReady:
		return subtlePill(label, "#DCFCE7", "#166534")
	case team.CapabilityLifecycleDisabled:
		return subtlePill(label, "#E2E8F0", "#334155")
	case team.CapabilityLifecycleDeferred, team.CapabilityLifecyclePartial, team.CapabilityLifecycleProvisioning:
		return subtlePill(label, "#FEF3C7", "#92400E")
	default:
		return subtlePill(label, "#FEE2E2", "#991B1B")
	}
}

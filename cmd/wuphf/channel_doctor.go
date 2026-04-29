package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/team"
)

type channelDoctorDoneMsg struct {
	report channelDoctorReport
	err    error
}

var detectRuntimeCapabilitiesFn = func(opts team.CapabilityProbeOptions) team.RuntimeCapabilities {
	return team.DetectRuntimeCapabilitiesWithOptions(opts)
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

package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/team"
)

func TestInspectDoctorUsesCapabilityRegistry(t *testing.T) {
	prev := detectRuntimeCapabilitiesFn
	detectRuntimeCapabilitiesFn = func(team.CapabilityProbeOptions) team.RuntimeCapabilities {
		return team.RuntimeCapabilities{
			Registry: team.CapabilityRegistry{
				Entries: []team.CapabilityDescriptor{
					{
						Key:       team.CapabilityKeyActions,
						Label:     "External actions",
						Category:  team.CapabilityCategoryAction,
						Level:     team.CapabilityWarn,
						Lifecycle: team.CapabilityLifecycleNeedsSetup,
						Detail:    "No configured provider available for action_execute.",
						NextStep:  "Configure a working provider.",
					},
					{
						Key:       team.CapabilityKeyConnections,
						Label:     "Connected accounts",
						Category:  team.CapabilityCategoryAction,
						Level:     team.CapabilityWarn,
						Lifecycle: team.CapabilityLifecyclePartial,
						Detail:    "One is configured, but no connected accounts are available yet.",
						NextStep:  "Connect Gmail, CRM, or another account.",
					},
				},
			},
		}
	}
	defer func() { detectRuntimeCapabilitiesFn = prev }()

	report, err := inspectDoctor()
	if err != nil {
		t.Fatalf("inspectDoctor: %v", err)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("expected two checks, got %+v", report.Checks)
	}
	if report.Checks[0].Severity != doctorFail {
		t.Fatalf("expected needs-setup capability to map to fail, got %+v", report.Checks[0])
	}
	if report.Checks[1].Severity != doctorWarn {
		t.Fatalf("expected partial capability to map to warn, got %+v", report.Checks[1])
	}

	card := renderDoctorCard(report, 80)
	for _, want := range []string{"needs setup", "partial", "Connected accounts"} {
		if !strings.Contains(card, want) {
			t.Fatalf("expected %q in doctor card: %q", want, card)
		}
	}
	if strings.Contains(card, "Capability map:") {
		t.Fatalf("doctor card should not contain redundant capability map section")
	}
}

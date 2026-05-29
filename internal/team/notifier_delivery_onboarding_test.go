package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// TestIsDeterministicPhase2CEODM_GatesCEODMDuringPhase2 locks in the
// regression that an LLM agent must not fire while the deterministic
// Phase 2 onboarding conversation is driving the CEO DM. Prior to the
// gate, deliverMessageNotification enqueued a Claude turn for every
// human reply to a ceo_form_field / ceo_chip_row / ceo_team_trim card,
// which produced a sticky "CEO is typing…" indicator and stalled the
// scripted flow behind a hallucinated LLM response.
func TestIsDeterministicPhase2CEODM_GatesCEODMDuringPhase2(t *testing.T) {
	cases := []struct {
		name    string
		channel string
		phase   string
		onboard bool
		want    bool
	}{
		{"CEO DM mid-phase-blueprint", "ceo__human", onboarding.PhaseBlueprint, false, true},
		{"CEO DM mid-phase-team", "ceo__human", onboarding.PhaseTeam, false, true},
		{"CEO DM mid-phase-greet", "human__ceo", onboarding.PhaseGreet, false, true},
		{"reserved CEO DM slug", onboarding.CEOOnboardingDMSlug, onboarding.PhaseBlueprint, false, true},
		{"CEO DM after onboarding complete", "ceo__human", onboarding.PhaseComplete, true, false},
		{"CEO DM in LLM-backed draft phase", "ceo__human", onboarding.PhaseDraft, false, false},
		{"non-CEO DM during Phase 2", "engineer__human", onboarding.PhaseTeam, false, false},
		{"non-DM channel during Phase 2", "general", onboarding.PhaseTeam, false, false},
		{"empty channel during Phase 2", "", onboarding.PhaseTeam, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// Belt-and-suspenders: clear WUPHF_RUNTIME_HOME so RuntimeHomeDir
			// resolves to the temp HOME and the test stays sealed from any
			// stale local broker state on the dev machine.
			t.Setenv("WUPHF_RUNTIME_HOME", "")

			s, err := onboarding.Load()
			if err != nil {
				t.Fatalf("onboarding.Load: %v", err)
			}
			s.Phase = tc.phase
			if tc.onboard {
				s.CompletedAt = "2026-05-28T00:00:00Z"
			}
			if err := onboarding.Save(s); err != nil {
				t.Fatalf("onboarding.Save: %v", err)
			}

			got := isDeterministicPhase2CEODM(tc.channel)
			if got != tc.want {
				t.Fatalf("isDeterministicPhase2CEODM(%q) with phase=%q onboarded=%v = %v, want %v",
					tc.channel, tc.phase, tc.onboard, got, tc.want)
			}
		})
	}
}

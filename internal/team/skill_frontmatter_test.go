package team

import (
	"strings"
	"testing"
)

func TestRenderAndParseSkillMarkdown_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fm   SkillFrontmatter
		body string
	}{
		{
			name: "minimal fields",
			fm: SkillFrontmatter{
				Name:        "daily-digest",
				Description: "Send a daily summary to the team.",
			},
			body: "## Steps\n\n1. Gather messages.\n2. Send digest.",
		},
		{
			name: "all top-level fields",
			fm: SkillFrontmatter{
				Name:        "incident-triage",
				Description: "Triage production incidents systematically.",
				Version:     "1.2.3",
				License:     "Apache-2.0",
			},
			body: "Follow the runbook in team/playbooks/incident.md.",
		},
		{
			name: "full wuphf metadata",
			fm: SkillFrontmatter{
				Name:        "weekly-retro",
				Description: "Run a weekly retrospective.",
				Version:     "1.0.0",
				License:     "MIT",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						Title:                "Weekly Retro",
						Trigger:              "Every Friday",
						SourceArticles:       []string{"team/playbooks/retro.md"},
						SourceSignals:        []string{"team/agents/eng/notebook/retro-notes.md"},
						CreatedBy:            "archivist",
						Status:               "disabled",
						DisabledFromStatus:   "proposed",
						LastSynthesizedSHA:   "abc1234",
						LastSynthesizedTs:    "2026-04-28T12:00:00Z",
						FactCountAtSynthesis: 42,
						Tags:                 []string{"process", "team"},
						RelatedSkills:        []string{"sprint-planning"},
						WorkflowProvider:     "zapier",
						WorkflowKey:          "wk-001",
						WorkflowDefinition:   "trigger: weekly",
						WorkflowSchedule:     "0 17 * * 5",
						RelayID:              "relay-abc",
						RelayPlatform:        "slack",
						RelayEventTypes:      []string{"message", "reaction"},
					},
				},
			},
			body: "Run the retro template each week.",
		},
		{
			name: "with safety scan",
			fm: SkillFrontmatter{
				Name:        "safe-deploy",
				Description: "Deploy with zero-downtime strategy.",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						CreatedBy: "archivist",
						Status:    "proposed",
						SafetyScan: &SkillSafetyScan{
							Verdict:    "safe",
							Findings:   []string{},
							TrustLevel: "community",
							Summary:    "No dangerous patterns detected.",
						},
					},
				},
			},
			body: "Use rolling deploys.",
		},
		{
			name: "empty body",
			fm: SkillFrontmatter{
				Name:        "ping",
				Description: "Ping the team.",
			},
			body: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := RenderSkillMarkdown(tc.fm, tc.body)
			if err != nil {
				t.Fatalf("RenderSkillMarkdown: %v", err)
			}

			got, gotBody, err := ParseSkillMarkdown(rendered)
			if err != nil {
				t.Fatalf("ParseSkillMarkdown: %v", err)
			}

			// Mandatory fields.
			if got.Name != tc.fm.Name {
				t.Errorf("Name: got %q, want %q", got.Name, tc.fm.Name)
			}
			if got.Description != tc.fm.Description {
				t.Errorf("Description: got %q, want %q", got.Description, tc.fm.Description)
			}

			// Optional top-level fields.
			if tc.fm.Version != "" && got.Version != tc.fm.Version {
				t.Errorf("Version: got %q, want %q", got.Version, tc.fm.Version)
			}
			if tc.fm.License != "" && got.License != tc.fm.License {
				t.Errorf("License: got %q, want %q", got.License, tc.fm.License)
			}

			// wuphf metadata.
			w := tc.fm.Metadata.Wuphf
			gw := got.Metadata.Wuphf
			if w.Title != "" && gw.Title != w.Title {
				t.Errorf("Wuphf.Title: got %q, want %q", gw.Title, w.Title)
			}
			if w.Trigger != "" && gw.Trigger != w.Trigger {
				t.Errorf("Wuphf.Trigger: got %q, want %q", gw.Trigger, w.Trigger)
			}
			if w.CreatedBy != "" && gw.CreatedBy != w.CreatedBy {
				t.Errorf("Wuphf.CreatedBy: got %q, want %q", gw.CreatedBy, w.CreatedBy)
			}
			if w.Status != "" && gw.Status != w.Status {
				t.Errorf("Wuphf.Status: got %q, want %q", gw.Status, w.Status)
			}
			if w.DisabledFromStatus != "" && gw.DisabledFromStatus != w.DisabledFromStatus {
				t.Errorf("Wuphf.DisabledFromStatus: got %q, want %q", gw.DisabledFromStatus, w.DisabledFromStatus)
			}
			if w.LastSynthesizedSHA != "" && gw.LastSynthesizedSHA != w.LastSynthesizedSHA {
				t.Errorf("Wuphf.LastSynthesizedSHA: got %q, want %q", gw.LastSynthesizedSHA, w.LastSynthesizedSHA)
			}
			if w.FactCountAtSynthesis != 0 && gw.FactCountAtSynthesis != w.FactCountAtSynthesis {
				t.Errorf("Wuphf.FactCountAtSynthesis: got %d, want %d", gw.FactCountAtSynthesis, w.FactCountAtSynthesis)
			}
			if w.WorkflowProvider != "" && gw.WorkflowProvider != w.WorkflowProvider {
				t.Errorf("Wuphf.WorkflowProvider: got %q, want %q", gw.WorkflowProvider, w.WorkflowProvider)
			}
			if w.RelayPlatform != "" && gw.RelayPlatform != w.RelayPlatform {
				t.Errorf("Wuphf.RelayPlatform: got %q, want %q", gw.RelayPlatform, w.RelayPlatform)
			}
			if w.SafetyScan != nil {
				if gw.SafetyScan == nil {
					t.Error("SafetyScan: got nil, want non-nil")
				} else if gw.SafetyScan.Verdict != w.SafetyScan.Verdict {
					t.Errorf("SafetyScan.Verdict: got %q, want %q", gw.SafetyScan.Verdict, w.SafetyScan.Verdict)
				}
			}

			// Body round-trip.
			wantBody := strings.TrimSpace(tc.body)
			if gotBody != wantBody {
				t.Errorf("body: got %q, want %q", gotBody, wantBody)
			}
		})
	}
}

func TestRenderSkillMarkdown_MandatoryFieldValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fm      SkillFrontmatter
		wantErr string
	}{
		{
			name:    "empty name",
			fm:      SkillFrontmatter{Name: "", Description: "Some description."},
			wantErr: "name is mandatory",
		},
		{
			name:    "whitespace only name",
			fm:      SkillFrontmatter{Name: "   ", Description: "Some description."},
			wantErr: "name is mandatory",
		},
		{
			name:    "empty description",
			fm:      SkillFrontmatter{Name: "my-skill", Description: ""},
			wantErr: "description is mandatory",
		},
		{
			name:    "whitespace only description",
			fm:      SkillFrontmatter{Name: "my-skill", Description: "\t "},
			wantErr: "description is mandatory",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := RenderSkillMarkdown(tc.fm, "body")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseSkillMarkdown_MandatoryFieldValidation(t *testing.T) {
	t.Parallel()

	missingName := "---\ndescription: Some description.\n---\n\nbody\n"
	missingDesc := "---\nname: my-skill\n---\n\nbody\n"
	noDelimiters := "name: my-skill\ndescription: desc\n"

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "missing name", input: missingName, wantErr: "name is mandatory"},
		{name: "missing description", input: missingDesc, wantErr: "description is mandatory"},
		{name: "no frontmatter delimiters", input: noDelimiters, wantErr: "missing opening delimiter"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := ParseSkillMarkdown([]byte(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestTeamSkillToFrontmatter(t *testing.T) {
	t.Parallel()

	sk := teamSkill{
		Name:               "send-digest",
		Title:              "Send Digest",
		Description:        "Send a daily digest to the team.",
		Content:            "## Steps\n\n1. Gather.\n2. Send.",
		CreatedBy:          "archivist",
		Status:             "disabled",
		DisabledFromStatus: "proposed",
		Tags:               []string{"comms", "daily"},
		Trigger:            "Every morning",
		WorkflowProvider:   "zapier",
		WorkflowKey:        "wk-digest",
		WorkflowDefinition: "trigger: daily",
		WorkflowSchedule:   "0 8 * * *",
		RelayID:            "r-001",
		RelayPlatform:      "slack",
		RelayEventTypes:    []string{"message"},
	}

	fm := teamSkillToFrontmatter(sk)

	if fm.Name != sk.Name {
		t.Errorf("Name: got %q, want %q", fm.Name, sk.Name)
	}
	if fm.Description != sk.Description {
		t.Errorf("Description: got %q, want %q", fm.Description, sk.Description)
	}
	if fm.Version != "1.0.0" {
		t.Errorf("Version: got %q, want 1.0.0", fm.Version)
	}
	if fm.License != "MIT" {
		t.Errorf("License: got %q, want MIT", fm.License)
	}
	w := fm.Metadata.Wuphf
	if w.Title != sk.Title {
		t.Errorf("Wuphf.Title: got %q, want %q", w.Title, sk.Title)
	}
	if w.Trigger != sk.Trigger {
		t.Errorf("Wuphf.Trigger: got %q, want %q", w.Trigger, sk.Trigger)
	}
	if w.CreatedBy != sk.CreatedBy {
		t.Errorf("Wuphf.CreatedBy: got %q, want %q", w.CreatedBy, sk.CreatedBy)
	}
	if w.Status != sk.Status {
		t.Errorf("Wuphf.Status: got %q, want %q", w.Status, sk.Status)
	}
	if w.DisabledFromStatus != sk.DisabledFromStatus {
		t.Errorf("Wuphf.DisabledFromStatus: got %q, want %q", w.DisabledFromStatus, sk.DisabledFromStatus)
	}
	if w.WorkflowProvider != sk.WorkflowProvider {
		t.Errorf("Wuphf.WorkflowProvider: got %q, want %q", w.WorkflowProvider, sk.WorkflowProvider)
	}
	if w.RelayPlatform != sk.RelayPlatform {
		t.Errorf("Wuphf.RelayPlatform: got %q, want %q", w.RelayPlatform, sk.RelayPlatform)
	}
	if len(w.Tags) != len(sk.Tags) {
		t.Errorf("Wuphf.Tags len: got %d, want %d", len(w.Tags), len(sk.Tags))
	}
	if len(w.RelayEventTypes) != len(sk.RelayEventTypes) {
		t.Errorf("Wuphf.RelayEventTypes len: got %d, want %d", len(w.RelayEventTypes), len(sk.RelayEventTypes))
	}
}

func TestSpecToTeamSkillPreservesLifecycleStatusMetadata(t *testing.T) {
	t.Parallel()

	sk := specToTeamSkill(SkillFrontmatter{
		Name:        "approval-required",
		Description: "A proposal paused before approval.",
		Metadata: SkillMetadata{
			Wuphf: SkillWuphfMeta{
				CreatedBy:          "archivist",
				Status:             "disabled",
				DisabledFromStatus: "proposed",
			},
		},
	}, "Step 1: wait for approval.", "")

	if sk.Status != "disabled" {
		t.Errorf("Status: got %q, want disabled", sk.Status)
	}
	if sk.DisabledFromStatus != "proposed" {
		t.Errorf("DisabledFromStatus: got %q, want proposed", sk.DisabledFromStatus)
	}
}

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestChannelMemberDraftCurrentStepClampsBounds(t *testing.T) {
	d := channelMemberDraft{Step: -5}
	if got := d.currentStep(); got != "slug" {
		t.Fatalf("negative Step should clamp to first step, got %q", got)
	}
	d = channelMemberDraft{Step: 99}
	if got := d.currentStep(); got != "permission" {
		t.Fatalf("out-of-range Step should clamp to last step, got %q", got)
	}
}

func TestChannelMemberDraftCurrentStepProgresses(t *testing.T) {
	want := []string{"slug", "name", "role", "expertise", "personality", "permission"}
	for i, expected := range want {
		got := channelMemberDraft{Step: i}.currentStep()
		if got != expected {
			t.Errorf("Step %d = %q, want %q", i, got, expected)
		}
	}
}

func TestNormalizeDraftSlugLowercasesAndDashes(t *testing.T) {
	cases := map[string]string{
		"Frontend":         "frontend",
		"  Customer Ops  ": "customer-ops",
		"data_eng":         "data-eng",
		"FOO_BAR  Baz":     "foo-bar--baz",
	}
	for input, want := range cases {
		if got := channelui.NormalizeDraftSlug(input); got != want {
			t.Errorf("channelui.NormalizeDraftSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseExpertiseInputDedupesAndTrims(t *testing.T) {
	got := channelui.ParseExpertiseInput("Go,  React, Go,  ,Postgres ,React")
	want := []string{"Go", "React", "Postgres"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("idx %d: got %q want %q", i, got[i], w)
		}
	}
}

func TestParseExpertiseInputEmpty(t *testing.T) {
	if got := channelui.ParseExpertiseInput(""); len(got) != 0 {
		t.Fatalf("empty input should yield empty slice, got %v", got)
	}
	if got := channelui.ParseExpertiseInput("   ,  ,"); len(got) != 0 {
		t.Fatalf("whitespace-only input should yield empty slice, got %v", got)
	}
}

func TestMemberDraftComposerLabelByStep(t *testing.T) {
	cases := map[string]string{
		"slug":        "New teammate slug",
		"name":        "New teammate name",
		"role":        "Teammate role/title",
		"expertise":   "Expertise list",
		"personality": "Personality",
		"permission":  "Permission mode",
	}
	for step, want := range cases {
		idx := 0
		for i, s := range memberDraftSteps {
			if s == step {
				idx = i
			}
		}
		got := memberDraftComposerLabel(channelMemberDraft{Step: idx})
		if got != want {
			t.Errorf("step %q: got %q want %q", step, got, want)
		}
	}
}

func TestMemberDraftStepHintCoversAllSteps(t *testing.T) {
	for i := range memberDraftSteps {
		hint := memberDraftStepHint(channelMemberDraft{Step: i})
		if strings.TrimSpace(hint) == "" {
			t.Errorf("step %d (%s) has empty hint", i, memberDraftSteps[i])
		}
	}
}

func TestRenderMemberDraftCardCreateMode(t *testing.T) {
	draft := channelMemberDraft{
		Mode: "create",
		Slug: "data-eng",
		Name: "Data Engineering",
		Step: 2,
	}
	got := stripANSI(renderMemberDraftCard(draft, 60))
	if !strings.Contains(got, "New teammate") {
		t.Fatalf("expected create-mode title, got %q", got)
	}
	if !strings.Contains(got, "data-eng") {
		t.Fatalf("expected slug to render, got %q", got)
	}
	if !strings.Contains(got, "Data Engineering") {
		t.Fatalf("expected name to render, got %q", got)
	}
}

func TestRenderMemberDraftCardEditMode(t *testing.T) {
	draft := channelMemberDraft{Mode: "edit", Slug: "fe", Name: "Frontend"}
	got := stripANSI(renderMemberDraftCard(draft, 80))
	if !strings.Contains(got, "Edit teammate") {
		t.Fatalf("expected edit-mode title, got %q", got)
	}
}

func TestRenderMemberDraftCardEnforcesMinimumWidth(t *testing.T) {
	draft := channelMemberDraft{Mode: "create"}
	if got := renderMemberDraftCard(draft, 10); got == "" {
		t.Fatalf("undersized card should still render with min width")
	}
}

func TestMutateOfficeMemberSpecErrorPathSurfacesBrokerBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/office-members" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("slug already taken"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)

	draft := channelMemberDraft{Mode: "create", Slug: "fe", Name: "Frontend", Role: "fe"}
	msg, ok := mutateOfficeMemberSpec(draft, "office")().(channelMemberDraftDoneMsg)
	if !ok {
		t.Fatalf("expected channelMemberDraftDoneMsg")
	}
	if msg.err == nil || msg.err.Error() != "slug already taken" {
		t.Fatalf("expected broker body in error, got %v", msg.err)
	}
}

func TestMutateOfficeMemberSpecCreateSendsExpectedPayload(t *testing.T) {
	var posted map[string]any
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/office-members" {
			_ = json.Unmarshal(body, &posted)
		}
		// Force the post-success path to fail BEFORE team.NewLauncher is reached
		// by returning 400 — this still exercises the JSON serialization branch.
		// We can't realistically run team.NewLauncher in a test environment.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("denied"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)

	draft := channelMemberDraft{
		Mode:           "create",
		Slug:           "data-eng",
		Name:           "Data Eng",
		Role:           "Engineer",
		Expertise:      "Go, SQL,Go",
		Personality:    "calm",
		PermissionMode: "auto",
	}
	mutateOfficeMemberSpec(draft, "office")()

	if posted["action"] != "create" {
		t.Errorf("expected action=create, got %v", posted["action"])
	}
	if posted["slug"] != "data-eng" {
		t.Errorf("expected slug=data-eng, got %v", posted["slug"])
	}
	expertise, ok := posted["expertise"].([]any)
	if !ok {
		t.Fatalf("expertise should be slice, got %T", posted["expertise"])
	}
	if len(expertise) != 2 || expertise[0] != "Go" || expertise[1] != "SQL" {
		t.Errorf("expected dedup'd expertise [Go, SQL], got %v", expertise)
	}
	if posted["permission_mode"] != "auto" {
		t.Errorf("expected permission_mode=auto, got %v", posted["permission_mode"])
	}
}

func TestMutateOfficeMemberSpecEditUsesUpdateAction(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &posted)
		// Force error path to skip the launcher reconfigure.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)

	draft := channelMemberDraft{Mode: "edit", Slug: "fe", Name: "Frontend"}
	mutateOfficeMemberSpec(draft, "office")()
	if posted["action"] != "update" {
		t.Errorf("expected action=update, got %v", posted["action"])
	}
}

package onboarding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleStateGETReturnsValidJSON verifies that GET /onboarding/state
// returns HTTP 200 with a valid JSON body that can be decoded into State.
func TestHandleStateGETReturnsValidJSON(t *testing.T) {
	withTempHome(t, func(_ string) {
		req := httptest.NewRequest(http.MethodGet, "/onboarding/state", nil)
		w := httptest.NewRecorder()
		HandleState(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
		}
		var s State
		if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
			t.Fatalf("response is not valid State JSON: %v\nbody: %s", err, w.Body.String())
		}
		if s.Version != currentStateVersion {
			t.Errorf("Version: got %d, want %d", s.Version, currentStateVersion)
		}
	})
}

// TestHandleStateMethodNotAllowed verifies POST is rejected.
func TestHandleStateMethodNotAllowed(t *testing.T) {
	withTempHome(t, func(_ string) {
		req := httptest.NewRequest(http.MethodPost, "/onboarding/state", nil)
		w := httptest.NewRecorder()
		HandleState(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}

// TestHandleProgressPOSTPersists verifies that a POST to /onboarding/progress
// with step+answers persists the partial state.
func TestHandleProgressPOSTPersists(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{
			"step":    "welcome",
			"answers": map[string]interface{}{"company_name": "Initech"},
		}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/progress", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleProgress(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}

		// Verify the state was actually persisted.
		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.Partial == nil {
			t.Fatal("Partial should not be nil after saving progress")
		}
		if s.Partial.Step != "welcome" {
			t.Errorf("Partial.Step: got %q, want %q", s.Partial.Step, "welcome")
		}
		if s.Partial.Answers["welcome"]["company_name"] != "Initech" {
			t.Errorf("expected company_name=Initech in partial answers")
		}
	})
}

func TestHandleProgressAcceptsLegacyFlatShape(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{
			"step":        "setup",
			"company":     "Initech",
			"description": "Workflow consulting",
			"priority":    "Ship the first lane",
		}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/progress", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleProgress(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.Partial == nil {
			t.Fatal("Partial should not be nil after saving progress")
		}
		if got := s.Partial.Answers["setup"]["company"]; got != "Initech" {
			t.Fatalf("expected company=Initech in legacy partial answers, got %#v", got)
		}
	})
}

// TestHandleProgressMissingStep verifies that a missing step field returns 400.
func TestHandleProgressMissingStep(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{"answers": map[string]interface{}{}}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/progress", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleProgress(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

// TestHandleCompletePostIdempotent verifies that a second POST after onboarding
// is already complete returns {"already_completed": true}.
func TestHandleCompletePostIdempotent(t *testing.T) {
	withTempHome(t, func(_ string) {
		// Seed an already-complete state.
		s := &State{
			CompletedAt: time.Now().UTC().Format(time.RFC3339),
			Version:     currentStateVersion,
			CompanyName: "Initech",
			Checklist:   DefaultChecklist(),
		}
		if err := Save(s); err != nil {
			t.Fatalf("Save: %v", err)
		}

		body := map[string]interface{}{"task": "some task", "skip_task": false}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["already_completed"] != true {
			t.Errorf("expected already_completed=true, got: %v", resp)
		}
		if resp["redirect"] != "/" {
			t.Errorf("expected redirect=/, got: %v", resp["redirect"])
		}
	})
}

// TestHandleCompletePostEmptyTaskReturns400 verifies that an empty task
// without skip_task=true is rejected.
func TestHandleCompletePostEmptyTaskReturns400(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{"task": "", "skip_task": false}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})
}

// TestHandleCompletePostSkipTaskBypassesEmptyTask verifies that skip_task=true
// succeeds even when task is empty.
func TestHandleCompletePostSkipTaskBypassesEmptyTask(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{"task": "", "skip_task": true}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)
		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["ok"] != true {
			t.Errorf("expected ok=true, got: %v", resp)
		}
	})
}

// TestHandleCompletePostPersistsCompletedState verifies that after a successful
// complete, state.Onboarded() returns true.
func TestHandleCompletePostPersistsCompletedState(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{"task": "Write the landing page", "skip_task": false}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d\nbody: %s", w.Code, w.Body.String())
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !s.Onboarded() {
			t.Error("state should be onboarded after HandleComplete")
		}
	})
}

// TestHandleCompleteDecodesBlueprintAndAgents verifies that POST
// /onboarding/complete now decodes the blueprint id and selected agent
// slugs from the body and threads them into completeFn. Previously these
// fields were silently dropped, causing every user's team to collapse to
// the DefaultManifest roster regardless of what they picked in the wizard.
func TestHandleCompleteDecodesBlueprintAndAgents(t *testing.T) {
	withTempHome(t, func(_ string) {
		var gotTask, gotBlueprint string
		var gotSkipTask bool
		var gotAgents []string
		captured := func(task string, skipTask bool, blueprintID string, selectedAgents []string) error {
			gotTask = task
			gotSkipTask = skipTask
			gotBlueprint = blueprintID
			gotAgents = selectedAgents
			return nil
		}

		body := map[string]interface{}{
			"task":      "Stand up niche CRM",
			"skip_task": false,
			"blueprint": "niche-crm",
			"agents":    []string{"operator", "builder"},
		}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, captured)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}
		if gotTask != "Stand up niche CRM" {
			t.Errorf("task: got %q want %q", gotTask, "Stand up niche CRM")
		}
		if gotSkipTask {
			t.Errorf("skipTask: got true want false")
		}
		if gotBlueprint != "niche-crm" {
			t.Errorf("blueprint: got %q want %q", gotBlueprint, "niche-crm")
		}
		if len(gotAgents) != 2 || gotAgents[0] != "operator" || gotAgents[1] != "builder" {
			t.Errorf("agents: got %v want [operator builder]", gotAgents)
		}
	})
}

// TestHandleCompleteBackwardCompatWithLegacyClient verifies that a POST
// body without the new blueprint/agents fields (e.g. from an older client)
// is still accepted, with blueprintID empty and selectedAgents nil. The
// downstream onboardingCompleteFn must treat these as "from scratch".
func TestHandleCompleteBackwardCompatWithLegacyClient(t *testing.T) {
	withTempHome(t, func(_ string) {
		var gotBlueprint string
		var gotAgents []string
		captured := func(task string, skipTask bool, blueprintID string, selectedAgents []string) error {
			gotBlueprint = blueprintID
			gotAgents = selectedAgents
			return nil
		}

		body := map[string]interface{}{"task": "go", "skip_task": false}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, captured)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, body: %s", w.Code, w.Body.String())
		}
		if gotBlueprint != "" {
			t.Errorf("blueprint: got %q want empty", gotBlueprint)
		}
		if len(gotAgents) != 0 {
			t.Errorf("agents: got %v want empty/nil", gotAgents)
		}
	})
}

// TestHandleCompleteReturns500OnCompleteFnError verifies that if
// completeFn returns an error (e.g. LoadBlueprint failed), the handler
// returns HTTP 500 so the wizard can surface the error to the user.
// The response body must NOT include the wrapped error detail (which can
// carry filesystem paths, yaml parse messages, or user-supplied ids);
// those stay server-side in logs.
func TestHandleCompleteReturns500OnCompleteFnError(t *testing.T) {
	withTempHome(t, func(_ string) {
		const secretDetail = "secret-path-/etc/wuphf/state.yaml"
		failing := func(task string, skipTask bool, blueprintID string, selectedAgents []string) error {
			return fmt.Errorf("%s: simulated loader failure for %q", secretDetail, blueprintID)
		}

		body := map[string]interface{}{"task": "go", "skip_task": false, "blueprint": "bogus"}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, failing)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d want 500\nbody: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), secretDetail) {
			t.Errorf("500 body leaked internal error detail; body: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "bogus") {
			t.Errorf("500 body echoed user-supplied blueprint id; body: %s", w.Body.String())
		}
	})
}

// TestHandleCompleteSkipTaskPersistsOnboardedState verifies that
// skip_task=true still flips the state to onboarded on disk, so a user
// who opts out of the first-task prompt does not re-enter the wizard on
// next launch. This was a gap caught in review — the in-memory seeding
// was verified but disk persistence was not.
func TestHandleCompleteSkipTaskPersistsOnboardedState(t *testing.T) {
	withTempHome(t, func(_ string) {
		body := map[string]interface{}{"task": "", "skip_task": true, "blueprint": "", "agents": nil}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d\nbody: %s", w.Code, w.Body.String())
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !s.Onboarded() {
			t.Error("expected Onboarded()=true after skip_task=true; wizard would reopen on next launch")
		}
	})
}

// TestHandleChecklistDoneMarksItem verifies that POST /onboarding/checklist/{id}/done
// marks the item and persists it.
func TestHandleChecklistDoneMarksItem(t *testing.T) {
	withTempHome(t, func(_ string) {
		if err := Save(&State{Version: currentStateVersion, Checklist: DefaultChecklist()}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/onboarding/checklist/pick_team/done", nil)
		w := httptest.NewRecorder()
		HandleChecklistDone(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d\nbody: %s", w.Code, w.Body.String())
		}

		s, _ := Load()
		for _, item := range s.Checklist {
			if item.ID == "pick_team" && !item.Done {
				t.Error("pick_team should be done")
			}
		}
	})
}

// TestHandleChecklistDismiss verifies that POST /onboarding/checklist/dismiss
// sets ChecklistDismissed.
func TestHandleChecklistDismiss(t *testing.T) {
	withTempHome(t, func(_ string) {
		if err := Save(&State{Version: currentStateVersion, Checklist: DefaultChecklist()}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/onboarding/checklist/dismiss", nil)
		w := httptest.NewRecorder()
		HandleChecklistDismiss(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d\nbody: %s", w.Code, w.Body.String())
		}

		s, _ := Load()
		if !s.ChecklistDismissed {
			t.Error("ChecklistDismissed should be true")
		}
	})
}

// TestRegisterRoutesRegistersAllPaths verifies that RegisterRoutes wires
// the expected five routes.
func TestRegisterRoutesRegistersAllPaths(t *testing.T) {
	withTempHome(t, func(_ string) {
		mux := http.NewServeMux()
		RegisterRoutes(mux, nil, "", nil)

		routes := []struct {
			method string
			path   string
			want   int
		}{
			{http.MethodGet, "/onboarding/state", http.StatusOK},
			{http.MethodPost, "/onboarding/progress", http.StatusBadRequest}, // missing step
			{http.MethodPost, "/onboarding/complete", http.StatusBadRequest}, // missing task
			{http.MethodPost, "/onboarding/checklist/discord/done", http.StatusOK},
			{http.MethodPost, "/onboarding/checklist/dismiss", http.StatusOK},
		}

		// Ensure the state file exists before hitting routes that need it.
		if err := Save(&State{Version: currentStateVersion, Checklist: DefaultChecklist()}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		for _, tc := range routes {
			var body bytes.Buffer
			if tc.method == http.MethodPost && tc.path == "/onboarding/progress" {
				// Send a body with an empty step so we get a predictable 400.
				json.NewEncoder(&body).Encode(map[string]interface{}{"answers": map[string]interface{}{}})
			}
			if tc.method == http.MethodPost && tc.path == "/onboarding/complete" {
				json.NewEncoder(&body).Encode(map[string]interface{}{"task": "", "skip_task": false})
			}
			req := httptest.NewRequest(tc.method, tc.path, &body)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("%s %s: status %d, want %d (body: %s)",
					tc.method, tc.path, w.Code, tc.want, w.Body.String())
			}
		}
	})
}

package onboarding

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		RegisterRoutes(mux, nil)

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

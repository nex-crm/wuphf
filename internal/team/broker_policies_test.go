package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecordPolicy_AssignsStableID pins that newly-recorded policies
// receive a non-empty ID with the documented "policy-" prefix and that
// re-recording the same rule (case-insensitive) reactivates and reuses
// the original ID rather than minting a duplicate.
func TestRecordPolicy_AssignsStableID(t *testing.T) {
	b := newTestBroker(t)

	first, err := b.RecordPolicy("human_directed", "Always announce releases in #general")
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	if !strings.HasPrefix(first.ID, "policy-") {
		t.Errorf("expected ID prefix 'policy-', got %q", first.ID)
	}
	if !first.Active {
		t.Errorf("expected new policy to be Active")
	}

	// Same rule, different case: must dedupe and re-use original ID.
	second, err := b.RecordPolicy("human_directed", "ALWAYS ANNOUNCE RELEASES IN #GENERAL")
	if err != nil {
		t.Fatalf("re-record: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("expected dedupe by rule (case-insensitive); first=%q second=%q", first.ID, second.ID)
	}

	// Empty rule must error.
	if _, err := b.RecordPolicy("human_directed", "   "); err == nil {
		t.Error("expected error on empty rule")
	}
}

// TestListPolicies_ReturnsCopyNotInternalSlice guards against an
// aliasing regression: a caller mutating the returned slice (e.g.
// nil-ing out an entry) must NOT corrupt b.policies.
func TestListPolicies_ReturnsCopyNotInternalSlice(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.RecordPolicy("human_directed", "rule one"); err != nil {
		t.Fatalf("RecordPolicy: %v", err)
	}
	got := b.ListPolicies()
	if len(got) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(got))
	}
	// Mutate the returned copy.
	got[0].Rule = "mutated"

	// Internal store must be untouched.
	again := b.ListPolicies()
	if again[0].Rule == "mutated" {
		t.Error("ListPolicies returned aliased slice — caller mutation leaked into broker state")
	}
}

// TestHandlePolicies_GETReturnsJSONList pins the wire shape of GET
// /policies: a JSON object with key "policies" carrying an array of
// active officePolicy records. Inactive policies must be filtered.
func TestHandlePolicies_GETReturnsJSONList(t *testing.T) {
	b := newTestBroker(t)
	active, _ := b.RecordPolicy("human_directed", "active rule")
	inactive, _ := b.RecordPolicy("human_directed", "soon-deactivated rule")
	b.mu.Lock()
	for i := range b.policies {
		if b.policies[i].ID == inactive.ID {
			b.policies[i].Active = false
		}
	}
	b.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/policies", nil)
	rec := httptest.NewRecorder()
	b.handlePolicies(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", got)
	}

	var resp struct {
		Policies []officePolicy `json:"policies"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Policies) != 1 || resp.Policies[0].ID != active.ID {
		t.Errorf("expected only the active policy; got %+v", resp.Policies)
	}
}

// TestHandlePolicies_POSTPersistsRule verifies that POST /policies
// stores the rule and the same rule appears on a follow-up GET, so the
// happy path is locked end-to-end.
func TestHandlePolicies_POSTPersistsRule(t *testing.T) {
	b := newTestBroker(t)

	body := bytes.NewBufferString(`{"source":"human_directed","rule":"never push to main on Friday"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/policies", body)
	postRec := httptest.NewRecorder()
	b.handlePolicies(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST: expected 200, got %d: %s", postRec.Code, postRec.Body.String())
	}
	var posted officePolicy
	if err := json.NewDecoder(postRec.Body).Decode(&posted); err != nil {
		t.Fatalf("POST decode: %v", err)
	}
	if posted.Rule != "never push to main on Friday" {
		t.Errorf("posted rule mismatch: %q", posted.Rule)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/policies", nil)
	getRec := httptest.NewRecorder()
	b.handlePolicies(getRec, getReq)
	var got struct {
		Policies []officePolicy `json:"policies"`
	}
	_ = json.NewDecoder(getRec.Body).Decode(&got)
	if len(got.Policies) != 1 || got.Policies[0].ID != posted.ID {
		t.Errorf("expected GET to surface the POSTed rule, got %+v", got.Policies)
	}

	// Empty rule must 400.
	bad := bytes.NewBufferString(`{"source":"human_directed","rule":"   "}`)
	badReq := httptest.NewRequest(http.MethodPost, "/policies", bad)
	badRec := httptest.NewRecorder()
	b.handlePolicies(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty rule, got %d", badRec.Code)
	}
}

package team

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// TestListPolicies_ReturnsDistinctSlice guards against backing-array
// aliasing: a caller appending to the returned slice must NOT extend
// or corrupt b.policies. Element-level aliasing is impossible today
// because officePolicy has no pointer/slice/map fields, but the
// backing-array share is a real risk if a future refactor returns
// b.policies directly.
func TestListPolicies_ReturnsDistinctSlice(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.RecordPolicy("human_directed", "rule one"); err != nil {
		t.Fatalf("RecordPolicy: %v", err)
	}
	got := b.ListPolicies()
	if len(got) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(got))
	}
	// Distinct backing array: appending must not affect b.policies.
	_ = append(got, officePolicy{ID: "leaked", Rule: "should not appear"})

	again := b.ListPolicies()
	if len(again) != 1 {
		t.Errorf("internal slice grew via aliased append: now %d entries", len(again))
	}
	for _, p := range again {
		if p.ID == "leaked" {
			t.Errorf("internal slice contains leaked entry from append on returned slice")
		}
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

// TestHandlePolicies_RejectsOversizedBody pins the 16 KiB body cap on
// POST /policies. A policy rule + source string fits under 4 KiB;
// anything larger is a bug, malformed paste, or hostile input.
//
// Accept either 413 or 400 — see PAM's pattern at pam_test.go:691. The
// cap is enforced either way; the test pins "cap is enforced".
func TestHandlePolicies_RejectsOversizedBody(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handlePolicies))
	defer srv.Close()

	oversize := bytes.Repeat([]byte("x"), policyRequestMaxBodyBytes+1)
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(oversize))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 413 or 400 for oversized body, got %d", resp.StatusCode)
	}
}

// TestHandlePolicies_DELETE_DeactivatesByID pins both DELETE branches:
// id-from-body fallback and id-from-URL path. Active=false should flip
// for the matching policy without removing it (audit trail), and
// missing-id requests must 400 instead of silently no-opping.
func TestHandlePolicies_DELETE_DeactivatesByID(t *testing.T) {
	b := newTestBroker(t)
	posted, _ := b.RecordPolicy("human_directed", "soon to be retired")

	// id-from-body fallback (path is exactly /policies, no trailing id).
	delBody := bytes.NewBufferString(fmt.Sprintf(`{"id":%q}`, posted.ID))
	delReq := httptest.NewRequest(http.MethodDelete, "/policies", delBody)
	delRec := httptest.NewRecorder()
	b.handlePolicies(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE body-id: expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}
	if got := b.ListPolicies(); len(got) != 0 {
		t.Errorf("expected ListPolicies to filter out deactivated policy, got %d entries", len(got))
	}

	// id-from-URL path (`/policies/<id>`).
	posted2, _ := b.RecordPolicy("human_directed", "another retiree")
	pathReq := httptest.NewRequest(http.MethodDelete, "/policies/"+posted2.ID, nil)
	pathRec := httptest.NewRecorder()
	b.handlePolicies(pathRec, pathReq)
	if pathRec.Code != http.StatusOK {
		t.Fatalf("DELETE path-id: expected 200, got %d: %s", pathRec.Code, pathRec.Body.String())
	}

	// Missing id (empty body, /policies path) must 400.
	missing := httptest.NewRequest(http.MethodDelete, "/policies", bytes.NewBufferString(`{}`))
	missingRec := httptest.NewRecorder()
	b.handlePolicies(missingRec, missing)
	if missingRec.Code != http.StatusBadRequest {
		t.Errorf("DELETE missing-id: expected 400, got %d", missingRec.Code)
	}
}

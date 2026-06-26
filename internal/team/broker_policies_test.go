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
	active, err := b.RecordPolicy("human_directed", "active rule")
	if err != nil {
		t.Fatalf("RecordPolicy active: %v", err)
	}
	inactive, err := b.RecordPolicy("human_directed", "soon-deactivated rule")
	if err != nil {
		t.Fatalf("RecordPolicy inactive: %v", err)
	}
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
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		Policies []officePolicy `json:"policies"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
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

	// Send valid JSON with the rule field pushed past the cap so the
	// failure is unambiguously coming from MaxBytesReader, not from
	// json.Decoder bailing at byte 1 on garbage input.
	body := append([]byte(`{"source":"test","rule":"`), bytes.Repeat([]byte("a"), policyRequestMaxBodyBytes+1)...)
	body = append(body, []byte(`"}`)...)
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
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
	posted, err := b.RecordPolicy("human_directed", "soon to be retired")
	if err != nil {
		t.Fatalf("RecordPolicy: %v", err)
	}

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
	posted2, err := b.RecordPolicy("human_directed", "another retiree")
	if err != nil {
		t.Fatalf("RecordPolicy second: %v", err)
	}
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

// TestRecordPolicyScoped_AgentMergeSemantics pins the B3 scope-widening
// contract: scoped+scoped dedupe unions the lists; nil (= all agents) on
// either side of a dedupe dominates; whitespace differences in the rule
// text still dedupe.
func TestRecordPolicyScoped_AgentMergeSemantics(t *testing.T) {
	b := newTestBroker(t)

	first, err := b.RecordPolicyScoped("auto_detected", "Always CC the CSM on renewal emails", []string{"eng", "ENG", " "})
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	if len(first.Agents) != 1 || first.Agents[0] != "eng" {
		t.Fatalf("expected normalized agents [eng], got %v", first.Agents)
	}

	// Same rule with collapsed-whitespace + case difference, new agent:
	// dedupe and union the scopes.
	second, err := b.RecordPolicyScoped("auto_detected", "always   cc the CSM on renewal emails", []string{"ae"})
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected normalized-text dedupe, got new policy %q", second.ID)
	}
	if len(second.Agents) != 2 || second.Agents[0] != "ae" || second.Agents[1] != "eng" {
		t.Fatalf("expected union [ae eng], got %v", second.Agents)
	}

	// Nil scope (all agents) dominates on dedupe: the policy must never
	// silently narrow.
	third, err := b.RecordPolicyScoped("human_directed", "Always CC the CSM on renewal emails", nil)
	if err != nil {
		t.Fatalf("third record: %v", err)
	}
	if third.ID != first.ID || third.Agents != nil {
		t.Fatalf("expected nil (all-agents) scope to dominate, got %v", third.Agents)
	}
}

// TestPolicyAppliesToAgent pins the assignment filter: nil/empty = all
// agents; a non-empty list scopes to exactly those slugs.
func TestPolicyAppliesToAgent(t *testing.T) {
	all := officePolicy{Rule: "r"}
	if !policyAppliesToAgent(all, "eng") || !policyAppliesToAgent(all, "ceo") {
		t.Fatal("nil Agents must apply to everyone")
	}
	scoped := officePolicy{Rule: "r", Agents: []string{"eng"}}
	if !policyAppliesToAgent(scoped, "eng") {
		t.Fatal("scoped policy must apply to its agent")
	}
	if policyAppliesToAgent(scoped, "ceo") {
		t.Fatal("scoped policy must NOT apply to other agents")
	}
}

// TestHandlePolicies_POSTAcceptsAgents pins the additive `agents` wire key
// on POST /policies.
func TestHandlePolicies_POSTAcceptsAgents(t *testing.T) {
	b := newTestBroker(t)
	body := bytes.NewBufferString(`{"source":"human_directed","rule":"scoped rule","agents":["eng"]}`)
	req := httptest.NewRequest(http.MethodPost, "/policies", body)
	rec := httptest.NewRecorder()
	b.handlePolicies(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var posted officePolicy
	if err := json.NewDecoder(rec.Body).Decode(&posted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(posted.Agents) != 1 || posted.Agents[0] != "eng" {
		t.Fatalf("expected agents [eng] on the wire, got %v", posted.Agents)
	}
}

// TestHandlePoliciesSubpath_AssignUnassign drives the per-agent assignment
// endpoints (mirroring the skills enable-for/disable-for pattern):
// unassign on an all-agents policy materializes the roster minus the
// agent; assign adds an agent back; unassigning the last agent is rejected.
func TestHandlePoliciesSubpath_AssignUnassign(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "eng", Name: "Engineer"}}
	b.mu.Unlock()

	p, err := b.RecordPolicy("human_directed", "never deploy on Friday")
	if err != nil {
		t.Fatalf("RecordPolicy: %v", err)
	}

	call := func(verb, agent string) *httptest.ResponseRecorder {
		body := bytes.NewBufferString(fmt.Sprintf(`{"agent":%q}`, agent))
		req := httptest.NewRequest(http.MethodPost, "/policies/"+p.ID+"/"+verb, body)
		rec := httptest.NewRecorder()
		b.handlePoliciesSubpath(rec, req)
		return rec
	}

	// Unassign from an all-agents policy → roster minus the agent.
	rec := call("unassign", "eng")
	if rec.Code != http.StatusOK {
		t.Fatalf("unassign: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out officePolicy
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Agents) != 1 || out.Agents[0] != "ceo" {
		t.Fatalf("expected [ceo] after unassigning eng from all-agents policy, got %v", out.Agents)
	}

	// Assign eng back → union.
	rec = call("assign", "eng")
	if rec.Code != http.StatusOK {
		t.Fatalf("assign: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Agents) != 2 || out.Agents[0] != "ceo" || out.Agents[1] != "eng" {
		t.Fatalf("expected [ceo eng] after re-assign, got %v", out.Agents)
	}

	// Assigning a slug that is not in the roster is rejected.
	if rec := call("assign", "ghost"); rec.Code != http.StatusBadRequest {
		t.Fatalf("assign ghost: expected 400, got %d", rec.Code)
	}

	// Unassign down to one agent, then reject removing the last one (an
	// empty list would silently flip the policy back to all-agents).
	if rec := call("unassign", "eng"); rec.Code != http.StatusOK {
		t.Fatalf("unassign eng: expected 200, got %d", rec.Code)
	}
	if rec := call("unassign", "ceo"); rec.Code != http.StatusConflict {
		t.Fatalf("unassign last agent: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

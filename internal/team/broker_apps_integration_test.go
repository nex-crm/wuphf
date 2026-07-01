package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// TestProposeAppApprovalSpawnsAppBuilderTask locks in the implicit-intent gate:
// an agent's propose_app raises a NON-BLOCKING approval, and only on approve
// does the broker spawn a task owned by the App Builder. Drives the real HTTP
// handlers end-to-end (POST /requests -> POST /requests/answer).
func TestProposeAppApprovalSpawnsAppBuilderTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	// 1. propose_app -> POST /requests with an app_proposal payload.
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Build a new internal tool: Lead Scorer?",
		"question": "Build a new internal tool: Lead Scorer?",
		"blocking": false,
		"required": false,
		"app_proposal": map[string]any{
			"name":        "Lead Scorer",
			"icon":        "🎯",
			"summary":     "Score inbound leads",
			"description": "Rank inbound leads against our ICP weights.",
		},
	})
	created := postAppsJSON(t, base+"/requests", b.Token(), body)
	reqObj, _ := created["request"].(map[string]any)
	if reqObj == nil {
		t.Fatalf("no request in create response: %v", created)
	}
	// Non-blocking exemption: an app proposal keeps the approval options but
	// must NOT freeze the channel even though kind=approval.
	if blocking, _ := reqObj["blocking"].(bool); blocking {
		t.Fatalf("app proposal should be non-blocking, got blocking=true: %v", reqObj)
	}
	reqID, _ := reqObj["id"].(string)
	if reqID == "" {
		t.Fatalf("no request id: %v", reqObj)
	}

	// 2. Human approves -> POST /requests/answer with choice_id=approve.
	answer, _ := json.Marshal(map[string]any{"id": reqID, "choice_id": "approve"})
	postAppsJSON(t, base+"/requests/answer", b.Token(), answer)

	// 3. A task owned by the App Builder must now exist.
	b.mu.Lock()
	defer b.mu.Unlock()
	var found *teamTask
	for i := range b.tasks {
		if b.tasks[i].Owner == appBuilderSlug {
			found = &b.tasks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an app-builder task after approval; tasks=%+v", b.tasks)
	}
	if found.Title != "Build app: Lead Scorer" {
		t.Fatalf("task title = %q, want %q", found.Title, "Build app: Lead Scorer")
	}
}

// TestProposeAppRejectionSpawnsNoTask is the negative path: reject must not
// build anything.
func TestProposeAppRejectionSpawnsNoTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"question": "Build a new internal tool: Throwaway?",
		"blocking": false,
		"app_proposal": map[string]any{
			"name":        "Throwaway",
			"description": "Nope.",
		},
	})
	created := postAppsJSON(t, base+"/requests", b.Token(), body)
	reqObj, _ := created["request"].(map[string]any)
	reqID, _ := reqObj["id"].(string)

	answer, _ := json.Marshal(map[string]any{"id": reqID, "choice_id": "reject"})
	postAppsJSON(t, base+"/requests/answer", b.Token(), answer)

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].Owner == appBuilderSlug {
			t.Fatalf("rejected proposal must not create an app-builder task: %+v", b.tasks[i])
		}
	}
}

// TestRegisterAppRestrictedToAppBuilder locks the write gate: a random agent
// holding the broker token must not register apps directly; only the App Builder
// (or a human session) may. Drives POST /apps with the X-WUPHF-Agent header.
func TestRegisterAppRestrictedToAppBuilder(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"name": "Sneaky Tool",
		"html": validAppHTML,
	})

	// A non-app-builder agent is forbidden.
	if code := postAppsStatus(t, base+"/apps", b.Token(), "ceo", body); code != http.StatusForbidden {
		t.Fatalf("non-app-builder register: got %d, want 403", code)
	}
	// The App Builder is allowed.
	if code := postAppsStatus(t, base+"/apps", b.Token(), appBuilderSlug, body); code != http.StatusOK {
		t.Fatalf("app-builder register: got %d, want 200", code)
	}
}

// TestAppVersionEndpointsNonDestructive drives the version-timeline HTTP surface
// end-to-end: listing retained builds (structured, newest-first, current-flagged)
// and reading one past build's bytes for preview WITHOUT changing the current
// version. This is the new serving surface behind Phase 4's history timeline.
func TestAppVersionEndpointsNonDestructive(t *testing.T) {
	// Isolate the app store under a temp runtime home so the test never touches
	// a real ~/.wuphf/apps. Must be set before the first /apps call constructs
	// the store (appStore() reads CustomAppsRootDir lazily, via sync.Once).
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	htmlB := `<!doctype html><html><head></head><body><div id="root">B</div><script>var b=2;</script></body></html>`

	// Register v1, then update to v2 (both as the App Builder).
	v1Body, _ := json.Marshal(map[string]any{"name": "Lead Scorer", "html": validAppHTML})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, v1Body)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id in register response: %v", created)
	}
	v2Body, _ := json.Marshal(map[string]any{"id": id, "name": "Lead Scorer", "html": htmlB})
	postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, v2Body)

	// List → two structured versions, newest first, v2 current.
	status, list := getAppsJSON(t, base+"/apps/"+id+"/versions", b.Token())
	if status != http.StatusOK {
		t.Fatalf("list versions: %d", status)
	}
	versions, _ := list["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions = %v, want 2", versions)
	}
	first, _ := versions[0].(map[string]any)
	if v, _ := first["version"].(float64); int(v) != 2 {
		t.Fatalf("newest version = %v, want 2", first["version"])
	}
	if cur, _ := first["current"].(bool); !cur {
		t.Fatalf("newest version should be current: %v", first)
	}
	if by, _ := first["updatedBy"].(string); by != appBuilderSlug {
		t.Fatalf("newest updatedBy = %q, want %q", by, appBuilderSlug)
	}

	// Read v1's bytes for preview — flat shape, exact v1 bytes, NOT current.
	status, ver := getAppsJSON(t, base+"/apps/"+id+"/versions/1", b.Token())
	if status != http.StatusOK {
		t.Fatalf("get version 1: %d", status)
	}
	if html, _ := ver["html"].(string); html != validAppHTML {
		t.Fatalf("v1 html mismatch: %q", ver["html"])
	}
	if cur, _ := ver["current"].(bool); cur {
		t.Fatalf("v1 must not be flagged current: %v", ver)
	}

	// Non-destructive: the current build is still v2 after the preview read.
	status, curResp := getAppsJSON(t, base+"/apps/"+id, b.Token())
	if status != http.StatusOK {
		t.Fatalf("get current: %d", status)
	}
	curApp, _ := curResp["app"].(map[string]any)
	if v, _ := curApp["version"].(float64); int(v) != 2 {
		t.Fatalf("preview read changed current version to %v", curApp["version"])
	}
	if html, _ := curResp["html"].(string); html != htmlB {
		t.Fatalf("preview read changed the current bytes")
	}

	// Unknown version → 400-class caller error (consistent with rollback).
	status, _ = getAppsJSON(t, base+"/apps/"+id+"/versions/99", b.Token())
	if status != http.StatusBadRequest {
		t.Fatalf("unknown version status = %d, want 400", status)
	}
}

func getAppsJSON(t *testing.T, url, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

func postAppsAsAgent(t *testing.T, url, token, agentSlug string, body []byte) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WUPHF-Agent", agentSlug)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s -> %d: %s", url, resp.StatusCode, raw)
	}
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func postAppsStatus(t *testing.T, url, token, agentSlug string, body []byte) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if agentSlug != "" {
		req.Header.Set("X-WUPHF-Agent", agentSlug)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	return resp.StatusCode
}

func postAppsJSON(t *testing.T, url, token string, body []byte) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s -> %d: %s", url, resp.StatusCode, raw)
	}
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// TestAppDBEndpointBrokerTokenRoundTrip locks the DB write gate at the HTTP
// layer: the sandboxed app reaches the broker through the web proxy carrying the
// BROKER token (broker-kind, NOT a human session and NOT the app-builder agent),
// so a define → upsert → GET round-trip MUST succeed under a plain broker token.
// This is the exact caller a human/app-builder-only gate would wrongly reject —
// which would break the app writing its own data model in the browser.
func TestAppDBEndpointBrokerTokenRoundTrip(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	// Register an app (as the App Builder) so it has a manifest to attach a DB to.
	regBody, _ := json.Marshal(map[string]any{"name": "Data App", "html": validAppHTML})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, regBody)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id in register response: %v", created)
	}

	// Fresh app: GET returns empty tables (broker token, no agent header).
	status, empty := getAppsJSON(t, base+"/apps/"+id+"/db", b.Token())
	if status != http.StatusOK {
		t.Fatalf("GET db: %d", status)
	}
	if tables, _ := empty["tables"].([]any); len(tables) != 0 {
		t.Fatalf("fresh app tables = %v, want empty", empty["tables"])
	}

	// define + upsert with a PLAIN broker token (postAppsJSON sends no agent
	// header → broker-kind). Must succeed — this is the regression guard.
	defBody, _ := json.Marshal(map[string]any{
		"op":    "define",
		"table": "Emails",
		"columns": []map[string]any{
			{"name": "id", "type": "string"},
			{"name": "urgency", "type": "number"},
		},
	})
	postAppsJSON(t, base+"/apps/"+id+"/db", b.Token(), defBody)

	upBody, _ := json.Marshal(map[string]any{
		"op":    "upsert",
		"table": "Emails",
		"key":   "id",
		"rows": []map[string]any{
			{"id": "a", "urgency": 10},
			{"id": "a", "urgency": 99}, // same key in one batch → last wins
			{"id": "b", "urgency": 20},
		},
	})
	postAppsJSON(t, base+"/apps/"+id+"/db", b.Token(), upBody)

	// GET back: two rows (key dedup), and row "a" carries the replaced value.
	status, out := getAppsJSON(t, base+"/apps/"+id+"/db", b.Token())
	if status != http.StatusOK {
		t.Fatalf("GET db after write: %d", status)
	}
	tables, _ := out["tables"].([]any)
	if len(tables) != 1 {
		t.Fatalf("tables = %v, want 1", out["tables"])
	}
	tbl, _ := tables[0].(map[string]any)
	rows, _ := tbl["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want 2 after key dedup", rows)
	}
	var aURG float64
	var sawA bool
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if row["id"] == "a" {
			sawA = true
			aURG, _ = row["urgency"].(float64)
		}
	}
	if !sawA || aURG != 99 {
		t.Fatalf("row a urgency = %v (sawA=%v), want 99", aURG, sawA)
	}
}

// TestAppDBWriteThrottle locks the per-app write-frequency cap: a tight loop of
// DB writes is cut off at appDBWriteLimit per window with a 429 (the store's
// growth caps do not bound frequency), while reads stay unmetered.
func TestAppDBWriteThrottle(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	regBody, _ := json.Marshal(map[string]any{"name": "Loop App", "html": validAppHTML})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, regBody)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id in register response: %v", created)
	}

	defBody, _ := json.Marshal(map[string]any{
		"op": "define", "table": "T",
		"columns": []map[string]any{{"name": "id", "type": "string"}},
	})
	upBody, _ := json.Marshal(map[string]any{
		"op": "upsert", "table": "T", "key": "id",
		"rows": []map[string]any{{"id": "a"}},
	})

	// The define plus upserts up to the cap all pass…
	postAppsJSON(t, base+"/apps/"+id+"/db", b.Token(), defBody)
	for i := 1; i < appDBWriteLimit; i++ {
		if status := postAppsStatus(t, base+"/apps/"+id+"/db", b.Token(), "", upBody); status != http.StatusOK {
			t.Fatalf("write %d = %d, want 200 under the cap", i, status)
		}
	}
	// …the write over the cap is rejected with 429…
	if status := postAppsStatus(t, base+"/apps/"+id+"/db", b.Token(), "", upBody); status != http.StatusTooManyRequests {
		t.Fatalf("write over cap = %d, want 429", status)
	}
	// …and reads (the "query" op and GET) stay unmetered.
	qBody, _ := json.Marshal(map[string]any{"op": "query", "table": "T"})
	if status := postAppsStatus(t, base+"/apps/"+id+"/db", b.Token(), "", qBody); status != http.StatusOK {
		t.Fatalf("query while write-limited = %d, want 200", status)
	}
	if status, _ := getAppsJSON(t, base+"/apps/"+id+"/db", b.Token()); status != http.StatusOK {
		t.Fatalf("GET while write-limited = %d, want 200", status)
	}
}

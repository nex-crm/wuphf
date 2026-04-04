# Thread Lifecycle & Structured Handoffs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add thread conclusion lifecycle and structured handoffs so agents produce clean outputs for humans and transfer work with full context.

**Architecture:** Two new broker subsystems layered on existing message/task storage. Thread conclusions are dual-stored (structured object + `[CONCLUSION]` channel message). Handoffs are stored on tasks and trigger enriched notifications. Notification gating in the launcher skips concluded threads for auto-notifications but lets tagged/CEO messages through.

**Tech Stack:** Go, net/http, MCP (go-sdk), existing Broker/Launcher/MCP patterns.

**Spec:** `docs/2026-04-05-thread-lifecycle-handoffs-design.md`

**Working tree:** `/Users/najmuzzaman/Documents/nex/wuphf-telegram`
**Branch:** `nazz/research/open-multi-agent`
**Build:** `go build -o wuphf ./cmd/wuphf`
**Tests:** `go test ./...`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/team/broker.go` | Modify | Add `threadConclusion`, `conclusionSummary`, `taskHandoff` types. Add `conclusions` field to Broker + brokerState. Add `handleConclude`, `handleConcludeReopen`, `handleGetConclusions`, `handleHandoff` endpoints. Add `IsThreadConcluded()` public accessor. Wire into persistence. |
| `internal/team/broker_test.go` | Modify | Tests for conclusion CRUD, idempotency, authorization, handoff validation, persistence. |
| `internal/team/launcher.go` | Modify | Update `notificationTargetsForMessage()` to skip concluded threads (allow tagged through). Update `deliverTaskNotification()` to inject handoff context. Add `isThreadConcluded()` helper. |
| `internal/team/launcher_test.go` | Modify | Tests for concluded-thread notification suppression, tagged-message piercing, handoff notification injection. |
| `internal/teammcp/server.go` | Modify | Add `team_conclude`, `team_handoff`, `team_reopen` MCP tools. Add conclusion/handoff types. Update `team_poll` with conclusions + pending handoffs sections. |
| `internal/teammcp/server_test.go` | Modify | Tests for MCP tool handlers via real broker. |
| `internal/agent/prompts.go` | Modify | Add conclude/handoff instructions to CEO and specialist prompts. |
| `internal/agent/prompts_test.go` | Modify | Verify new prompt content appears. |

---

### Task 1: Thread Conclusion Data Model + Persistence

**Files:**
- Modify: `internal/team/broker.go` (types at ~line 87, Broker struct at ~line 289, brokerState at ~line 249, loadState at ~line 758, saveLocked at ~line 804)
- Test: `internal/team/broker_test.go`

- [ ] **Step 1: Write failing test — conclusion persists across broker reload**

```go
func TestConclusionPersistsAcrossReload(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.conclusions = []threadConclusion{{
		ThreadID:    "msg-1",
		Channel:     "general",
		Summary:     conclusionSummary{Discussed: "API design", Decided: "REST", Done: "Spec written", OpenItems: ""},
		ConcludedBy: "be",
		ConcludedAt: "2026-04-05T00:00:00Z",
	}}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := NewBroker()
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	if len(reloaded.conclusions) != 1 {
		t.Fatalf("expected 1 conclusion after reload, got %d", len(reloaded.conclusions))
	}
	if reloaded.conclusions[0].ThreadID != "msg-1" {
		t.Fatalf("expected thread_id msg-1, got %s", reloaded.conclusions[0].ThreadID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run TestConclusionPersistsAcrossReload -v`
Expected: FAIL — `conclusions` field undefined

- [ ] **Step 3: Add types and wire persistence**

In `broker.go`, add after the `teamTask` struct (around line 111):

```go
type conclusionSummary struct {
	Discussed string `json:"discussed"`
	Decided   string `json:"decided"`
	Done      string `json:"done"`
	OpenItems string `json:"open_items,omitempty"`
}

type threadConclusion struct {
	ThreadID    string            `json:"thread_id"`
	Channel     string            `json:"channel"`
	Summary     conclusionSummary `json:"summary"`
	ConcludedBy string            `json:"concluded_by"`
	ConcludedAt string            `json:"concluded_at"`
}

type taskHandoff struct {
	FromAgent string `json:"from_agent"`
	ToAgent   string `json:"to_agent"`
	WhatIDid  string `json:"what_i_did"`
	WhatToDo  string `json:"what_to_do"`
	Context   string `json:"context"`
	CreatedAt string `json:"created_at"`
}
```

Add `Handoffs []taskHandoff` field to `teamTask` struct.

Add `conclusions []threadConclusion` to `Broker` struct (after `skills`).

Add `Conclusions []threadConclusion` to `brokerState` struct.

In `loadState()`: add `b.conclusions = state.Conclusions` after `b.skills = state.Skills`.

In `saveLocked()`:
- Add `Conclusions: b.conclusions,` to the `brokerState{}` literal.
- Add `&& len(b.conclusions) == 0` to the empty-state check.

Add public accessor:

```go
func (b *Broker) IsThreadConcluded(channel, threadID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	for _, c := range b.conclusions {
		if normalizeChannelSlug(c.Channel) == channel && c.ThreadID == threadID {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run TestConclusionPersistsAcrossReload -v`
Expected: PASS

- [ ] **Step 5: Build to verify compilation**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`
Expected: Success

- [ ] **Step 6: Commit**

```bash
cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && git add internal/team/broker.go internal/team/broker_test.go && git commit -m "feat: thread conclusion + handoff data model and persistence"
```

---

### Task 2: Conclude Endpoint + Authorization

**Files:**
- Modify: `internal/team/broker.go` (add `handleConclude` handler, register route)
- Test: `internal/team/broker_test.go`

- [ ] **Step 1: Write failing test — POST /conclude creates conclusion**

```go
func TestHandleConcludeCreatesConclusion(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Post a message to create a thread root
	msg, err := b.PostMessage("fe", "general", "Let's build the landing page", nil, "")
	if err != nil {
		t.Fatalf("post message: %v", err)
	}

	// Conclude the thread
	body := fmt.Sprintf(`{"channel":"general","thread_id":"%s","discussed":"Landing page","decided":"3 sections","done":"Built and deployed","concluded_by":"fe"}`, msg.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/conclude", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /conclude: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !b.IsThreadConcluded("general", msg.ID) {
		t.Fatal("expected thread to be concluded")
	}
}
```

- [ ] **Step 2: Write failing test — non-participant cannot conclude**

```go
func TestHandleConcludeRejectsNonParticipant(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	msg, _ := b.PostMessage("fe", "general", "Thread root", nil, "")

	body := fmt.Sprintf(`{"channel":"general","thread_id":"%s","discussed":"X","decided":"Y","done":"Z","concluded_by":"be"}`, msg.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/conclude", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /conclude: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Write failing test — double conclude is idempotent**

```go
func TestHandleConcludeIdempotent(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	msg, _ := b.PostMessage("fe", "general", "Thread root", nil, "")
	conclude := func() int {
		body := fmt.Sprintf(`{"channel":"general","thread_id":"%s","discussed":"X","decided":"Y","done":"Z","concluded_by":"fe"}`, msg.ID)
		req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/conclude", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := conclude(); code != 200 {
		t.Fatalf("first conclude: expected 200, got %d", code)
	}
	if code := conclude(); code != 200 {
		t.Fatalf("second conclude: expected 200 (idempotent), got %d", code)
	}
	b.mu.Lock()
	count := 0
	for _, c := range b.conclusions {
		if c.ThreadID == msg.ID {
			count++
		}
	}
	b.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 conclusion (idempotent), got %d", count)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleConclude" -v`
Expected: FAIL — `handleConclude` undefined

- [ ] **Step 5: Implement handleConclude + register route**

In `broker.go`, add the route in `StartOnPort` (after `/tasks/ack`):

```go
mux.HandleFunc("/conclude", b.requireAuth(b.handleConclude))
mux.HandleFunc("/conclude/reopen", b.requireAuth(b.handleConcludeReopen))
mux.HandleFunc("/conclusions", b.requireAuth(b.handleGetConclusions))
```

Add handler:

```go
func (b *Broker) handleConclude(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel     string `json:"channel"`
		ThreadID    string `json:"thread_id"`
		Discussed   string `json:"discussed"`
		Decided     string `json:"decided"`
		Done        string `json:"done"`
		OpenItems   string `json:"open_items"`
		ConcludedBy string `json:"concluded_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	threadID := strings.TrimSpace(body.ThreadID)
	concludedBy := strings.TrimSpace(body.ConcludedBy)
	if threadID == "" || concludedBy == "" {
		http.Error(w, "thread_id and concluded_by required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Idempotency: return existing conclusion if already concluded
	for _, c := range b.conclusions {
		if normalizeChannelSlug(c.Channel) == channel && c.ThreadID == threadID {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"conclusion": c})
			return
		}
	}

	// Authorization: concludedBy must have posted in the thread
	participated := false
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			continue
		}
		if (msg.ID == threadID || msg.ReplyTo == threadID) && msg.From == concludedBy {
			participated = true
			break
		}
	}
	if !participated {
		http.Error(w, "only thread participants can conclude", http.StatusForbidden)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	conclusion := threadConclusion{
		ThreadID: threadID,
		Channel:  channel,
		Summary: conclusionSummary{
			Discussed: strings.TrimSpace(body.Discussed),
			Decided:   strings.TrimSpace(body.Decided),
			Done:      strings.TrimSpace(body.Done),
			OpenItems: strings.TrimSpace(body.OpenItems),
		},
		ConcludedBy: concludedBy,
		ConcludedAt: now,
	}
	b.conclusions = append(b.conclusions, conclusion)

	// Post a [CONCLUSION] message into the thread for TUI visibility
	b.counter++
	conclusionContent := fmt.Sprintf("[CONCLUSION] Discussed: %s | Decided: %s | Done: %s",
		conclusion.Summary.Discussed, conclusion.Summary.Decided, conclusion.Summary.Done)
	if conclusion.Summary.OpenItems != "" {
		conclusionContent += " | Open: " + conclusion.Summary.OpenItems
	}
	conclusionMsg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      concludedBy,
		Channel:   channel,
		Kind:      "conclusion",
		Content:   conclusionContent,
		ReplyTo:   threadID,
		Timestamp: now,
	}
	b.messages = append(b.messages, conclusionMsg)

	b.appendActionLocked("thread_concluded", "office", channel, concludedBy, truncateSummary("Concluded: "+conclusion.Summary.Done, 140), threadID)
	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"conclusion": conclusion})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleConclude" -v`
Expected: PASS (all 3 tests)

- [ ] **Step 7: Build**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 8: Commit**

```bash
git add internal/team/broker.go internal/team/broker_test.go && git commit -m "feat: POST /conclude endpoint with authorization and idempotency"
```

---

### Task 3: Reopen + Get Conclusions Endpoints

**Files:**
- Modify: `internal/team/broker.go`
- Test: `internal/team/broker_test.go`

- [ ] **Step 1: Write failing test — reopen removes conclusion**

```go
func TestHandleConcludeReopenRemovesConclusion(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	msg, _ := b.PostMessage("ceo", "general", "Thread", nil, "")

	// Conclude
	body := fmt.Sprintf(`{"channel":"general","thread_id":"%s","discussed":"X","decided":"Y","done":"Z","concluded_by":"ceo"}`, msg.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/conclude", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	http.DefaultClient.Do(req)

	// Reopen (CEO only)
	body = fmt.Sprintf(`{"channel":"general","thread_id":"%s","reason":"need more work","slug":"ceo"}`, msg.ID)
	req, _ = http.NewRequest("POST", "http://"+b.Addr()+"/conclude/reopen", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if b.IsThreadConcluded("general", msg.ID) {
		t.Fatal("expected thread to be reopened")
	}
}
```

- [ ] **Step 2: Write failing test — non-lead cannot reopen**

```go
func TestHandleConcludeReopenRejectsNonLead(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	msg, _ := b.PostMessage("fe", "general", "Thread", nil, "")
	body := fmt.Sprintf(`{"channel":"general","thread_id":"%s","discussed":"X","decided":"Y","done":"Z","concluded_by":"fe"}`, msg.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/conclude", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	http.DefaultClient.Do(req)

	body = fmt.Sprintf(`{"channel":"general","thread_id":"%s","reason":"reasons","slug":"fe"}`, msg.ID)
	req, _ = http.NewRequest("POST", "http://"+b.Addr()+"/conclude/reopen", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-lead reopen, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleConcludeReopen" -v`

- [ ] **Step 4: Implement handleConcludeReopen and handleGetConclusions**

`handleConcludeReopen`: Validates slug is the lead (check `officeLeadSlug()` pattern — need to add a helper or check against the first member with role "lead"). For simplicity, check that the slug matches the first agent in the default office members or is "ceo".

```go
func (b *Broker) handleConcludeReopen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel  string `json:"channel"`
		ThreadID string `json:"thread_id"`
		Reason   string `json:"reason"`
		Slug     string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	threadID := strings.TrimSpace(body.ThreadID)
	slug := strings.TrimSpace(body.Slug)
	if threadID == "" || slug == "" {
		http.Error(w, "thread_id and slug required", http.StatusBadRequest)
		return
	}

	// Only lead/CEO or human can reopen
	if slug != "ceo" && slug != "you" && !b.isLeadSlug(slug) {
		http.Error(w, "only the lead agent can reopen concluded threads", http.StatusForbidden)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	removed := false
	for i := range b.conclusions {
		if normalizeChannelSlug(b.conclusions[i].Channel) == channel && b.conclusions[i].ThreadID == threadID {
			b.conclusions = append(b.conclusions[:i], b.conclusions[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		http.Error(w, "thread not concluded", http.StatusNotFound)
		return
	}
	b.appendActionLocked("thread_reopened", "office", channel, slug, truncateSummary("Reopened: "+body.Reason, 140), threadID)
	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"reopened": true})
}

func (b *Broker) isLeadSlug(slug string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.members {
		if m.Slug == slug && strings.Contains(strings.ToLower(m.Role), "lead") {
			return true
		}
	}
	return false
}
```

Wait — `isLeadSlug` takes the lock but `handleConcludeReopen` also takes the lock. Move the lead check before the lock, or make it lockless. Better: check slug before locking, use an unlocked helper.

Correct approach: check `slug == "ceo" || slug == "you"` before the lock (these are the known lead/human slugs). For a general lead check, read the pack config. But the broker doesn't have the pack. Simplest: allow "ceo" and "you" (the human) to reopen. This matches the spec ("CEO or human").

```go
func (b *Broker) handleConcludeReopen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel  string `json:"channel"`
		ThreadID string `json:"thread_id"`
		Reason   string `json:"reason"`
		Slug     string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	threadID := strings.TrimSpace(body.ThreadID)
	slug := strings.TrimSpace(body.Slug)
	if threadID == "" || slug == "" {
		http.Error(w, "thread_id and slug required", http.StatusBadRequest)
		return
	}
	if slug != "ceo" && slug != "you" {
		http.Error(w, "only the CEO or human can reopen concluded threads", http.StatusForbidden)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	removed := false
	for i := range b.conclusions {
		if normalizeChannelSlug(b.conclusions[i].Channel) == channel && b.conclusions[i].ThreadID == threadID {
			b.conclusions = append(b.conclusions[:i], b.conclusions[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		http.Error(w, "thread not concluded", http.StatusNotFound)
		return
	}
	b.appendActionLocked("thread_reopened", "office", channel, slug, truncateSummary("Reopened: "+body.Reason, 140), threadID)
	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"reopened": true})
}
```

Add `handleGetConclusions`:

```go
func (b *Broker) handleGetConclusions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "general"
	}
	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}

	b.mu.Lock()
	result := make([]threadConclusion, 0)
	for _, c := range b.conclusions {
		if normalizeChannelSlug(c.Channel) != channel {
			continue
		}
		if threadID != "" && c.ThreadID != threadID {
			continue
		}
		result = append(result, c)
		if len(result) >= limit {
			break
		}
	}
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"conclusions": result})
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleConcludeReopen" -v`
Expected: PASS

- [ ] **Step 6: Build**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 7: Commit**

```bash
git add internal/team/broker.go internal/team/broker_test.go && git commit -m "feat: POST /conclude/reopen and GET /conclusions endpoints"
```

---

### Task 4: Handoff Endpoint

**Files:**
- Modify: `internal/team/broker.go`
- Test: `internal/team/broker_test.go`

- [ ] **Step 1: Write failing test — handoff transfers task ownership**

```go
func TestHandleHandoffTransfersOwnership(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	task, _, _ := b.EnsureTask("general", "Build landing page", "details", "designer", "ceo", "")

	body := fmt.Sprintf(`{"channel":"general","task_id":"%s","from_agent":"designer","to_agent":"fe","what_i_did":"Figma spec done","what_to_do":"Build HTML","context":"Mobile-first"}`, task.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/handoff", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /handoff: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	// Verify ownership transferred
	tasks := b.ChannelTasks("general")
	for _, tt := range tasks {
		if tt.ID == task.ID {
			if tt.Owner != "fe" {
				t.Fatalf("expected owner fe, got %s", tt.Owner)
			}
			if len(tt.Handoffs) != 1 {
				t.Fatalf("expected 1 handoff, got %d", len(tt.Handoffs))
			}
			if tt.Handoffs[0].WhatIDid != "Figma spec done" {
				t.Fatalf("handoff what_i_did mismatch")
			}
			return
		}
	}
	t.Fatal("task not found")
}
```

- [ ] **Step 2: Write failing test — non-owner cannot handoff**

```go
func TestHandleHandoffRejectsNonOwner(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	task, _, _ := b.EnsureTask("general", "Build landing page", "", "designer", "ceo", "")

	body := fmt.Sprintf(`{"channel":"general","task_id":"%s","from_agent":"be","to_agent":"fe","what_i_did":"X","what_to_do":"Y","context":"Z"}`, task.ID)
	req, _ := http.NewRequest("POST", "http://"+b.Addr()+"/handoff", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner handoff, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleHandoff" -v`

- [ ] **Step 4: Implement handleHandoff**

Register route in `StartOnPort` (after `/conclude/reopen`):

```go
mux.HandleFunc("/handoff", b.requireAuth(b.handleHandoff))
```

Add handler:

```go
func (b *Broker) handleHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel   string `json:"channel"`
		TaskID    string `json:"task_id"`
		FromAgent string `json:"from_agent"`
		ToAgent   string `json:"to_agent"`
		WhatIDid  string `json:"what_i_did"`
		WhatToDo  string `json:"what_to_do"`
		Context   string `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	taskID := strings.TrimSpace(body.TaskID)
	fromAgent := strings.TrimSpace(body.FromAgent)
	toAgent := strings.TrimSpace(body.ToAgent)
	if taskID == "" || fromAgent == "" || toAgent == "" {
		http.Error(w, "task_id, from_agent, and to_agent required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Validate to_agent is enabled in channel
	enabled := false
	for _, member := range b.enabledMembersLocked(channel) {
		if member == toAgent {
			enabled = true
			break
		}
	}
	if !enabled {
		http.Error(w, "to_agent is not an enabled member of the channel", http.StatusBadRequest)
		return
	}

	for i := range b.tasks {
		if b.tasks[i].ID != taskID || normalizeChannelSlug(b.tasks[i].Channel) != channel {
			continue
		}
		task := &b.tasks[i]
		if task.Owner != fromAgent {
			http.Error(w, "only the task owner can handoff", http.StatusForbidden)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		handoff := taskHandoff{
			FromAgent: fromAgent,
			ToAgent:   toAgent,
			WhatIDid:  strings.TrimSpace(body.WhatIDid),
			WhatToDo:  strings.TrimSpace(body.WhatToDo),
			Context:   strings.TrimSpace(body.Context),
			CreatedAt: now,
		}
		task.Handoffs = append(task.Handoffs, handoff)
		task.Owner = toAgent
		task.AckedAt = "" // reset ack for new owner
		task.UpdatedAt = now
		b.appendActionLocked("task_handoff", "office", channel, fromAgent, truncateSummary(fmt.Sprintf("Handed off to @%s: %s", toAgent, task.Title), 140), task.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"task": *task})
		return
	}
	http.Error(w, "task not found", http.StatusNotFound)
}
```

Note: Need to check if `enabledMembersLocked` exists or use the unlocked version. The existing `EnabledMembers` takes the lock itself. Add a lockless helper:

```go
func (b *Broker) enabledMembersLocked(channel string) []string {
	channel = normalizeChannelSlug(channel)
	ch := b.findChannelLocked(channel)
	if ch == nil {
		return nil
	}
	disabled := make(map[string]struct{}, len(ch.Disabled))
	for _, d := range ch.Disabled {
		disabled[d] = struct{}{}
	}
	if len(ch.Members) > 0 {
		out := make([]string, 0, len(ch.Members))
		for _, m := range ch.Members {
			if _, ok := disabled[m]; !ok {
				out = append(out, m)
			}
		}
		return out
	}
	out := make([]string, 0, len(b.members))
	for _, m := range b.members {
		if _, ok := disabled[m.Slug]; !ok {
			out = append(out, m.Slug)
		}
	}
	return out
}
```

Check whether this helper already exists by searching for `enabledMembersLocked` or the implementation of `EnabledMembers`. If `EnabledMembers` contains the logic, extract the locked version from it.

- [ ] **Step 5: Run tests**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestHandleHandoff" -v`
Expected: PASS

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -v`
Expected: All pass

- [ ] **Step 7: Build**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 8: Commit**

```bash
git add internal/team/broker.go internal/team/broker_test.go && git commit -m "feat: POST /handoff endpoint with ownership transfer and validation"
```

---

### Task 5: Notification Gating for Concluded Threads

**Files:**
- Modify: `internal/team/launcher.go` (lines 673-748)
- Test: `internal/team/launcher_test.go`

- [ ] **Step 1: Write failing test — concluded thread suppresses auto-notifications**

```go
func TestNotificationSuppressedForConcludedThread(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	// Post a thread root and a reply
	root, _ := b.PostMessage("fe", "general", "Thread root", nil, "")
	b.PostMessage("be", "general", "Reply in thread", nil, root.ID)

	// Conclude the thread
	b.mu.Lock()
	b.conclusions = append(b.conclusions, threadConclusion{
		ThreadID: root.ID, Channel: "general", ConcludedBy: "fe",
		Summary: conclusionSummary{Discussed: "X", Decided: "Y", Done: "Z"},
		ConcludedAt: "2026-04-05T00:00:00Z",
	})
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "FE"},
				{Slug: "be", Name: "BE"},
			},
		},
	}

	// New message in the concluded thread (no tags)
	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "designer",
		Channel: "general",
		Content: "Late reply",
		ReplyTo: root.ID,
	})
	// Should suppress all (no tags, thread is concluded)
	if len(immediate) != 0 || len(delayed) != 0 {
		t.Fatalf("expected no notifications for concluded thread, got immediate=%d delayed=%d", len(immediate), len(delayed))
	}
}
```

- [ ] **Step 2: Write failing test — tagged message pierces concluded barrier**

```go
func TestTaggedMessagePiercesConcludedThread(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	root, _ := b.PostMessage("fe", "general", "Thread root", nil, "")
	b.mu.Lock()
	b.conclusions = append(b.conclusions, threadConclusion{
		ThreadID: root.ID, Channel: "general", ConcludedBy: "fe",
		Summary: conclusionSummary{Discussed: "X", Decided: "Y", Done: "Z"},
		ConcludedAt: "2026-04-05T00:00:00Z",
	})
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "FE"},
				{Slug: "be", Name: "BE"},
			},
		},
	}

	// Tagged message in concluded thread — should still deliver
	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: "@fe check the copy",
		Tagged:  []string{"fe"},
		ReplyTo: root.ID,
	})
	// CEO is sender so excluded from immediate, but this is a CEO message → broadcasts to all
	// (broadcastAll = true because msg.From == lead)
	if len(delayed) == 0 {
		t.Fatal("expected tagged message to pierce concluded barrier")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestNotification.*Concluded|TestTagged.*Concluded" -v`

- [ ] **Step 4: Add concluded-thread check to notificationTargetsForMessage**

In `launcher.go`, add the helper:

```go
func (l *Launcher) isThreadConcluded(channel, threadID string) bool {
	if l.broker == nil || threadID == "" {
		return false
	}
	return l.broker.IsThreadConcluded(channel, threadID)
}
```

In `notificationTargetsForMessage`, add a check after the `broadcastAll` assignment and before the tag check. Insert at line ~722:

```go
	// Concluded thread gating: suppress auto-notifications but allow tagged/CEO through
	threadRoot := msg.ReplyTo
	if threadRoot == "" {
		threadRoot = msg.ID // message might BE the root
	}
	concluded := l.isThreadConcluded(msg.Channel, msg.ReplyTo)

	if concluded && !broadcastAll && len(msg.Tagged) == 0 {
		// Concluded thread, no explicit tags, not from lead → suppress all
		return nil, nil
	}
```

This goes right after `broadcastAll := msg.From == lead` and before the tag check. The tag check and broadcastAll check proceed normally if the message has tags or is from the lead.

- [ ] **Step 5: Run tests**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run "TestNotification.*Concluded|TestTagged.*Concluded" -v`
Expected: PASS

- [ ] **Step 6: Run full team tests**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -v`
Expected: All pass (including pre-existing tests)

- [ ] **Step 7: Build**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 8: Commit**

```bash
git add internal/team/launcher.go internal/team/launcher_test.go && git commit -m "feat: suppress notifications for concluded threads, allow tagged piercing"
```

---

### Task 6: Handoff Notification Injection

**Files:**
- Modify: `internal/team/launcher.go` (deliverTaskNotification, ~line 477)
- Test: `internal/team/launcher_test.go`

- [ ] **Step 1: Write failing test — handoff notification includes context**

```go
func TestHandoffNotificationContent(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "FE"},
			},
		},
	}
	task := teamTask{
		ID:      "task-1",
		Channel: "general",
		Title:   "Build page",
		Owner:   "fe",
		Handoffs: []taskHandoff{{
			FromAgent: "designer",
			ToAgent:   "fe",
			WhatIDid:  "Figma spec done",
			WhatToDo:  "Build HTML from spec",
			Context:   "Mobile-first layout",
			CreatedAt: "2026-04-05T00:00:00Z",
		}},
	}
	action := officeActionLog{Kind: "task_handoff", Actor: "designer", Channel: "general"}
	content := l.taskNotificationContent(action, task)
	if !strings.Contains(content, "Figma spec done") {
		t.Fatalf("expected handoff context in notification, got: %s", content)
	}
	if !strings.Contains(content, "Build HTML from spec") {
		t.Fatalf("expected what_to_do in notification, got: %s", content)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run TestHandoffNotificationContent -v`

- [ ] **Step 3: Update taskNotificationContent to include handoff context**

Find `taskNotificationContent` in launcher.go and add a handoff-specific branch:

```go
// In taskNotificationContent, add at the start:
if action.Kind == "task_handoff" && len(task.Handoffs) > 0 {
    h := task.Handoffs[len(task.Handoffs)-1] // latest handoff
    return fmt.Sprintf("[Handoff from @%s → @%s on #%s %s]: What was done: %s | What to do: %s | Context: %s",
        h.FromAgent, h.ToAgent, channel, task.Title,
        truncate(h.WhatIDid, 200), truncate(h.WhatToDo, 200), truncate(h.Context, 200))
}
```

- [ ] **Step 4: Run test**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/team/... -run TestHandoffNotificationContent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/team/launcher.go internal/team/launcher_test.go && git commit -m "feat: inject handoff context into task notifications"
```

---

### Task 7: MCP Tools — team_conclude, team_handoff, team_reopen

**Files:**
- Modify: `internal/teammcp/server.go`
- Test: `internal/teammcp/server_test.go`

- [ ] **Step 1: Write failing test — team_conclude calls broker**

```go
func TestHandleTeamConclude(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	t.Setenv("WUPHF_AGENT_SLUG", "fe")

	msg, _ := b.PostMessage("fe", "general", "Thread root", nil, "")

	result, _, err := handleTeamConclude(context.Background(), nil, TeamConcludeArgs{
		ThreadID:  msg.ID,
		Discussed: "Landing page",
		Decided:   "3 sections",
		Done:      "Built and deployed",
	})
	if err != nil {
		t.Fatalf("handleTeamConclude: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	if !b.IsThreadConcluded("general", msg.ID) {
		t.Fatal("expected thread to be concluded")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/teammcp/... -run TestHandleTeamConclude -v`

- [ ] **Step 3: Add arg types**

In `server.go`, add after existing arg types:

```go
type TeamConcludeArgs struct {
	Channel   string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ThreadID  string `json:"thread_id" jsonschema:"Root message ID of the thread to conclude"`
	Discussed string `json:"discussed" jsonschema:"What topics were covered"`
	Decided   string `json:"decided" jsonschema:"What decisions were made"`
	Done      string `json:"done" jsonschema:"What concrete work was completed"`
	OpenItems string `json:"open_items,omitempty" jsonschema:"Anything remaining or handed off"`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamHandoffArgs struct {
	Channel  string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	TaskID   string `json:"task_id" jsonschema:"Task ID to hand off"`
	ToAgent  string `json:"to_agent" jsonschema:"Agent slug receiving the handoff"`
	WhatIDid string `json:"what_i_did" jsonschema:"What you completed"`
	WhatToDo string `json:"what_to_do" jsonschema:"What the receiving agent should do next"`
	Context  string `json:"context,omitempty" jsonschema:"Relevant details, gotchas, dependencies"`
	MySlug   string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamReopenArgs struct {
	Channel  string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ThreadID string `json:"thread_id" jsonschema:"Root message ID of the thread to reopen"`
	Reason   string `json:"reason" jsonschema:"Why the thread needs to be reopened"`
	MySlug   string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}
```

- [ ] **Step 4: Register MCP tools and implement handlers**

Register tools (after `team_task_ack`):

```go
mcp.AddTool(server, &mcp.Tool{
	Name:        "team_conclude",
	Description: "Conclude a thread with a structured summary. The summary is shown directly to the human. Write it for them, not for agents.",
}, handleTeamConclude)

mcp.AddTool(server, &mcp.Tool{
	Name:        "team_handoff",
	Description: "Hand off a task to another agent with full context: what you did, what they need to do, and relevant details.",
}, handleTeamHandoff)

mcp.AddTool(server, &mcp.Tool{
	Name:        "team_reopen",
	Description: "Reopen a previously concluded thread. CEO/lead only.",
}, handleTeamReopen)
```

Implement handlers:

```go
func handleTeamConclude(ctx context.Context, _ *mcp.CallToolRequest, args TeamConcludeArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	threadID := strings.TrimSpace(args.ThreadID)
	if threadID == "" {
		return toolError(fmt.Errorf("thread_id is required")), nil, nil
	}
	var result struct {
		Conclusion struct {
			ThreadID    string `json:"thread_id"`
			ConcludedAt string `json:"concluded_at"`
		} `json:"conclusion"`
	}
	if err := brokerPostJSON(ctx, "/conclude", map[string]any{
		"channel":      channel,
		"thread_id":    threadID,
		"discussed":    strings.TrimSpace(args.Discussed),
		"decided":      strings.TrimSpace(args.Decided),
		"done":         strings.TrimSpace(args.Done),
		"open_items":   strings.TrimSpace(args.OpenItems),
		"concluded_by": mySlug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Thread %s in #%s concluded.", result.Conclusion.ThreadID, channel)), nil, nil
}

func handleTeamHandoff(ctx context.Context, _ *mcp.CallToolRequest, args TeamHandoffArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	taskID := strings.TrimSpace(args.TaskID)
	toAgent := strings.TrimSpace(args.ToAgent)
	if taskID == "" || toAgent == "" {
		return toolError(fmt.Errorf("task_id and to_agent are required")), nil, nil
	}
	var result struct {
		Task struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Owner string `json:"owner"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/handoff", map[string]any{
		"channel":    channel,
		"task_id":    taskID,
		"from_agent": mySlug,
		"to_agent":   toAgent,
		"what_i_did": strings.TrimSpace(args.WhatIDid),
		"what_to_do": strings.TrimSpace(args.WhatToDo),
		"context":    strings.TrimSpace(args.Context),
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Handed off %s to @%s — %s", result.Task.ID, result.Task.Owner, result.Task.Title)), nil, nil
}

func handleTeamReopen(ctx context.Context, _ *mcp.CallToolRequest, args TeamReopenArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	threadID := strings.TrimSpace(args.ThreadID)
	if threadID == "" {
		return toolError(fmt.Errorf("thread_id is required")), nil, nil
	}
	if err := brokerPostJSON(ctx, "/conclude/reopen", map[string]any{
		"channel":   channel,
		"thread_id": threadID,
		"reason":    strings.TrimSpace(args.Reason),
		"slug":      mySlug,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Thread %s in #%s reopened.", threadID, channel)), nil, nil
}
```

- [ ] **Step 5: Run test**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/teammcp/... -run TestHandleTeamConclude -v`
Expected: PASS

- [ ] **Step 6: Run full MCP test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/teammcp/... -v`
Expected: All pass

- [ ] **Step 7: Build**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 8: Commit**

```bash
git add internal/teammcp/server.go internal/teammcp/server_test.go && git commit -m "feat: team_conclude, team_handoff, team_reopen MCP tools"
```

---

### Task 8: team_poll Enhancements — Conclusions + Pending Handoffs

**Files:**
- Modify: `internal/teammcp/server.go` (handleTeamPoll ~line 693, add formatConclusionsSummary and formatPendingHandoffs)

- [ ] **Step 1: Write failing test — team_poll includes conclusions**

```go
func TestTeamPollIncludesConclusions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	t.Setenv("WUPHF_AGENT_SLUG", "fe")

	msg, _ := b.PostMessage("fe", "general", "Thread root", nil, "")
	// Conclude via API
	handleTeamConclude(context.Background(), nil, TeamConcludeArgs{
		ThreadID: msg.ID, Discussed: "API", Decided: "REST", Done: "Spec written",
	})

	result, _, err := handleTeamPoll(context.Background(), nil, TeamPollArgs{})
	if err != nil {
		t.Fatalf("handleTeamPoll: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Recent Conclusions") {
		t.Fatalf("expected conclusions in poll, got: %s", text)
	}
	if !strings.Contains(text, "Spec written") {
		t.Fatalf("expected conclusion content in poll")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/teammcp/... -run TestTeamPollIncludesConclusions -v`

- [ ] **Step 3: Add formatConclusionsSummary and formatPendingHandoffs**

```go
func formatConclusionsSummary(ctx context.Context, channel string) string {
	var result struct {
		Conclusions []struct {
			ThreadID string `json:"thread_id"`
			Summary  struct {
				Discussed string `json:"discussed"`
				Decided   string `json:"decided"`
				Done      string `json:"done"`
				OpenItems string `json:"open_items"`
			} `json:"summary"`
			ConcludedBy string `json:"concluded_by"`
		} `json:"conclusions"`
	}
	path := "/conclusions?channel=" + url.QueryEscape(channel) + "&limit=5"
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Conclusions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Recent Conclusions\n")
	for _, c := range result.Conclusions {
		sb.WriteString(fmt.Sprintf("- Thread %s (@%s): Done: %s", c.ThreadID, c.ConcludedBy, c.Summary.Done))
		if c.Summary.OpenItems != "" {
			sb.WriteString(" | Open: " + c.Summary.OpenItems)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatPendingHandoffs(ctx context.Context, mySlug, channel string) string {
	if mySlug == "" {
		return ""
	}
	var result brokerTasksResponse
	path := "/tasks?channel=" + url.QueryEscape(channel) + "&my_slug=" + url.QueryEscape(mySlug)
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Tasks) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, task := range result.Tasks {
		if task.Owner != mySlug || len(task.Handoffs) == 0 {
			continue
		}
		h := task.Handoffs[len(task.Handoffs)-1]
		if h.ToAgent != mySlug {
			continue
		}
		if sb.Len() == 0 {
			sb.WriteString("## Pending Handoffs\n")
		}
		sb.WriteString(fmt.Sprintf("- %s from @%s: What was done: %s | What to do: %s", task.ID, h.FromAgent, h.WhatIDid, h.WhatToDo))
		if h.Context != "" {
			sb.WriteString(" | Context: " + h.Context)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
```

Update `brokerTaskSummary` to include Handoffs:

```go
// Add to brokerTaskSummary struct:
Handoffs []struct {
	FromAgent string `json:"from_agent"`
	ToAgent   string `json:"to_agent"`
	WhatIDid  string `json:"what_i_did"`
	WhatToDo  string `json:"what_to_do"`
	Context   string `json:"context"`
} `json:"handoffs,omitempty"`
```

Update `handleTeamPoll` to include both sections in the response:

```go
// Replace the existing return line in handleTeamPoll (non-1:1 mode):
conclusionsSummary := formatConclusionsSummary(ctx, channel)
handoffsSummary := formatPendingHandoffs(ctx, resolveSlugOptional(args.MySlug), channel)
return textResult(fmt.Sprintf("Channel #%s\n\n%s\n\nTagged messages for you: %d\n\n%s\n\n%s\n\n%s\n\n%s\n\n%s",
	channel, summary, result.TaggedCount, taskSummary, requestSummary, memorySummary, conclusionsSummary, handoffsSummary)), nil, nil
```

- [ ] **Step 4: Run test**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/teammcp/... -run TestTeamPollIncludesConclusions -v`
Expected: PASS

- [ ] **Step 5: Build + full test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf && go test ./internal/teammcp/... -v`

- [ ] **Step 6: Commit**

```bash
git add internal/teammcp/server.go internal/teammcp/server_test.go && git commit -m "feat: team_poll includes conclusions and pending handoffs"
```

---

### Task 9: Agent Prompts — Conclude + Handoff Instructions

**Files:**
- Modify: `internal/agent/prompts.go`
- Test: `internal/agent/prompts_test.go`

- [ ] **Step 1: Write failing test — CEO prompt includes conclude/handoff instructions**

```go
func TestBuildTeamLeadPromptIncludesConcludeHandoff(t *testing.T) {
	prompt := BuildTeamLeadPrompt(
		AgentConfig{Slug: "ceo", Name: "CEO"},
		[]AgentConfig{{Slug: "fe", Name: "FE"}},
		"TestPack",
	)
	if !strings.Contains(prompt, "team_conclude") {
		t.Fatal("CEO prompt should mention team_conclude")
	}
	if !strings.Contains(prompt, "team_handoff") {
		t.Fatal("CEO prompt should mention team_handoff")
	}
}
```

- [ ] **Step 2: Write failing test — specialist prompt includes conclude/handoff**

```go
func TestBuildSpecialistPromptIncludesConcludeHandoff(t *testing.T) {
	prompt := BuildSpecialistPrompt(AgentConfig{Slug: "fe", Name: "FE", Expertise: []string{"frontend"}})
	if !strings.Contains(prompt, "team_conclude") {
		t.Fatal("specialist prompt should mention team_conclude")
	}
	if !strings.Contains(prompt, "team_handoff") {
		t.Fatal("specialist prompt should mention team_handoff")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/agent/... -run "TestBuild.*Conclude" -v`

- [ ] **Step 4: Add conclude/handoff instructions to prompts**

In `BuildTeamLeadPrompt`, add before `SKILL DETECTION:`:

```
THREAD LIFECYCLE:
When a body of work is complete, use team_conclude to close the thread with a summary.
The summary should be something the human can read and forward — not internal shorthand.
Include: what was discussed, what was decided, what was done, and any open items.

When assigning work across agents, prefer team_handoff over raw task reassignment.
Handoffs carry context: what was done, what's next, and what the receiving agent needs to know.
```

In `BuildSpecialistPrompt`, add after the existing rules:

```
7. When you finish your piece of work and another agent needs to continue, use team_handoff to transfer with context — include what you did, what's left, and any gotchas.
8. When a discussion reaches a conclusion, use team_conclude with a clear summary. The summary is shown directly to the human — write it for them, not for agents.
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./internal/agent/... -v`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add internal/agent/prompts.go internal/agent/prompts_test.go && git commit -m "feat: add conclude/handoff instructions to agent prompts"
```

---

### Task 10: Full Integration Test + Push

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go test ./... 2>&1`
Expected: All 17 packages pass

- [ ] **Step 2: Build final binary**

Run: `cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 3: Push and update PR**

```bash
cd /Users/najmuzzaman/Documents/nex/wuphf-telegram && git push
```

Update the existing PR description to include the new thread lifecycle and handoff features.

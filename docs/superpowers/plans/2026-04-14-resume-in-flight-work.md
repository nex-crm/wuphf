# Resume In-Flight Work Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When WUPHF restarts, automatically re-push orphaned tasks and unanswered conversations to agents so work resumes without human intervention.

**Architecture:** Add a `resumeInFlightWork()` method on `Launcher` that scans broker state for in-flight tasks and unanswered human messages, builds a combined resume work packet per agent, and delivers through the existing notification infrastructure. Called at end of `primeVisibleAgents()` (tmux) or synchronously before notification loops (headless).

**Tech Stack:** Go, existing broker/launcher/notification infrastructure

**Spec:** `docs/superpowers/specs/2026-04-14-resume-in-flight-work-design.md`

---

### Task 1: Add broker accessors for in-flight state

**Files:**
- Modify: `internal/team/broker.go` (after `AllTasks()` at line ~1445)
- Test: `internal/team/broker_test.go`

- [ ] **Step 1: Write failing test for `InFlightTasks()`**

Add to `internal/team/broker_test.go`:

```go
func TestInFlightTasks(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Done task", Status: "done", Owner: "fe"},
		{ID: "t2", Title: "Active task", Status: "in_progress", Owner: "be"},
		{ID: "t3", Title: "Review task", Status: "review", Owner: "fe"},
		{ID: "t4", Title: "Pending task", Status: "pending", Owner: "ceo"},
		{ID: "t5", Title: "Blocked task", Status: "blocked", Owner: "be"},
		{ID: "t6", Title: "Cancelled task", Status: "cancelled", Owner: "fe"},
		{ID: "t7", Title: "No owner", Status: "in_progress", Owner: ""},
		{ID: "t8", Title: "Completed task", Status: "completed", Owner: "be"},
		{ID: "t9", Title: "Canceled task", Status: "canceled", Owner: "fe"},
	}

	got := b.InFlightTasks()
	wantIDs := []string{"t2", "t3", "t4", "t5"}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d in-flight tasks, got %d: %+v", len(wantIDs), len(got), got)
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("task[%d]: expected ID %q, got %q", i, want, got[i].ID)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestInFlightTasks -v`
Expected: FAIL — `b.InFlightTasks undefined`

- [ ] **Step 3: Implement `InFlightTasks()`**

Add to `internal/team/broker.go` after the `AllTasks()` method (line ~1445):

```go
// InFlightTasks returns tasks with non-terminal status and an assigned owner.
// Terminal statuses: done, completed, canceled, cancelled.
func (b *Broker) InFlightTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Owner == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "done", "completed", "canceled", "cancelled", "":
			continue
		}
		out = append(out, task)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestInFlightTasks -v`
Expected: PASS

- [ ] **Step 5: Write failing test for `RecentHumanMessages()`**

Add to `internal/team/broker_test.go`:

```go
func TestRecentHumanMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.messages = []channelMessage{
		{ID: "m1", From: "you", Content: "Build the dashboard", Channel: "general"},
		{ID: "m2", From: "ceo", Content: "On it", Channel: "general", ReplyTo: "m1"},
		{ID: "m3", From: "human", Content: "Check API limits", Channel: "engineering"},
		{ID: "m4", From: "fe", Content: "Working on dashboard", Channel: "general"},
		{ID: "m5", From: "nex", Content: "New notification", Channel: "general"},
		{ID: "m6", From: "system", Content: "Routing...", Channel: "general"},
		{ID: "m7", From: "you", Content: "Old message", Channel: "general"},
	}

	got := b.RecentHumanMessages(5)
	// Should return m5, m3, m7 (human-originated, within last 5), NOT m2, m4, m6
	// m1 is outside the last-5 window
	wantIDs := map[string]bool{"m5": true, "m3": true, "m7": true}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d human messages, got %d: %+v", len(wantIDs), len(got), got)
	}
	for _, msg := range got {
		if !wantIDs[msg.ID] {
			t.Errorf("unexpected message ID %q", msg.ID)
		}
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestRecentHumanMessages -v`
Expected: FAIL — `b.RecentHumanMessages undefined`

- [ ] **Step 7: Implement `RecentHumanMessages()`**

Add to `internal/team/broker.go` after `InFlightTasks()`:

```go
// RecentHumanMessages returns the most recent human-originated messages
// (from "you", "human", or "nex") within the last `limit` messages.
// Messages are returned in original order.
func (b *Broker) RecentHumanMessages(limit int) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.messages
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]channelMessage, 0)
	for _, msg := range msgs {
		from := strings.ToLower(strings.TrimSpace(msg.From))
		if from == "you" || from == "human" || from == "nex" {
			out = append(out, msg)
		}
	}
	return out
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestRecentHumanMessages -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/team/broker.go internal/team/broker_test.go
git commit -m "feat: add InFlightTasks and RecentHumanMessages broker accessors

Support the resume-on-startup feature by exposing filtered views of
broker state for in-flight tasks and recent human messages."
```

---

### Task 2: Implement resume detection and packet building

**Files:**
- Create: `internal/team/resume.go`
- Test: `internal/team/resume_test.go`

- [ ] **Step 1: Write failing test for `findUnansweredMessages()`**

Create `internal/team/resume_test.go`:

```go
package team

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestFindUnansweredMessages(t *testing.T) {
	// Message m1 from human, agent replied (m2). Should NOT be unanswered.
	// Message m3 from human, no agent reply. Should BE unanswered.
	// Message m5 from nex, no reply. Should BE unanswered.
	allMessages := []channelMessage{
		{ID: "m1", From: "you", Content: "Build the dashboard", Channel: "general"},
		{ID: "m2", From: "ceo", Content: "On it", Channel: "general", ReplyTo: "m1"},
		{ID: "m3", From: "human", Content: "Check API limits", Channel: "engineering"},
		{ID: "m4", From: "fe", Content: "Unrelated work", Channel: "design"},
		{ID: "m5", From: "nex", Content: "New lead arrived", Channel: "general"},
	}

	humanMsgs := []channelMessage{
		{ID: "m1", From: "you", Content: "Build the dashboard", Channel: "general"},
		{ID: "m3", From: "human", Content: "Check API limits", Channel: "engineering"},
		{ID: "m5", From: "nex", Content: "New lead arrived", Channel: "general"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	wantIDs := map[string]bool{"m3": true, "m5": true}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d unanswered, got %d: %+v", len(wantIDs), len(got), got)
	}
	for _, msg := range got {
		if !wantIDs[msg.ID] {
			t.Errorf("unexpected unanswered message ID %q", msg.ID)
		}
	}
}

func TestFindUnansweredMessagesAllAnswered(t *testing.T) {
	allMessages := []channelMessage{
		{ID: "m1", From: "you", Content: "Do X", Channel: "general"},
		{ID: "m2", From: "ceo", Content: "Done", Channel: "general", ReplyTo: "m1"},
	}
	humanMsgs := []channelMessage{
		{ID: "m1", From: "you", Content: "Do X", Channel: "general"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	if len(got) != 0 {
		t.Fatalf("expected 0 unanswered, got %d: %+v", len(got), got)
	}
}

func TestFindUnansweredMessagesEmpty(t *testing.T) {
	got := findUnansweredMessages(nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestFindUnanswered -v`
Expected: FAIL — `findUnansweredMessages undefined`

- [ ] **Step 3: Implement `findUnansweredMessages()`**

Create `internal/team/resume.go`:

```go
package team

import (
	"fmt"
	"strings"
)

// findUnansweredMessages returns human messages that have no agent reply
// in the same thread. A message is "answered" if any non-human, non-system
// message exists with a ReplyTo matching the message's ID.
func findUnansweredMessages(humanMsgs, allMessages []channelMessage) []channelMessage {
	if len(humanMsgs) == 0 {
		return nil
	}
	// Build set of message IDs that have been replied to by an agent.
	repliedTo := make(map[string]struct{})
	for _, msg := range allMessages {
		from := strings.ToLower(strings.TrimSpace(msg.From))
		if from == "you" || from == "human" || from == "nex" || from == "system" || from == "" {
			continue
		}
		if replyTo := strings.TrimSpace(msg.ReplyTo); replyTo != "" {
			repliedTo[replyTo] = struct{}{}
		}
	}

	out := make([]channelMessage, 0)
	for _, msg := range humanMsgs {
		if _, answered := repliedTo[strings.TrimSpace(msg.ID)]; !answered {
			out = append(out, msg)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestFindUnanswered -v`
Expected: PASS

- [ ] **Step 5: Write failing test for `buildResumePacket()`**

Add to `internal/team/resume_test.go`:

```go
func TestBuildResumePacket(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Title: "Implement auth", Status: "in_progress", PipelineStage: "implement", Owner: "be"},
		{ID: "t2", Title: "Review PR", Status: "review", PipelineStage: "review", Owner: "be", WorktreePath: "/tmp/wt-1"},
	}
	msgs := []channelMessage{
		{ID: "m3", From: "you", Content: "Check the API rate limits", Channel: "general"},
	}

	got := buildResumePacket("be", tasks, msgs)
	if !strings.Contains(got, "[Session resumed") {
		t.Error("missing resume header")
	}
	if !strings.Contains(got, "#t1") {
		t.Error("missing task t1")
	}
	if !strings.Contains(got, "#t2") {
		t.Error("missing task t2")
	}
	if !strings.Contains(got, "Working directory:") {
		t.Error("missing worktree path for t2")
	}
	if !strings.Contains(got, "Check the API rate limits") {
		t.Error("missing unanswered message")
	}
	if !strings.Contains(got, "team_broadcast") {
		t.Error("missing reply instructions")
	}
}

func TestBuildResumePacketTasksOnly(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Title: "Fix bug", Status: "in_progress", Owner: "fe"},
	}

	got := buildResumePacket("fe", tasks, nil)
	if !strings.Contains(got, "#t1") {
		t.Error("missing task")
	}
	if strings.Contains(got, "Unanswered messages:") {
		t.Error("should not include unanswered section when empty")
	}
}

func TestBuildResumePacketMessagesOnly(t *testing.T) {
	msgs := []channelMessage{
		{ID: "m1", From: "you", Content: "Hello", Channel: "general"},
	}

	got := buildResumePacket("ceo", nil, msgs)
	if strings.Contains(got, "Active tasks:") {
		t.Error("should not include tasks section when empty")
	}
	if !strings.Contains(got, "Hello") {
		t.Error("missing message")
	}
}

func TestBuildResumePacketEmpty(t *testing.T) {
	got := buildResumePacket("fe", nil, nil)
	if got != "" {
		t.Errorf("expected empty string for no work, got %q", got)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestBuildResumePacket -v`
Expected: FAIL — `buildResumePacket undefined`

- [ ] **Step 7: Implement `buildResumePacket()`**

Add to `internal/team/resume.go`:

```go
// buildResumePacket creates a combined work packet for an agent with all
// their in-flight tasks and unanswered messages. Returns "" if there's
// nothing to resume.
func buildResumePacket(slug string, tasks []teamTask, msgs []channelMessage) string {
	if len(tasks) == 0 && len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Session resumed — picking up where you left off]\n")

	if len(tasks) > 0 {
		sb.WriteString("\nActive tasks:\n")
		for _, task := range tasks {
			line := fmt.Sprintf("- #%s %q (%s", task.ID, truncate(task.Title, 96), strings.TrimSpace(task.Status))
			if stage := strings.TrimSpace(task.PipelineStage); stage != "" {
				line += ", stage: " + stage
			}
			line += ")"
			sb.WriteString(line + "\n")
			if path := strings.TrimSpace(task.WorktreePath); path != "" {
				sb.WriteString(fmt.Sprintf("  Working directory: %q\n", path))
			}
		}
	}

	if len(msgs) > 0 {
		sb.WriteString("\nUnanswered messages:\n")
		for _, msg := range msgs {
			channel := normalizeChannelSlug(msg.Channel)
			if channel == "" {
				channel = "general"
			}
			sb.WriteString(fmt.Sprintf("- @%s in #%s (%s): %q\n", msg.From, channel, msg.ID, truncate(msg.Content, 200)))
		}
	}

	sb.WriteString(fmt.Sprintf("\nResume instructions:\nContinue your most urgent work. For tasks, pick up where you left off. For unanswered messages, read the context and respond.\nUse team_broadcast with my_slug %q and the appropriate channel and reply_to_id.", slug))

	return sb.String()
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestBuildResumePacket -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/team/resume.go internal/team/resume_test.go
git commit -m "feat: add resume detection and packet building

findUnansweredMessages scans for human messages with no agent reply.
buildResumePacket creates a combined work packet per agent with their
in-flight tasks and unanswered conversations."
```

---

### Task 3: Implement `resumeInFlightWork()` on Launcher

**Files:**
- Modify: `internal/team/resume.go` (add the Launcher method)
- Test: `internal/team/resume_test.go` (add integration-level test)

- [ ] **Step 1: Write failing test for `resumeInFlightWork()`**

Add to `internal/team/resume_test.go`:

```go
func TestResumeInFlightWorkBuildsCorrectPackets(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Build dashboard", Status: "in_progress", Owner: "fe", PipelineStage: "implement"},
		{ID: "t2", Title: "Fix API bug", Status: "in_progress", Owner: "be", PipelineStage: "fix"},
		{ID: "t3", Title: "Done task", Status: "done", Owner: "fe"},
	}
	b.messages = []channelMessage{
		{ID: "m1", From: "you", Content: "Deploy the hotfix", Channel: "general"},
		{ID: "m2", From: "ceo", Content: "Working on it", Channel: "general", ReplyTo: "m1"},
		{ID: "m3", From: "you", Content: "Check staging", Channel: "engineering"},
		// m3 has no reply — should be unanswered
	}

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo"},
				{Slug: "fe"},
				{Slug: "be"},
			},
		},
	}

	packets := l.buildResumePackets()
	// fe should have 1 task (t1)
	// be should have 1 task (t2)
	// ceo should get unanswered message m3 (routed as lead)
	if len(packets) == 0 {
		t.Fatal("expected resume packets, got none")
	}

	fePacket, hasFe := packets["fe"]
	if !hasFe {
		t.Fatal("expected packet for fe")
	}
	if !strings.Contains(fePacket, "#t1") {
		t.Error("fe packet missing task t1")
	}

	bePacket, hasBe := packets["be"]
	if !hasBe {
		t.Fatal("expected packet for be")
	}
	if !strings.Contains(bePacket, "#t2") {
		t.Error("be packet missing task t2")
	}
}

func TestResumeInFlightWorkSkipsAgentsNotInPack(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Task for removed agent", Status: "in_progress", Owner: "designer"},
	}

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo"},
				{Slug: "fe"},
			},
		},
	}

	packets := l.buildResumePackets()
	if _, has := packets["designer"]; has {
		t.Error("should not build packet for agent not in pack")
	}
}

func TestResumeInFlightWorkEmptyState(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents:   []agent.AgentConfig{{Slug: "ceo"}},
		},
	}

	packets := l.buildResumePackets()
	if len(packets) != 0 {
		t.Errorf("expected no packets for empty state, got %d", len(packets))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestResumeInFlightWork -v`
Expected: FAIL — `l.buildResumePackets undefined`

- [ ] **Step 3: Implement `buildResumePackets()` and `resumeInFlightWork()`**

Add to `internal/team/resume.go`:

```go
// buildResumePackets scans broker state for in-flight tasks and unanswered
// human messages, then builds a resume work packet per agent. Returns a map
// of agent slug -> resume packet string. Agents not in the current pack are
// skipped.
func (l *Launcher) buildResumePackets() map[string]string {
	if l.broker == nil || l.pack == nil {
		return nil
	}

	// Build pack membership set.
	packSlugs := make(map[string]struct{}, len(l.pack.Agents))
	for _, cfg := range l.pack.Agents {
		packSlugs[cfg.Slug] = struct{}{}
	}

	// Scan tasks: group in-flight tasks by owner.
	agentTasks := make(map[string][]teamTask)
	for _, task := range l.broker.InFlightTasks() {
		if _, inPack := packSlugs[task.Owner]; !inPack {
			continue
		}
		agentTasks[task.Owner] = append(agentTasks[task.Owner], task)
	}

	// Scan messages: find unanswered human messages.
	humanMsgs := l.broker.RecentHumanMessages(50)
	allMsgs := l.broker.AllMessages()
	unanswered := findUnansweredMessages(humanMsgs, allMsgs)

	// Route unanswered messages to agents using pack membership.
	// We cannot use notificationTargetsForMessage here because it depends on
	// agentPaneTargets() which requires tmux pane state (empty in headless mode).
	// Instead, route based on explicit tags or fall back to the pack lead.
	agentMsgs := make(map[string][]channelMessage)
	lead := l.pack.LeadSlug
	if lead == "" {
		lead = "ceo"
	}
	for _, msg := range unanswered {
		routed := false
		for _, tag := range msg.Tagged {
			tag = strings.TrimSpace(tag)
			if _, inPack := packSlugs[tag]; inPack {
				agentMsgs[tag] = append(agentMsgs[tag], msg)
				routed = true
			}
		}
		if !routed {
			if _, inPack := packSlugs[lead]; inPack {
				agentMsgs[lead] = append(agentMsgs[lead], msg)
			}
		}
	}

	// Build packets.
	packets := make(map[string]string)
	allSlugs := make(map[string]struct{})
	for slug := range agentTasks {
		allSlugs[slug] = struct{}{}
	}
	for slug := range agentMsgs {
		allSlugs[slug] = struct{}{}
	}
	for slug := range allSlugs {
		packet := buildResumePacket(slug, agentTasks[slug], agentMsgs[slug])
		if packet != "" {
			packets[slug] = packet
		}
	}
	return packets
}

// resumeInFlightWork delivers resume packets to all agents with interrupted work.
// In tmux mode, this is called at the end of primeVisibleAgents() after agents
// are ready. In headless mode, called synchronously before notification loops.
func (l *Launcher) resumeInFlightWork() {
	packets := l.buildResumePackets()
	if len(packets) == 0 {
		return
	}

	targets := l.agentPaneTargets()
	for slug, packet := range packets {
		if l.usesCodexRuntime() || l.webMode {
			l.enqueueHeadlessCodexTurn(slug, packet)
			continue
		}
		if target, ok := targets[slug]; ok {
			l.sendNotificationToPane(target.PaneTarget, packet)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run TestResumeInFlightWork -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/team/resume.go internal/team/resume_test.go
git commit -m "feat: implement resumeInFlightWork on Launcher

Scans broker state for orphaned tasks and unanswered conversations,
builds combined resume packets per agent, delivers through existing
notification infrastructure."
```

---

### Task 4: Wire resume into startup paths

**Files:**
- Modify: `internal/team/launcher.go` (end of `primeVisibleAgents()`, line ~1065)
- Modify: `internal/team/headless_codex.go` (in `launchHeadlessCodex()`, line ~62)

- [ ] **Step 1: Wire into tmux path**

In `internal/team/launcher.go`, at the end of `primeVisibleAgents()` (after the existing `l.deliverMessageNotification(latest)` call at line ~1065), add the resume sweep:

Replace the end of `primeVisibleAgents()`:

```go
	// If the human already posted while Claude was still booting, replay a catch-up nudge
	// so the first visible message is not lost forever behind the startup interactivity.
	if l.broker == nil {
		return
	}
	msgs := l.broker.Messages()
	if len(msgs) == 0 {
		return
	}
	latest := msgs[len(msgs)-1]
	l.deliverMessageNotification(latest)
}
```

With:

```go
	// If the human already posted while Claude was still booting, replay a catch-up nudge
	// so the first visible message is not lost forever behind the startup interactivity.
	if l.broker == nil {
		return
	}
	msgs := l.broker.Messages()
	if len(msgs) > 0 {
		latest := msgs[len(msgs)-1]
		l.deliverMessageNotification(latest)
	}

	// Resume in-flight work: push orphaned tasks and unanswered conversations
	// back to their owning agents now that panes are ready.
	l.resumeInFlightWork()
}
```

- [ ] **Step 2: Wire into headless path**

In `internal/team/headless_codex.go`, in `launchHeadlessCodex()`, add the synchronous resume call **before** the notification loops start. Change lines 62-69:

From:

```go
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())

	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
```

To:

```go
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())

	// Resume in-flight work synchronously before starting notification loops.
	// This prevents races where a fresh notification and a resume packet collide
	// in the headless queue.
	l.resumeInFlightWork()

	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
```

- [ ] **Step 3: Verify existing tests still pass**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -v -count=1 2>&1 | tail -20`
Expected: All existing tests PASS

- [ ] **Step 4: Verify build succeeds**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go build ./...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/team/launcher.go internal/team/headless_codex.go
git commit -m "feat: wire resume sweep into startup paths

Tmux: resume runs at end of primeVisibleAgents after agents are ready.
Headless: resume runs synchronously before notification loops to
prevent queue races."
```

---

### Task 5: Integration test for full resume flow

**Files:**
- Modify: `internal/team/resume_test.go`

- [ ] **Step 1: Write integration test simulating a restart with state**

Add to `internal/team/resume_test.go`:

```go
func TestResumeInFlightWorkNoBrokerNoPanic(t *testing.T) {
	l := &Launcher{}
	// Should be a no-op, not panic.
	l.resumeInFlightWork()
}

func TestResumeInFlightWorkNoPackNoPanic(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	l := &Launcher{broker: b}
	// Should be a no-op, not panic.
	l.resumeInFlightWork()
}

func TestBuildResumePacketsUnansweredRoutesToLead(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.messages = []channelMessage{
		{ID: "m1", From: "you", Content: "What's the status?", Channel: "general"},
		// No agent reply to m1
	}
	// Ensure general channel has ceo as a member.
	b.channels = []teamChannel{
		{Slug: "general", Name: "General", Members: []string{"ceo", "fe"}},
	}
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO"},
		{Slug: "fe", Name: "Frontend", Role: "Frontend"},
	}

	l := &Launcher{
		broker:      b,
		focusMode:   true,
		sessionName: "wuphf-team",
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo"},
				{Slug: "fe"},
			},
		},
	}

	packets := l.buildResumePackets()
	// Untagged message in general should route to CEO (lead) in focus mode.
	if _, hasCEO := packets["ceo"]; !hasCEO {
		t.Errorf("expected CEO to get unanswered message, got packets for: %v", keysOf(packets))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 2: Run all resume tests**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/team/ -run "TestResume|TestBuildResume|TestFindUnanswered" -v`
Expected: All PASS

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./... 2>&1 | tail -20`
Expected: All PASS (or pre-existing failures only)

- [ ] **Step 4: Commit**

```bash
git add internal/team/resume_test.go
git commit -m "test: add integration tests for resume flow

Tests nil broker/pack safety, unanswered message routing to lead,
and edge cases for the resume-on-startup feature."
```

---

### File Structure Summary

| File | Purpose |
|---|---|
| `internal/team/broker.go` | Add `InFlightTasks()` and `RecentHumanMessages()` accessors |
| `internal/team/resume.go` | New file: `findUnansweredMessages()`, `buildResumePacket()`, `buildResumePackets()`, `resumeInFlightWork()` |
| `internal/team/resume_test.go` | New file: all resume-related tests |
| `internal/team/launcher.go` | Wire `resumeInFlightWork()` into end of `primeVisibleAgents()` |
| `internal/team/headless_codex.go` | Wire `resumeInFlightWork()` synchronously before notify loops |

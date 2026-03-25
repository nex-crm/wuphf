package agent

import (
	"sync"
	"testing"
	"time"
)

func newTestService(t *testing.T, streamFn StreamFn) *AgentService {
	t.Helper()
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	tools := NewToolRegistry()
	queues := NewMessageQueues()

	return NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)
}

func TestCreateFromTemplate(t *testing.T) {
	svc := newTestService(t, nil)

	ma, err := svc.CreateFromTemplate("my-seo", "seo-agent")
	if err != nil {
		t.Fatalf("CreateFromTemplate: %v", err)
	}

	if ma.Config.Slug != "my-seo" {
		t.Errorf("expected slug 'my-seo', got %q", ma.Config.Slug)
	}
	if ma.Config.Name != "SEO Analyst" {
		t.Errorf("expected name 'SEO Analyst', got %q", ma.Config.Name)
	}

	// Verify it exists in the service.
	got, ok := svc.Get("my-seo")
	if !ok {
		t.Fatal("expected agent to exist after Create")
	}
	if got.Config.Slug != "my-seo" {
		t.Errorf("Get returned wrong slug: %q", got.Config.Slug)
	}
}

func TestCreateDuplicateSlug(t *testing.T) {
	svc := newTestService(t, nil)

	_, err := svc.Create(AgentConfig{Slug: "dup", Name: "Dup"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = svc.Create(AgentConfig{Slug: "dup", Name: "Dup2"})
	if err == nil {
		t.Fatal("expected error for duplicate slug")
	}
}

func TestCreateFromUnknownTemplate(t *testing.T) {
	svc := newTestService(t, nil)

	_, err := svc.CreateFromTemplate("x", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestStartStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	cfg := AgentConfig{
		Slug:      "lifecycle",
		Name:      "Lifecycle Agent",
		Expertise: []string{"testing"},
	}

	_, err := svc.Create(cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Start.
	if err := svc.Start("lifecycle"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, ok := svc.GetState("lifecycle")
	if !ok {
		t.Fatal("expected agent state")
	}
	if state.Phase != PhaseIdle {
		t.Errorf("expected idle after Start, got %s", state.Phase)
	}

	// Stop.
	if err := svc.Stop("lifecycle"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Start/stop on nonexistent slug.
	if err := svc.Start("nope"); err == nil {
		t.Error("expected error starting nonexistent agent")
	}
	if err := svc.Stop("nope"); err == nil {
		t.Error("expected error stopping nonexistent agent")
	}
}

func TestSteerMessageDelivery(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	cfg := AgentConfig{
		Slug: "steer-test",
		Name: "Steer Test",
	}
	svc.Create(cfg)

	if err := svc.Steer("steer-test", "go left"); err != nil {
		t.Fatalf("Steer: %v", err)
	}

	// Verify the queue has the message.
	if !queues.HasSteer("steer-test") {
		t.Error("expected steer message in queue")
	}

	msg, ok := queues.DrainSteer("steer-test")
	if !ok || msg != "go left" {
		t.Errorf("expected 'go left', got %q (ok=%v)", msg, ok)
	}

	// Steer on nonexistent.
	if err := svc.Steer("nope", "x"); err == nil {
		t.Error("expected error steering nonexistent agent")
	}
}

func TestFollowUpMessageDelivery(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	cfg := AgentConfig{
		Slug: "followup-test",
		Name: "FollowUp Test",
	}
	svc.Create(cfg)

	if err := svc.FollowUp("followup-test", "continue"); err != nil {
		t.Fatalf("FollowUp: %v", err)
	}

	if !queues.HasFollowUp("followup-test") {
		t.Error("expected follow-up message in queue")
	}

	// FollowUp on nonexistent.
	if err := svc.FollowUp("nope", "x"); err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestSubscribeUnsubscribe(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	var mu sync.Mutex
	callCount := 0
	unsub := svc.Subscribe(func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	// Create an agent — should fire the listener.
	svc.Create(AgentConfig{Slug: "sub-test", Name: "Sub Test"})

	mu.Lock()
	c := callCount
	mu.Unlock()
	if c == 0 {
		t.Error("expected listener to fire on Create")
	}

	// Unsubscribe.
	unsub()

	beforeCount := c
	svc.Create(AgentConfig{Slug: "sub-test-2", Name: "Sub Test 2"})

	mu.Lock()
	c = callCount
	mu.Unlock()
	if c != beforeCount {
		t.Errorf("expected no more calls after unsubscribe, got %d (was %d)", c, beforeCount)
	}
}

func TestEnsureRunningTickLoop(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	// Use a channel-based mock StreamFn that signals when it's called.
	streamCalled := make(chan struct{}, 10)
	mockStream := func(msgs []Message, tls []AgentTool) <-chan StreamChunk {
		ch := make(chan StreamChunk, 1)
		go func() {
			defer close(ch)
			select {
			case streamCalled <- struct{}{}:
			default:
			}
			ch <- StreamChunk{Type: "text", Content: "tick response"}
		}()
		return ch
	}

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	// Create and manually wire the stream function into the loop.
	cfg := AgentConfig{
		Slug:      "tick-test",
		Name:      "Tick Test",
		Expertise: []string{"testing"},
	}
	ma, err := svc.Create(cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Replace the loop's stream function with our mock.
	// Since we're in the same package, we can access internal fields.
	ma.Loop.streamFn = mockStream

	// Enqueue a message and start the loop.
	queues.FollowUp("tick-test", "do something")
	svc.Start("tick-test")
	svc.EnsureRunning("tick-test")

	// Wait for the stream to be called (the tick loop should trigger it).
	select {
	case <-streamCalled:
		// Success — the tick loop is running and progressing the agent.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tick loop to call streamFn")
	}

	// Stop should clean up.
	if err := svc.Stop("tick-test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Calling EnsureRunning again after stop should be safe (no-op since agent is stopped).
	svc.EnsureRunning("tick-test")
}

func TestEnsureRunningDoesNotHoldServiceMutexDuringTick(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	queues := NewMessageQueues()
	tools := NewToolRegistry()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	mockStream := func(msgs []Message, tls []AgentTool) <-chan StreamChunk {
		ch := make(chan StreamChunk)
		go func() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			ch <- StreamChunk{Type: "text", Content: "done"}
			close(ch)
		}()
		return ch
	}

	svc := NewAgentService(
		WithToolRegistry(tools),
		WithSessionStore(sessions),
		WithQueues(queues),
	)

	ma, err := svc.Create(AgentConfig{Slug: "blocking", Name: "Blocking Agent"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ma.Loop.streamFn = mockStream

	queues.FollowUp("blocking", "do something")
	if err := svc.Start("blocking"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc.EnsureRunning("blocking")

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for blocking tick to start")
	}

	done := make(chan struct{})
	go func() {
		_ = svc.List()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("List blocked while agent tick was in progress")
	}

	done = make(chan struct{})
	go func() {
		_, _ = svc.GetState("blocking")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("GetState blocked while agent tick was in progress")
	}

	close(release)
}

func TestListAgents(t *testing.T) {
	svc := newTestService(t, nil)

	svc.Create(AgentConfig{Slug: "b-agent", Name: "B"})
	svc.Create(AgentConfig{Slug: "a-agent", Name: "A"})

	list := svc.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(list))
	}
	if list[0].Config.Slug != "a-agent" {
		t.Errorf("expected first agent 'a-agent', got %q", list[0].Config.Slug)
	}
	if list[1].Config.Slug != "b-agent" {
		t.Errorf("expected second agent 'b-agent', got %q", list[1].Config.Slug)
	}
}

func TestRemoveAgent(t *testing.T) {
	svc := newTestService(t, nil)

	svc.Create(AgentConfig{Slug: "remove-me", Name: "Remove Me"})
	if err := svc.Remove("remove-me"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, ok := svc.Get("remove-me")
	if ok {
		t.Error("expected agent to be removed")
	}

	if err := svc.Remove("remove-me"); err == nil {
		t.Error("expected error removing nonexistent agent")
	}
}

func TestGetTemplateNames(t *testing.T) {
	svc := newTestService(t, nil)
	names := svc.GetTemplateNames()
	if len(names) == 0 {
		t.Fatal("expected template names")
	}

	// Verify sorted.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("template names not sorted: %v", names)
			break
		}
	}
}

func TestGetTemplate(t *testing.T) {
	svc := newTestService(t, nil)

	cfg, ok := svc.GetTemplate("seo-agent")
	if !ok {
		t.Fatal("expected seo-agent template to exist")
	}
	if cfg.Name != "SEO Analyst" {
		t.Errorf("expected 'SEO Analyst', got %q", cfg.Name)
	}

	_, ok = svc.GetTemplate("nonexistent")
	if ok {
		t.Error("expected nonexistent template to not exist")
	}
}

func TestUpdateConfig(t *testing.T) {
	svc := newTestService(t, nil)

	svc.Create(AgentConfig{Slug: "update-me", Name: "Original", Expertise: []string{"a"}})

	newName := "Updated"
	newCron := "0 */2 * * *"
	err := svc.UpdateConfig("update-me", ConfigUpdate{
		Name:          &newName,
		Expertise:     []string{"b", "c"},
		HeartbeatCron: &newCron,
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	ma, ok := svc.Get("update-me")
	if !ok {
		t.Fatal("expected agent to exist")
	}
	if ma.Config.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", ma.Config.Name)
	}
	if len(ma.Config.Expertise) != 2 || ma.Config.Expertise[0] != "b" {
		t.Errorf("expected expertise [b, c], got %v", ma.Config.Expertise)
	}
	if ma.Config.HeartbeatCron != "0 */2 * * *" {
		t.Errorf("expected cron '0 */2 * * *', got %q", ma.Config.HeartbeatCron)
	}

	// Update nonexistent.
	if err := svc.UpdateConfig("nope", ConfigUpdate{}); err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

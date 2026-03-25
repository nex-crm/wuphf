package orchestration

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeTask(id string) TaskDefinition {
	return TaskDefinition{ID: id, Title: id, RequiredSkills: []string{"general"}}
}

func TestExecutor_SubmitAndCheckout(t *testing.T) {
	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 5})
	if err := e.Submit(makeTask("t1")); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	ok, err := e.Checkout("t1", "alice")
	if err != nil {
		t.Fatalf("checkout failed: %v", err)
	}
	if !ok {
		t.Fatal("checkout should succeed")
	}
	active := e.GetActive()
	if len(active) != 1 || active[0].ID != "t1" {
		t.Errorf("expected t1 in active, got %v", active)
	}
}

func TestExecutor_DuplicateCheckoutPrevented(t *testing.T) {
	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 5})
	_ = e.Submit(makeTask("t1"))
	ok, _ := e.Checkout("t1", "alice")
	if !ok {
		t.Fatal("first checkout should succeed")
	}
	ok2, _ := e.Checkout("t1", "bob")
	if ok2 {
		t.Error("second checkout of same task should fail")
	}
}

func TestExecutor_ConcurrencyLimit(t *testing.T) {
	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 1})
	_ = e.Submit(makeTask("t1"))
	_ = e.Submit(makeTask("t2"))

	ok1, _ := e.Checkout("t1", "alice")
	if !ok1 {
		t.Fatal("first checkout should succeed within limit")
	}
	ok2, _ := e.Checkout("t2", "bob")
	if ok2 {
		t.Error("second checkout should be blocked by concurrency limit")
	}
}

func TestExecutor_Release_Complete(t *testing.T) {
	var events []ExecutorEvent
	var mu sync.Mutex

	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 2})
	e.OnEvent(func(ev ExecutorEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	_ = e.Submit(makeTask("t1"))
	_, _ = e.Checkout("t1", "alice")

	res := "done"
	if err := e.Release("t1", &res, nil); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond) // let async events fire
	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, ev := range events {
		if ev.Type == "task:complete" && ev.TaskID == "t1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected task:complete event, got %v", events)
	}
	if len(e.GetActive()) != 0 {
		t.Error("no tasks should be active after release")
	}
}

func TestExecutor_Release_Fail(t *testing.T) {
	var events []ExecutorEvent
	var mu sync.Mutex

	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 2})
	e.OnEvent(func(ev ExecutorEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	_ = e.Submit(makeTask("t1"))
	_, _ = e.Checkout("t1", "alice")

	errMsg := "something broke"
	if err := e.Release("t1", nil, &errMsg); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, ev := range events {
		if ev.Type == "task:fail" && ev.Error == errMsg {
			found = true
		}
	}
	if !found {
		t.Errorf("expected task:fail event, got %v", events)
	}
}

func TestExecutor_Timeout(t *testing.T) {
	var timedOut atomic.Bool

	e := NewExecutor(OrchestratorConfig{
		MaxConcurrentAgents: 5,
		TaskTimeout:         50 * time.Millisecond,
	})
	e.OnEvent(func(ev ExecutorEvent) {
		if ev.Type == "task:timeout" {
			timedOut.Store(true)
		}
	})

	_ = e.Submit(makeTask("t1"))
	_, _ = e.Checkout("t1", "alice")

	time.Sleep(200 * time.Millisecond)
	if !timedOut.Load() {
		t.Error("expected task:timeout event")
	}
}

func TestExecutor_StopAll(t *testing.T) {
	var events []ExecutorEvent
	var mu sync.Mutex

	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 5})
	e.OnEvent(func(ev ExecutorEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	_ = e.Submit(makeTask("t1"))
	_ = e.Submit(makeTask("t2"))
	_, _ = e.Checkout("t1", "alice")
	_, _ = e.Checkout("t2", "bob")

	if err := e.StopAll(); err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	failCount := 0
	for _, ev := range events {
		if ev.Type == "task:fail" {
			failCount++
		}
	}
	if failCount < 2 {
		t.Errorf("expected 2 task:fail events, got %d (%v)", failCount, events)
	}
}

func TestExecutor_OnEvent_Unsubscribe(t *testing.T) {
	e := NewExecutor(OrchestratorConfig{MaxConcurrentAgents: 2})
	var count atomic.Int32
	unsub := e.OnEvent(func(_ ExecutorEvent) { count.Add(1) })
	unsub() // unsubscribe before any events

	_ = e.Submit(makeTask("t1"))
	_, _ = e.Checkout("t1", "alice")
	res := "ok"
	_ = e.Release("t1", &res, nil)

	time.Sleep(20 * time.Millisecond)
	if count.Load() != 0 {
		t.Errorf("unsubscribed handler should not be called, got %d calls", count.Load())
	}
}

func TestExecutor_DuplicateSubmit(t *testing.T) {
	e := NewExecutor(OrchestratorConfig{})
	_ = e.Submit(makeTask("t1"))
	err := e.Submit(makeTask("t1"))
	if err == nil {
		t.Error("duplicate submit should return an error")
	}
}

package agent

import (
	"sync"
	"testing"
)

func TestQueues_FIFOOrdering(t *testing.T) {
	q := NewMessageQueues()
	q.Steer("agent1", "first")
	q.Steer("agent1", "second")
	q.Steer("agent1", "third")

	msgs := []string{}
	for {
		m, ok := q.DrainSteer("agent1")
		if !ok {
			break
		}
		msgs = append(msgs, m)
	}

	expected := []string{"first", "second", "third"}
	for i, m := range msgs {
		if m != expected[i] {
			t.Errorf("position %d: got %q, want %q", i, m, expected[i])
		}
	}
}

func TestQueues_DrainEmpty(t *testing.T) {
	q := NewMessageQueues()
	_, ok := q.DrainSteer("nonexistent")
	if ok {
		t.Error("expected false draining empty steer queue")
	}
	_, ok = q.DrainFollowUp("nonexistent")
	if ok {
		t.Error("expected false draining empty follow-up queue")
	}
}

func TestQueues_HasMessages(t *testing.T) {
	q := NewMessageQueues()
	if q.HasMessages("agent1") {
		t.Error("expected no messages initially")
	}
	q.Steer("agent1", "hello")
	if !q.HasMessages("agent1") {
		t.Error("expected HasMessages true after Steer")
	}
	q.DrainSteer("agent1")
	if q.HasMessages("agent1") {
		t.Error("expected no messages after drain")
	}
	q.FollowUp("agent1", "follow")
	if !q.HasMessages("agent1") {
		t.Error("expected HasMessages true after FollowUp")
	}
}

func TestQueues_ConcurrentSteerDrain(t *testing.T) {
	q := NewMessageQueues()
	const n = 100
	var wg sync.WaitGroup

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			q.Steer("agent", "msg")
		}()
	}
	wg.Wait()

	count := 0
	for {
		_, ok := q.DrainSteer("agent")
		if !ok {
			break
		}
		count++
	}
	if count != n {
		t.Errorf("expected %d messages, got %d", n, count)
	}
}

func TestQueues_SeparateAgents(t *testing.T) {
	q := NewMessageQueues()
	q.Steer("alpha", "for alpha")
	q.Steer("beta", "for beta")

	m, _ := q.DrainSteer("alpha")
	if m != "for alpha" {
		t.Errorf("got %q, want %q", m, "for alpha")
	}
	m, _ = q.DrainSteer("beta")
	if m != "for beta" {
		t.Errorf("got %q, want %q", m, "for beta")
	}
}

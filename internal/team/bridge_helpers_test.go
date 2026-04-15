package team

import (
	"context"
	"testing"
	"time"
)

func TestExponentialBackoff(t *testing.T) {
	b := NewBridgeBackoff(100*time.Millisecond, 60*time.Second)
	d1 := b.Next()
	d2 := b.Next()
	d3 := b.Next()
	if d1 < 80*time.Millisecond || d1 > 120*time.Millisecond {
		t.Fatalf("d1: %v", d1)
	}
	if d2 <= d1 {
		t.Fatalf("d2 %v should exceed d1 %v", d2, d1)
	}
	_ = d3
	b.Reset()
	if got := b.Next(); got < 80*time.Millisecond || got > 120*time.Millisecond {
		t.Fatalf("after reset: %v", got)
	}
}

func TestExponentialBackoffCap(t *testing.T) {
	b := NewBridgeBackoff(1*time.Second, 2*time.Second)
	for i := 0; i < 20; i++ {
		if d := b.Next(); d > 3*time.Second { // jitter may push slightly over cap
			t.Fatalf("iteration %d: %v exceeds cap", i, d)
		}
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if !cb.Open() {
		t.Fatal("expected open after 3 failures")
	}
	time.Sleep(60 * time.Millisecond)
	if cb.Open() {
		t.Fatal("expected half-open after pause")
	}
	cb.RecordSuccess()
	if cb.Open() {
		t.Fatal("expected closed after success")
	}
}

func TestBackoffRespectsContext(t *testing.T) {
	b := NewBridgeBackoff(500*time.Millisecond, 60*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := b.Wait(ctx)
	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("Wait did not return on ctx cancel")
	}
}

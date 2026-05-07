package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokerLoadStateSanitizesTaskWatchdogAndSchedulerText(t *testing.T) {
	secret := "sk-" + strings.Repeat("L", 24)
	path := filepath.Join(t.TempDir(), "broker-state.json")
	raw, err := json.Marshal(brokerState{
		Tasks: []teamTask{{
			ID:      "task-1",
			Title:   "Task " + secret,
			Details: "Details " + secret,
		}},
		Watchdogs: []watchdogAlert{{
			ID:      "watchdog-1",
			Summary: "Alert " + secret,
		}},
		Scheduler: []schedulerJob{{
			Slug:          "job-1",
			Label:         "Label " + secret,
			Payload:       "Payload " + secret,
			LastRunStatus: "Status " + secret,
		}},
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	b := NewBrokerAt(path)
	if err := b.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}

	combined := strings.Join([]string{
		b.tasks[0].Title,
		b.tasks[0].Details,
		b.watchdogs[0].Summary,
		b.scheduler[0].Label,
		b.scheduler[0].Payload,
		b.scheduler[0].LastRunStatus,
	}, "\n")
	if strings.Contains(combined, secret) {
		t.Fatalf("loaded state leaked secret: %q", combined)
	}
}

func TestBrokerStateWriteSanitizesTaskWatchdogAndSchedulerText(t *testing.T) {
	secret := "sk-" + strings.Repeat("W", 24)
	b := newTestBroker(t)
	b.tasks = []teamTask{{
		ID:      "task-1",
		Title:   "Task " + secret,
		Details: "Details " + secret,
	}}
	b.watchdogs = []watchdogAlert{{
		ID:      "watchdog-1",
		Summary: "Alert " + secret,
	}}
	b.scheduler = []schedulerJob{{
		Slug:          "job-1",
		Label:         "Label " + secret,
		Payload:       "Payload " + secret,
		LastRunStatus: "Status " + secret,
	}}

	b.mu.Lock()
	write, err := b.prepareBrokerStateWriteLocked()
	b.mu.Unlock()
	if err != nil {
		t.Fatalf("prepareBrokerStateWriteLocked: %v", err)
	}
	if strings.Contains(string(write.data), secret) {
		t.Fatalf("state write leaked secret: %s", string(write.data))
	}
}

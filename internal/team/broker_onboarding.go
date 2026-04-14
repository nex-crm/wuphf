package team

import (
	"fmt"
	"strings"
	"time"
)

// onboardingCompleteFn is invoked by the onboarding package when the user
// finishes the wizard. It seeds the default team (idempotent — no-op if a
// team already exists), posts the user's first task to #general as a human
// message tagged to the office lead, and lets the existing launcher trigger
// the lead's delegate turn.
//
// Side effects happen BEFORE the onboarding package writes the completion
// flag to disk, so a crash between this call returning and the flag write
// re-enters the wizard — and the dedupe guard below prevents double-posting.
func (b *Broker) onboardingCompleteFn(task string, skipTask bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Seed default team if none is configured yet. This mirrors the path
	// taken on first boot (see ensureDefaultOfficeMembersLocked).
	b.ensureDefaultOfficeMembersLocked()

	// Skip-task path: team seeded, no first message. Caller marks onboarded.
	if skipTask {
		return b.saveLocked()
	}

	task = strings.TrimSpace(task)
	if task == "" {
		return fmt.Errorf("onboarding: task is required when skip_task=false")
	}

	// Dedupe: if a prior onboarding-complete already posted this exact task
	// (recognized via the onboarding_origin marker in Kind), skip re-posting.
	for _, existing := range b.messages {
		if existing.Channel == "general" && existing.Kind == "onboarding_origin" && existing.Content == task {
			return b.saveLocked()
		}
	}

	lead := officeLeadSlugFrom(b.members, nil)
	if lead == "" {
		lead = "ceo"
	}

	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "human",
		Channel:   "general",
		Kind:      "onboarding_origin",
		Content:   task,
		Tagged:    []string{lead},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)

	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	b.lastTaggedAt[lead] = time.Now()

	return b.saveLocked()
}

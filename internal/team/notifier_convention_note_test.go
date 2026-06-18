package team

// notifier_convention_note_test.go pins the Slack-channel authoring rules
// appended to agent notifications. These exist because the bridged channel
// contains real people: tags ping, the office speaks with one voice, and
// acknowledgement-only chatter is spam (observed live as six near-identical
// "no action needed" posts in two minutes).

import (
	"strings"
	"testing"
)

func TestSlackChannelConventionNote(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "slack-office",
		Name:    "slack-office",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack", RemoteID: "C0123"},
	})
	b.mu.Unlock()
	l := &Launcher{broker: b}

	note := l.slackChannelConventionNote("slack-office")
	for _, want := range []string{
		// Work quietly: outcomes + coordination only, no progress narration.
		"WORK QUIETLY, SPEAK FOR OUTCOMES AND COORDINATION",
		"NEVER post progress or status",
		"NEVER @-tag",
		// Delegation goes through a subtask, not a parent-thread tag.
		"give it its OWN subtask",
		"Do NOT @-tag the assignee in the parent",
		"ONE coordinating presence",
		"post NOTHING",
		"acknowledgement-only",
		// Task threads are active, unlike the main channel.
		"INSIDE A TASK THREAD you are NOT passive",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("slack convention note missing %q", want)
		}
	}

	if got := l.slackChannelConventionNote("general"); got != "" {
		t.Errorf("non-slack channel should get no note, got %q", got)
	}
}

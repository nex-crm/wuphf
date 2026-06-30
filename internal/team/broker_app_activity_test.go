package team

import "testing"

// appBuilderRunTaskID resolves an app's persistent edit channel to its backing
// app-builder run so GET /apps/{id}/activity can stream the right run WITHOUT the
// operator FE ever seeing a task id. This pins the app-scoped substrate's core
// mapping: the right channel resolves to the right task id; a foreign channel, a
// non-app-builder task sharing the channel, and an empty channel resolve to "".
func TestAppBuilderRunTaskIDResolvesEditChannel(t *testing.T) {
	b := &Broker{}
	b.tasks = append(b.tasks,
		teamTask{ID: "OFFICE-7", Owner: appBuilderSlug, Channel: "task-office-7"},
		teamTask{ID: "OFFICE-8", Owner: "ceo", Channel: "task-office-8"},
	)
	tests := []struct {
		name    string
		channel string
		want    string
	}{
		{"resolves app-builder run by channel", "task-office-7", "OFFICE-7"},
		{"unknown channel resolves to none", "task-office-99", ""},
		{"non-app-builder task on channel resolves to none", "task-office-8", ""},
		{"empty channel resolves to none", "", ""},
		{"whitespace channel resolves to none", "   ", ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := b.appBuilderRunTaskID(tt.channel); got != tt.want {
				t.Fatalf("appBuilderRunTaskID(%q) = %q, want %q", tt.channel, got, tt.want)
			}
		})
	}
}

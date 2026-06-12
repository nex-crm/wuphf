package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// The App Home view shows the task board by lane, links to every web-app
// surface, and the wiki index — the office overview inside Slack.
func TestSlackHomeTabRendersBoardWikiAndWebSurfaces(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://127.0.0.1:7905")
	seedTask := func(id, title, status string) {
		b.mu.Lock()
		b.tasks = append(b.tasks, teamTask{ID: id, Title: title, Owner: "ceo", Channel: "slack-general", status: status})
		b.mu.Unlock()
	}
	seedTask("OFFICE-1", "Ship the pricing facts", "in_progress")
	seedTask("OFFICE-2", "Review the launch plan", "review")
	seedTask("OFFICE-3", "Old win", "done")

	// app_home_opened drives the publish through the normal event path.
	evt := socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: slackevents.EventsAPIEvent{
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Data: &slackevents.AppHomeOpenedEvent{User: "U777", Tab: "home"},
			},
		},
	}
	if ack := tr.handleEvent(context.Background(), &brokerTransportHost{broker: b}, evt); !ack {
		t.Fatal("app_home_opened must be acked")
	}

	if len(api.publishedViews) != 1 {
		t.Fatalf("expected 1 published view, got %d", len(api.publishedViews))
	}
	pv := api.publishedViews[0]
	if pv.UserID != "U777" {
		t.Fatalf("view published for %q, want U777", pv.UserID)
	}
	raw, err := json.Marshal(pv.View)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	view := string(raw)

	for _, want := range []string{
		"WUPHF Office", "Task board", "Wiki",
		"OFFICE-1", "Ship the pricing facts",
		"OFFICE-2", "OFFICE-3",
		"http://127.0.0.1:7905/tasks", "http://127.0.0.1:7905/wiki",
		"http://127.0.0.1:7905/inbox", "http://127.0.0.1:7905/agents",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("home view missing %q\nview: %s", want, view)
		}
	}
}

// Without a web URL (broker booted before LaunchWeb, or headless tests) the
// view still renders — just without app links.
func TestSlackHomeTabWithoutWebURL(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	tr.publishHomeTab(context.Background(), "U777")
	if len(api.publishedViews) != 1 {
		t.Fatalf("expected 1 published view, got %d", len(api.publishedViews))
	}
	raw, _ := json.Marshal(api.publishedViews[0].View)
	if strings.Contains(string(raw), "Open in the app") {
		t.Fatalf("no web links expected without a web URL, got %s", raw)
	}
}

func TestSlackHomeTrim(t *testing.T) {
	if got := slackHomeTrim("", 10); got != "_empty_" {
		t.Fatalf("empty trim = %q", got)
	}
	long := strings.Repeat("x", 50)
	if got := slackHomeTrim(long, 10); !strings.HasPrefix(got, "xxxxxxxxxx\n_…truncated") {
		t.Fatalf("trim = %q", got)
	}
}

package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newSurfaceTestServer(t *testing.T, b *Broker) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/surfaces", b.requireAuth(b.handleSurfaces))
	mux.HandleFunc("/surfaces/", b.requireAuth(b.handleSurfaceSubpath))
	return httptest.NewServer(mux)
}

func TestBrokerSurfacesFiltersByChannelAccess(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "private-launch",
		Name:    "Private Launch",
		Members: []string{"pm"},
	})
	b.mu.Unlock()
	srv := newSurfaceTestServer(t, b)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"title":      "Private command center",
		"channel":    "private-launch",
		"created_by": "pm",
		"my_slug":    "pm",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/surfaces", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", res.StatusCode)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/surfaces?viewer_slug=eng", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer res.Body.Close()
	var list struct {
		Surfaces []SurfaceManifest `json:"surfaces"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Surfaces) != 0 {
		t.Fatalf("eng should not see private surface: %+v", list.Surfaces)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/surfaces/private-command-center?viewer_slug=eng", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read denied: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.StatusCode)
	}
}

func TestBrokerSurfaceWidgetMutationAuditsAndPublishes(t *testing.T) {
	b := newTestBroker(t)
	srv := newSurfaceTestServer(t, b)
	defer srv.Close()

	events, unsub := b.SubscribeSurfaceEvents(8)
	defer unsub()

	createBody, _ := json.Marshal(map[string]any{
		"title":      "Launch command center",
		"channel":    "general",
		"created_by": "ceo",
		"my_slug":    "ceo",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/surfaces", bytes.NewReader(createBody), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create surface: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create surface status=%d", res.StatusCode)
	}
	<-events

	widgetBody, _ := json.Marshal(map[string]any{
		"my_slug": "ceo",
		"actor":   "ceo",
		"widget": map[string]any{
			"id":     "notes",
			"title":  "Notes",
			"kind":   "markdown",
			"source": "kind: markdown\nmarkdown: Ship the surface.\n",
		},
	})
	req, _ = authReq(http.MethodPost, srv.URL+"/surfaces/launch-command-center/widgets", bytes.NewReader(widgetBody), b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upsert widget: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("upsert status=%d body=%s", res.StatusCode, body)
	}

	select {
	case evt := <-events:
		if evt.Type != "surface:widget_created" || evt.WidgetID != "notes" {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected surface widget event")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	var hasAction bool
	for _, action := range b.actions {
		if action.Kind == "widget_upserted" && action.Source == "studio" {
			hasAction = true
		}
	}
	if !hasAction {
		t.Fatalf("missing studio action: %+v", b.actions)
	}
	var hasMessage bool
	for _, msg := range b.messages {
		if msg.Source == "studio" && strings.Contains(msg.Content, "#/apps/studio") {
			hasMessage = true
		}
	}
	if !hasMessage {
		t.Fatalf("missing durable studio message: %+v", b.messages)
	}
}

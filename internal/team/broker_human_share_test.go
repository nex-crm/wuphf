package team

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHumanInviteAcceptIsOneUseAndCreatesSession(t *testing.T) {
	b := newTestBroker(t)
	token, invite, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if token == "" || invite.ID == "" {
		t.Fatalf("invite missing token/id: token=%q invite=%+v", token, invite)
	}

	body := []byte(`{"token":"` + token + `","display_name":"Mira","device":"MacBook Pro"}`)
	req := httptest.NewRequest(http.MethodPost, "/humans/invites/accept", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	b.handleHumanInviteAccept(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	var accepted struct {
		Session humanSessionResponse `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	if accepted.Session.DisplayName != "Mira" || accepted.Session.HumanSlug != "mira" {
		t.Fatalf("session identity = %+v", accepted.Session)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatalf("accept did not set a session cookie")
	}

	req = httptest.NewRequest(http.MethodPost, "/humans/invites/accept", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	b.handleHumanInviteAccept(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("second accept status = %d, want 410 body=%s", rec.Code, rec.Body.String())
	}
}

func TestHumanMeAcceptsSessionCookie(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser", "100.64.0.2:1234")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/humans/me", nil)
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	rec := httptest.NewRecorder()
	b.handleHumanMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"display_name":"Mira"`)) {
		t.Fatalf("me body missing Mira: %s", rec.Body.String())
	}
}

func TestHumanMeRejectsExpiredSessionServerSide(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, session, err := b.acceptHumanInvite(token, "Mira", "browser", "100.64.0.2:1234")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	b.mu.Lock()
	for i := range b.humanSessions {
		if b.humanSessions[i].ID == session.ID {
			b.humanSessions[i].ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		}
	}
	b.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/humans/me", nil)
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	rec := httptest.NewRecorder()
	b.handleHumanMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me status = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}
}

func TestHumanEventsFilterNotebookMetadata(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser", "100.64.0.2:1234")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(b.handleEvents))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build events request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("events status = %d body=%s", resp.StatusCode, string(raw))
	}

	reader := bufio.NewReader(resp.Body)
	b.PublishNotebookEvent(notebookWriteEvent{
		Slug:      "pm",
		Path:      "agents/pm/notebook/private.md",
		CommitSHA: "abc123",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	b.PublishWikiEvent(wikiWriteEvent{
		Path:       "team/shared.md",
		CommitSHA:  "def456",
		AuthorSlug: "pm",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})

	var lines []string
	for len(lines) < 20 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event stream: %v lines=%q", err, strings.Join(lines, ""))
		}
		lines = append(lines, line)
		if strings.Contains(line, "event: wiki:write") {
			break
		}
	}
	stream := strings.Join(lines, "")
	if strings.Contains(stream, "notebook:write") || strings.Contains(stream, "private.md") {
		t.Fatalf("human SSE leaked notebook metadata: %s", stream)
	}
	if !strings.Contains(stream, "event: wiki:write") {
		t.Fatalf("human SSE did not receive shared wiki event: %s", stream)
	}
}

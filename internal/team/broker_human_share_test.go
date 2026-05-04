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
	if bytes.Contains(rec.Body.Bytes(), []byte("remote_addr")) {
		t.Fatalf("accept response exposed remote address: %s", rec.Body.String())
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
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
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

func TestHumanSessionsHostCanRevokeSession(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, session, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/humans/sessions", strings.NewReader(`{"id":"`+session.ID+`"}`))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	rec := httptest.NewRecorder()
	b.requireAuth(b.handleHumanSessions)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/humans/me", nil)
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	rec = httptest.NewRecorder()
	b.handleHumanMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session me status = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/humans/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	rec = httptest.NewRecorder()
	b.requireAuth(b.handleHumanSessions)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"revoked_at"`) {
		t.Fatalf("sessions body missing revoked_at: %s", rec.Body.String())
	}
}

func TestHumanSessionCannotRevokeTeamMemberSessions(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, session, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/humans/sessions", strings.NewReader(`{"id":"`+session.ID+`"}`))
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	rec := httptest.NewRecorder()
	b.requireAuth(b.handleHumanSessions)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member revoke status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
}

func TestHumanMeRejectsExpiredSessionServerSide(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, session, err := b.acceptHumanInvite(token, "Mira", "browser")
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

func TestResetClearsHumanShareState(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, _, err := b.acceptHumanInvite(token, "Mira", "browser"); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	b.Reset()

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.humanInvites) != 0 || len(b.humanSessions) != 0 {
		t.Fatalf("Reset left share state: invites=%d sessions=%d", len(b.humanInvites), len(b.humanSessions))
	}
}

func TestHumanSessionAuthIsScopedToShareRoutes(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	cookie := &http.Cookie{Name: humanSessionCookie, Value: sessionToken}

	allowed := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewReader([]byte(`{}`)))
	allowed.AddCookie(cookie)
	allowedRec := httptest.NewRecorder()
	called := false
	b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		actor, ok := requestActorFromContext(r.Context())
		if !ok || actor.Kind != requestActorKindHuman || actor.Slug != "mira" {
			t.Fatalf("actor = %+v ok=%v, want human mira", actor, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})(allowedRec, allowed)
	if !called || allowedRec.Code != http.StatusNoContent {
		t.Fatalf("allowed human route called=%v status=%d body=%s", called, allowedRec.Code, allowedRec.Body.String())
	}

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/office-members"},
		{http.MethodPost, "/channels"},
		{http.MethodPost, "/wiki/write"},
		{http.MethodGet, "/notebook/read"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			b.withAuth(func(http.ResponseWriter, *http.Request) {
				t.Fatalf("handler should not be called for %s %s", tc.method, tc.path)
			})(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	brokerReq := httptest.NewRequest(http.MethodPost, "/office-members", nil)
	brokerReq.Header.Set("Authorization", "Bearer "+b.Token())
	brokerRec := httptest.NewRecorder()
	b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := requestActorFromContext(r.Context())
		if !ok || actor.Kind != requestActorKindBroker {
			t.Fatalf("actor = %+v ok=%v, want broker", actor, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})(brokerRec, brokerReq)
	if brokerRec.Code != http.StatusNoContent {
		t.Fatalf("broker route status = %d body=%s", brokerRec.Code, brokerRec.Body.String())
	}
}

func TestHumanSessionAuthBlocksDMChannelList(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	cookie := &http.Cookie{Name: humanSessionCookie, Value: sessionToken}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/channels", http.StatusNoContent},
		{"/channels?type=dm", http.StatusForbidden},
		{"/channels?type=DM", http.StatusForbidden},
		{"/channels?type=Dm", http.StatusForbidden},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			b.withAuth(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHumanEventsFilterNotebookMetadata(t *testing.T) {
	b := newTestBroker(t)
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
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

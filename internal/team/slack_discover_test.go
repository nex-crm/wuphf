package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slack-go/slack"
)

// discoverFixture builds a broker with one bridged Slack channel (C0DISC) plus a
// transport wired to a fake API whose own bot id is UBOT.
func discoverFixture(t *testing.T) (*Broker, *SlackTransport, *fakeSlackAPI) {
	t.Helper()
	b := newTestBroker(t)
	if _, err := b.createSlackChannel("C0DISC", "office"); err != nil {
		t.Fatalf("bridge channel: %v", err)
	}
	api := newFakeSlackAPI()
	st := newSlackTransport(b, "xoxb-test", "xapp-test", api)
	st.botUserID = "UBOT"
	return b, st, api
}

func botUser(id, name string, isBot bool) *slack.User {
	u := &slack.User{ID: id, Name: name, IsBot: isBot}
	u.Profile.RealName = name
	u.Profile.DisplayName = name
	return u
}

func TestDiscoverChannelBots_ClassifiesAndExcludes(t *testing.T) {
	b, st, api := discoverFixture(t)
	// Already-registered foreign agent (should come back AlreadyRegistered).
	if _, err := b.RegisterSlackAgent("agent-two", "Agent Two", "U0BOT2"); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	api.members["C0DISC"] = []string{"UBOT", "U0HUMAN", "U0BOT1", "U0BOT2"}
	api.users["U0HUMAN"] = botUser("U0HUMAN", "Pat", false)
	api.users["U0BOT1"] = botUser("U0BOT1", "Researcher", true)
	api.users["U0BOT2"] = botUser("U0BOT2", "Agent Two", true)

	bots, err := st.DiscoverChannelBots(context.Background(), "C0DISC")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	// Own bot (UBOT) and the human (U0HUMAN) are excluded; two bots remain.
	if len(bots) != 2 {
		t.Fatalf("want 2 bots, got %d: %+v", len(bots), bots)
	}
	byID := map[string]DiscoveredSlackBot{}
	for _, d := range bots {
		byID[d.UserID] = d
	}
	if _, ok := byID["UBOT"]; ok {
		t.Fatal("own coordinator bot must be excluded")
	}
	if _, ok := byID["U0HUMAN"]; ok {
		t.Fatal("humans must be excluded")
	}
	if d, ok := byID["U0BOT1"]; !ok || d.AlreadyRegistered {
		t.Fatalf("U0BOT1 should be an unregistered bot: %+v", d)
	}
	if d := byID["U0BOT2"]; !d.AlreadyRegistered || d.RegisteredSlug != "agent-two" {
		t.Fatalf("U0BOT2 should be already-registered as agent-two: %+v", d)
	}
}

func TestDiscoverChannelBots_Paginates(t *testing.T) {
	_, st, api := discoverFixture(t)
	// Two pages of membership — the bot on page 2 must not be dropped.
	api.memberPages = map[string][][]string{
		"C0DISC": {{"UBOT", "U0BOTA"}, {"U0BOTB"}},
	}
	api.users["U0BOTA"] = botUser("U0BOTA", "Alpha", true)
	api.users["U0BOTB"] = botUser("U0BOTB", "Beta", true)

	bots, err := st.DiscoverChannelBots(context.Background(), "C0DISC")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(bots) != 2 {
		t.Fatalf("pagination dropped a bot: got %d %+v", len(bots), bots)
	}
}

func discoverRequest(t *testing.T, b *Broker, channelID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/slack/discover?channel_id="+channelID, nil)
	w := httptest.NewRecorder()
	b.handleSlackDiscover(w, req)
	return w
}

func TestHandleSlackDiscover_GatesAndRuns(t *testing.T) {
	b, st, api := discoverFixture(t)
	api.members["C0DISC"] = []string{"U0BOT1"}
	api.users["U0BOT1"] = botUser("U0BOT1", "Researcher", true)

	// Unbridged channel id → 400.
	if w := discoverRequest(t, b, "C0NOPE"); w.Code != http.StatusBadRequest {
		t.Fatalf("unbridged channel: want 400, got %d", w.Code)
	}

	// Bridged channel but no running transport → 409.
	if w := discoverRequest(t, b, "C0DISC"); w.Code != http.StatusConflict {
		t.Fatalf("no transport: want 409, got %d", w.Code)
	}

	// Transport running → 200 with the bot list.
	b.slackTransport = st
	w := discoverRequest(t, b, "C0DISC")
	if w.Code != http.StatusOK {
		t.Fatalf("with transport: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		ChannelID string               `json:"channel_id"`
		Bots      []DiscoveredSlackBot `json:"bots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ChannelID != "C0DISC" || len(resp.Bots) != 1 || resp.Bots[0].UserID != "U0BOT1" {
		t.Fatalf("unexpected discover response: %+v", resp)
	}
}

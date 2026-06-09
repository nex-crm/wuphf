package team

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	teamTransport "github.com/nex-crm/wuphf/internal/team/transport"
)

// fakeSlackAPI is an in-memory slackAPI used by both the transport and bridge
// tests. It records every PostMessageContext call (rendered to channel/text/
// thread-ts via the slack-go UnsafeApplyMsgOptions helper) and serves canned
// user / auth / membership responses. All access is mutex-guarded so the
// warm-up goroutine and the test goroutine cannot race.
type fakeSlackAPI struct {
	mu sync.Mutex

	posts     []fakePost
	postErr   error
	postErrAt int // return postErr only on the Nth (1-based) post; 0 = every call
	postCalls int

	users   map[string]*slack.User
	userErr error

	authResp *slack.AuthTestResponse
	authErr  error

	members    map[string][]string
	membersErr error
}

type fakePost struct {
	ChannelID string
	Text      string
	ThreadTS  string
}

func newFakeSlackAPI() *fakeSlackAPI {
	return &fakeSlackAPI{
		users:    map[string]*slack.User{},
		members:  map[string][]string{},
		authResp: &slack.AuthTestResponse{UserID: "UBOT"},
	}
}

func (f *fakeSlackAPI) PostMessageContext(_ context.Context, channelID string, opts ...slack.MsgOption) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.postCalls++
	if f.postErr != nil && (f.postErrAt == 0 || f.postErrAt == f.postCalls) {
		return "", "", f.postErr
	}
	_, values, err := slack.UnsafeApplyMsgOptions("token", channelID, "https://slack.test/api/", opts...)
	if err != nil {
		return "", "", err
	}
	ts := "1700000000.0000" + strconv.Itoa(f.postCalls)
	f.posts = append(f.posts, fakePost{
		ChannelID: channelID,
		Text:      firstValue(values, "text"),
		ThreadTS:  firstValue(values, "thread_ts"),
	})
	return channelID, ts, nil
}

func (f *fakeSlackAPI) GetUserInfoContext(_ context.Context, userID string) (*slack.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userErr != nil {
		return nil, f.userErr
	}
	u, ok := f.users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

func (f *fakeSlackAPI) AuthTestContext(_ context.Context) (*slack.AuthTestResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.authResp, nil
}

func (f *fakeSlackAPI) GetUsersInConversationContext(_ context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.membersErr != nil {
		return nil, "", f.membersErr
	}
	return f.members[params.ChannelID], "", nil
}

func (f *fakeSlackAPI) snapshotPosts() []fakePost {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakePost, len(f.posts))
	copy(out, f.posts)
	return out
}

func firstValue(v url.Values, key string) string {
	if vals := v[key]; len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func newTestBrokerWithSlackChannel(t *testing.T, channelID string) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "slack-general",
		Name:    "slack-general",
		Members: []string{"ceo", "pm"},
		Surface: &channelSurface{
			Provider:    "slack",
			RemoteID:    channelID,
			RemoteTitle: "general",
			Mode:        "channel",
			BotTokenEnv: "SLACK_BOT_TOKEN",
		},
		CreatedBy: "test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()
	return b
}

func newTestSlackTransport(t *testing.T, channelID string, api slackAPI) (*SlackTransport, *Broker) {
	t.Helper()
	b := newTestBrokerWithSlackChannel(t, channelID)
	tr := newSlackTransport(b, "xoxb-test", "xapp-test", api)
	return tr, b
}

// --- Channel map + contract surface ---

func TestSlackTransportChannelMap(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	if len(tr.ChannelMap) != 1 {
		t.Fatalf("expected 1 channel mapping, got %d", len(tr.ChannelMap))
	}
	if got := tr.ChannelMap["C0123"]; got != "slack-general" {
		t.Fatalf("expected slug slack-general for C0123, got %q", got)
	}
}

func TestSlackTransportContractInterface(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())

	if tr.Name() != "slack" {
		t.Fatalf("Name() = %q, want slack", tr.Name())
	}
	binding := tr.Binding()
	if binding.ChannelSlug != "" || binding.MemberSlug != "" {
		t.Fatalf("Binding() should be zero-value for multi-channel adapter, got %+v", binding)
	}
	h := tr.Health()
	if h.State != teamTransport.HealthDisconnected {
		t.Fatalf("Health().State before Run = %q, want disconnected", h.State)
	}
}

func TestSlackRunGuards(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")
	host := &brokerTransportHost{broker: b}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cases := []struct {
		name string
		tr   *SlackTransport
		want string
	}{
		{"no bot token", newSlackTransport(b, "", "xapp", newFakeSlackAPI()), "bot token is empty"},
		{"no app token", newSlackTransport(b, "xoxb", "", newFakeSlackAPI()), "app token is empty"},
		{"no socket runner", newSlackTransport(b, "xoxb", "xapp", newFakeSlackAPI()), "socket runner not configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tr.Run(ctx, host)
			if err == nil || !contains(err.Error(), tc.want) {
				t.Fatalf("Run: expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// --- Inbound mapping ---

func TestSlackRouteInbound(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U7"] = &slack.User{ID: "U7", Name: "alice", RealName: "Alice Dev", IsBot: false}
	tr, b := newTestSlackTransport(t, "C0123", api)
	host := &brokerTransportHost{broker: b}

	msg := &slackevents.MessageEvent{
		User:      "U7",
		Channel:   "C0123",
		Text:      "hello via socket",
		TimeStamp: "1700000001.0001",
	}
	if err := tr.routeInbound(context.Background(), host, msg); err != nil {
		t.Fatalf("routeInbound: %v", err)
	}

	msgs := b.ChannelMessages("slack-general")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in channel, got %d", len(msgs))
	}
	if msgs[0].Content != "hello via socket" {
		t.Fatalf("content = %q, want %q", msgs[0].Content, "hello via socket")
	}
	if msgs[0].From != "Alice Dev" {
		t.Fatalf("from = %q, want resolved display name Alice Dev", msgs[0].From)
	}
	if msgs[0].Source != "slack" {
		t.Fatalf("source = %q, want slack", msgs[0].Source)
	}
	// Participant should be cached after resolution, with humanity recorded.
	if got := tr.UserMap["U7"]; got.name != "Alice Dev" || !got.human {
		t.Fatalf("UserMap[U7] = %+v, want {name:Alice Dev human:true}", got)
	}
}

func TestSlackRouteInboundUpsertsParticipantBeforeReceive(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U9"] = &slack.User{ID: "U9", RealName: "Bob"}
	tr, b := newTestSlackTransport(t, "C0123", api)
	rec := &slackOrderingHost{inner: &brokerTransportHost{broker: b}}

	msg := &slackevents.MessageEvent{User: "U9", Channel: "C0123", Text: "hi", TimeStamp: "1.1"}
	if err := tr.routeInbound(context.Background(), rec, msg); err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	if rec.order != "upsert,receive" {
		t.Fatalf("expected upsert before receive, got order %q", rec.order)
	}
	if rec.lastParticipant.AdapterName != "slack" || rec.lastParticipant.Key != "U9" {
		t.Fatalf("participant = %+v, want adapter slack key U9", rec.lastParticipant)
	}
	if !rec.lastParticipant.Human {
		t.Fatal("expected Human=true for a non-bot user")
	}
	if rec.lastBinding.Scope != teamTransport.ScopeChannel || rec.lastBinding.ChannelSlug != "slack-general" {
		t.Fatalf("binding = %+v, want channel scope slack-general", rec.lastBinding)
	}
}

func TestSlackRouteInboundSkips(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	host := &brokerTransportHost{broker: b}

	cases := []struct {
		name string
		msg  *slackevents.MessageEvent
	}{
		{"bot message", &slackevents.MessageEvent{User: "U1", BotID: "B1", Channel: "C0123", Text: "bot says hi", TimeStamp: "1"}},
		{"own bot user", &slackevents.MessageEvent{User: "UBOT", Channel: "C0123", Text: "self echo", TimeStamp: "2"}},
		{"subtyped edit", &slackevents.MessageEvent{User: "U1", SubType: "message_changed", Channel: "C0123", Text: "edited", TimeStamp: "3"}},
		{"empty text", &slackevents.MessageEvent{User: "U1", Channel: "C0123", Text: "   ", TimeStamp: "4"}},
		{"unmapped channel", &slackevents.MessageEvent{User: "U1", Channel: "CZZZ", Text: "elsewhere", TimeStamp: "5"}},
		{"empty user", &slackevents.MessageEvent{User: "", Channel: "C0123", Text: "ghost", TimeStamp: "6"}},
	}
	tr.botUserID = "UBOT"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tr.routeInbound(context.Background(), host, tc.msg); err != nil {
				t.Fatalf("routeInbound returned error for skip case: %v", err)
			}
		})
	}
	if msgs := b.ChannelMessages("slack-general"); len(msgs) != 0 {
		t.Fatalf("expected no messages delivered for skip cases, got %d", len(msgs))
	}
}

func TestSlackRouteInboundChannelMissing(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U1"] = &slack.User{ID: "U1", RealName: "X"}
	b := newTestBroker(t)
	// Map a channel id to a slug that does not exist as a broker channel so the
	// Host returns ErrBindingChannelMissing on ReceiveMessage.
	tr := newSlackTransport(b, "xoxb", "xapp", api)
	tr.ChannelMap["C0123"] = "ghost-channel"
	host := &brokerTransportHost{broker: b}

	msg := &slackevents.MessageEvent{User: "U1", Channel: "C0123", Text: "lost", TimeStamp: "1"}
	err := tr.routeInbound(context.Background(), host, msg)
	if err == nil || !errors.Is(err, teamTransport.ErrBindingChannelMissing) {
		t.Fatalf("expected ErrBindingChannelMissing, got %v", err)
	}
}

// --- resolveUser ---

func TestSlackResolveUser(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U1"] = &slack.User{ID: "U1", Profile: slack.UserProfile{DisplayName: "alice"}, IsBot: false}
	api.users["UBOTUSER"] = &slack.User{ID: "UBOTUSER", RealName: "Helper Bot", IsBot: true}
	tr, _ := newTestSlackTransport(t, "C0123", api)

	name, human := tr.resolveUser(context.Background(), "U1")
	if name != "alice" || !human {
		t.Fatalf("U1 = (%q, %v), want (alice, true)", name, human)
	}
	name, human = tr.resolveUser(context.Background(), "UBOTUSER")
	if name != "Helper Bot" || human {
		t.Fatalf("UBOTUSER = (%q, %v), want (Helper Bot, false)", name, human)
	}
	// Unknown user falls back to its id and is treated as human.
	name, human = tr.resolveUser(context.Background(), "UMISSING")
	if name != "UMISSING" || !human {
		t.Fatalf("UMISSING = (%q, %v), want (UMISSING, true)", name, human)
	}
	// Empty id.
	name, _ = tr.resolveUser(context.Background(), "")
	if name != "unknown" {
		t.Fatalf("empty user id = %q, want unknown", name)
	}
}

// --- Outbound: Send + FormatOutbound ---

func TestSlackFormatOutbound(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())

	out, ok := tr.FormatOutbound(channelMessage{Channel: "slack-general", From: "ceo", Title: "Update", Content: "All good"})
	if !ok {
		t.Fatal("expected ok=true for mapped channel")
	}
	if out.Text != "*@ceo*: [Update] All good" {
		t.Fatalf("text = %q", out.Text)
	}
	if out.Binding.Scope != teamTransport.ScopeChannel || out.Binding.ChannelSlug != "slack-general" {
		t.Fatalf("binding = %+v", out.Binding)
	}

	// Unmapped channel: ok=false, no panic.
	if _, ok := tr.FormatOutbound(channelMessage{Channel: "nope", Content: "x"}); ok {
		t.Fatal("expected ok=false for unmapped channel")
	}
}

func TestSlackSend(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	err := tr.Send(context.Background(), teamTransport.Outbound{
		Binding: teamTransport.Binding{Scope: teamTransport.ScopeChannel, ChannelSlug: "slack-general"},
		Text:    "*@pm*: shipping it",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].ChannelID != "C0123" || posts[0].Text != "*@pm*: shipping it" {
		t.Fatalf("post = %+v", posts[0])
	}
	if posts[0].ThreadTS != "" {
		t.Fatalf("expected no thread ts, got %q", posts[0].ThreadTS)
	}
}

func TestSlackSendThreaded(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	err := tr.Send(context.Background(), teamTransport.Outbound{
		Binding:   teamTransport.Binding{Scope: teamTransport.ScopeChannel, ChannelSlug: "slack-general"},
		Text:      "reply",
		ThreadKey: "1700000000.0001",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 1 || posts[0].ThreadTS != "1700000000.0001" {
		t.Fatalf("expected threaded post, got %+v", posts)
	}
}

func TestSlackSendUnmappedChannel(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	err := tr.Send(context.Background(), teamTransport.Outbound{
		Binding: teamTransport.Binding{Scope: teamTransport.ScopeChannel, ChannelSlug: "does-not-exist"},
		Text:    "x",
	})
	if err == nil || !contains(err.Error(), "no channel mapped") {
		t.Fatalf("expected no-channel error, got %v", err)
	}
}

func TestSlackSendAPIError(t *testing.T) {
	api := newFakeSlackAPI()
	api.postErr = errors.New("rate limited")
	tr, _ := newTestSlackTransport(t, "C0123", api)
	err := tr.Send(context.Background(), teamTransport.Outbound{
		Binding: teamTransport.Binding{Scope: teamTransport.ScopeChannel, ChannelSlug: "slack-general"},
		Text:    "x",
	})
	if err == nil || !contains(err.Error(), "rate limited") {
		t.Fatalf("expected wrapped API error, got %v", err)
	}
}

func TestSlackFormatOutboundKinds(t *testing.T) {
	cases := []struct {
		name string
		msg  channelMessage
		want string
	}{
		{"agent", channelMessage{From: "pm", Content: "hi"}, "*@pm*: hi"},
		{"system", channelMessage{From: "system", Content: "routing"}, "→ _routing_"},
		{"automation", channelMessage{Kind: "automation", Source: "github", Content: "merged"}, "🤖 *[github]*: merged"},
		{"skill", channelMessage{Kind: "skill_invocation", From: "ceo", Content: "x"}, "⚡ *@ceo* invoked a skill"},
		{"proposal", channelMessage{Kind: "skill_proposal", Content: "auto-deploy"}, "💡 *Skill proposed*: auto-deploy"},
		{"decision", channelMessage{Kind: "interview", From: "ceo", Content: "ship?", Title: "Release"}, "📋 *Decision needed* from @ceo\n\nship?\n\n_Release_"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSlackOutbound(tc.msg); got != tc.want {
				t.Fatalf("formatSlackOutbound = %q, want %q", got, tc.want)
			}
		})
	}
}

// slackOrderingHost wraps a Host and records the call order + last
// participant/binding so a test can prove UpsertParticipant happens before
// ReceiveMessage. (The package's share-test recordingHost is no-op for those two
// methods, so it cannot witness ordering — hence a dedicated wrapper here.)
type slackOrderingHost struct {
	inner           teamTransport.Host
	order           string
	lastParticipant teamTransport.Participant
	lastBinding     teamTransport.Binding
}

func (h *slackOrderingHost) ReceiveMessage(ctx context.Context, msg teamTransport.Message) error {
	if h.order == "" {
		h.order = "receive"
	} else {
		h.order += ",receive"
	}
	return h.inner.ReceiveMessage(ctx, msg)
}

func (h *slackOrderingHost) UpsertParticipant(ctx context.Context, p teamTransport.Participant, b teamTransport.Binding) error {
	if h.order == "" {
		h.order = "upsert"
	} else {
		h.order += ",upsert"
	}
	h.lastParticipant = p
	h.lastBinding = b
	return h.inner.UpsertParticipant(ctx, p, b)
}

func (h *slackOrderingHost) DetachParticipant(ctx context.Context, adapterName, key string) error {
	return h.inner.DetachParticipant(ctx, adapterName, key)
}

func (h *slackOrderingHost) RevokeParticipant(ctx context.Context, adapterName, key string) error {
	return h.inner.RevokeParticipant(ctx, adapterName, key)
}

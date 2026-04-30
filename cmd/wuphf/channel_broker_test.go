package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// brokerStub stands up an httptest.Server, points the broker base URL at it
// for the test, and resets WUPHF_BROKER_BASE_URL on teardown.
func brokerStub(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)
	t.Cleanup(srv.Close)
	return srv
}

func TestPollHealthSuccess(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","session_mode":"office","one_on_one_agent":""}`))
	}))

	cmd := pollHealth()
	if cmd == nil {
		t.Fatal("pollHealth returned nil cmd")
	}
	msg, ok := cmd().(channelHealthMsg)
	if !ok {
		t.Fatalf("expected channelHealthMsg, got %T", cmd())
	}
	if !msg.Connected {
		t.Fatalf("expected Connected=true, got %#v", msg)
	}
	if msg.SessionMode != "office" {
		t.Fatalf("expected session mode 'office', got %q", msg.SessionMode)
	}
}

func TestPollHealthRejectsNon200(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	cmd := pollHealth()
	msg, ok := cmd().(channelHealthMsg)
	if !ok {
		t.Fatalf("expected channelHealthMsg, got %T", cmd())
	}
	if msg.Connected {
		t.Fatalf("non-200 must not be reported as Connected")
	}
}

func TestPollBrokerDecodesMessages(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"messages":[{"id":"m1","from":"fe","content":"hi","timestamp":"2026-04-29T10:00:00Z"}]}`))
	}))

	msg, ok := pollBroker("", "office")().(channelMsg)
	if !ok {
		t.Fatalf("expected channelMsg, got %T", pollBroker("", "office")())
	}
	if len(msg.messages) != 1 || msg.messages[0].ID != "m1" {
		t.Fatalf("expected one message m1, got %#v", msg.messages)
	}
}

func TestPollBrokerSinceIDInQuery(t *testing.T) {
	var seen string
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))

	pollBroker("m42", "office")()
	if !strings.Contains(seen, "since_id=m42") {
		t.Fatalf("expected since_id=m42 in query, got %q", seen)
	}
	if !strings.Contains(seen, "channel=office") {
		t.Fatalf("expected channel=office in query, got %q", seen)
	}
}

func TestPollMembersDecodes(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"members":[{"slug":"fe","name":"Frontend"},{"slug":"be","name":"Backend"}]}`))
	}))

	msg, ok := pollMembers("office")().(channelMembersMsg)
	if !ok {
		t.Fatalf("expected channelMembersMsg, got %T", pollMembers("office")())
	}
	if len(msg.members) != 2 || msg.members[0].Slug != "fe" {
		t.Fatalf("expected fe + be, got %#v", msg.members)
	}
}

func TestPollChannelsDecodes(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"channels":[{"slug":"office","name":"Office","members":["fe","be"]}]}`))
	}))

	msg, ok := pollChannels()().(channelChannelsMsg)
	if !ok {
		t.Fatalf("expected channelChannelsMsg, got %T", pollChannels()())
	}
	if len(msg.channels) != 1 || msg.channels[0].Slug != "office" {
		t.Fatalf("expected one channel 'office', got %#v", msg.channels)
	}
}

func TestPollUsageEnsuresAgentsMap(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total":{"total_tokens":10}}`))
	}))

	msg, ok := pollUsage()().(channelUsageMsg)
	if !ok {
		t.Fatalf("expected channelUsageMsg, got %T", pollUsage()())
	}
	if msg.usage.Total.TotalTokens != 10 {
		t.Fatalf("expected total tokens 10, got %#v", msg.usage)
	}
	if msg.usage.Agents == nil {
		t.Fatalf("Agents map must be non-nil to be safe to read by callers")
	}
}

func TestCreateDMChannelPostsMembers(t *testing.T) {
	var posted struct {
		Members []string `json:"members"`
		Type    string   `json:"type"`
	}
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/dm" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &posted)
		_, _ = w.Write([]byte(`{"slug":"office__fe","name":"Frontend"}`))
	}))

	msg, ok := createDMChannel("fe")().(channelDMCreatedMsg)
	if !ok {
		t.Fatalf("expected channelDMCreatedMsg, got %T", createDMChannel("fe")())
	}
	if msg.err != nil {
		t.Fatalf("expected no error, got %v", msg.err)
	}
	if msg.slug != "office__fe" || msg.name != "Frontend" || msg.agentSlug != "fe" {
		t.Fatalf("unexpected msg: %#v", msg)
	}
	if posted.Type != "direct" {
		t.Fatalf("expected type 'direct', got %q", posted.Type)
	}
	if len(posted.Members) != 2 || posted.Members[0] != "human" || posted.Members[1] != "fe" {
		t.Fatalf("expected [human, fe] members, got %#v", posted.Members)
	}
}

func TestCreateDMChannelHandlesMalformedJSON(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))

	msg, _ := createDMChannel("fe")().(channelDMCreatedMsg)
	if msg.err == nil {
		t.Fatalf("expected decode error to surface, got nil")
	}
	if msg.agentSlug != "fe" {
		t.Fatalf("agent slug should be carried even on error, got %q", msg.agentSlug)
	}
}

func TestMutateTaskClaimReturnsNotice(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	msg, ok := mutateTask("claim", "task-1", "fe", "office")().(channelTaskMutationDoneMsg)
	if !ok {
		t.Fatalf("expected channelTaskMutationDoneMsg")
	}
	if msg.err != nil {
		t.Fatalf("expected nil err, got %v", msg.err)
	}
	if msg.notice != "Task claimed." {
		t.Fatalf("expected 'Task claimed.', got %q", msg.notice)
	}
}

func TestMutateTaskUnknownActionFallsBackToGenericNotice(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	msg := mutateTask("nudge", "task-1", "fe", "office")().(channelTaskMutationDoneMsg)
	if msg.notice != "Task updated." {
		t.Fatalf("expected fallback notice, got %q", msg.notice)
	}
}

func TestMutateTaskNon2xxSurfacesErrorBody(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad action"))
	}))

	msg := mutateTask("claim", "task-1", "fe", "office")().(channelTaskMutationDoneMsg)
	if msg.err == nil || msg.err.Error() != "bad action" {
		t.Fatalf("expected 'bad action' error, got %v", msg.err)
	}
}

func TestPostHumanInterruptSucceedsOn2xx(t *testing.T) {
	var seen map[string]any
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/requests" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		w.WriteHeader(http.StatusOK)
	}))

	msg := postHumanInterrupt("office")().(channelInterruptDoneMsg)
	if msg.err != nil {
		t.Fatalf("expected nil err, got %v", msg.err)
	}
	if seen["kind"] != "interrupt" || seen["channel"] != "office" || seen["blocking"] != true {
		t.Fatalf("unexpected interrupt payload: %#v", seen)
	}
}

func TestCancelRequestSurfacesNon2xxBody(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("already cancelled"))
	}))

	interview := channelui.Interview{ID: "req-1"}
	msg := cancelRequest(interview)().(channelCancelDoneMsg)
	if msg.err == nil || msg.err.Error() != "already cancelled" {
		t.Fatalf("expected 'already cancelled' error, got %v", msg.err)
	}
	if msg.requestID != "req-1" {
		t.Fatalf("expected requestID echoed back, got %q", msg.requestID)
	}
}

func TestCancelRequestEmptyBodyUsesStatus(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	msg := cancelRequest(channelui.Interview{ID: "req-2"})().(channelCancelDoneMsg)
	if msg.err == nil {
		t.Fatalf("expected error for empty 5xx body")
	}
	if !strings.Contains(msg.err.Error(), "broker returned") {
		t.Fatalf("expected fallback message containing 'broker returned', got %q", msg.err.Error())
	}
}

func TestPostInterviewAnswerSendsChoiceAndCustomText(t *testing.T) {
	var posted map[string]any
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/requests/answer" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &posted)
		w.WriteHeader(http.StatusOK)
	}))

	msg := postInterviewAnswer(channelui.Interview{ID: "req-7"}, "yes", "Yes", "with this twist")().(channelInterviewAnswerDoneMsg)
	if msg.err != nil {
		t.Fatalf("expected nil err, got %v", msg.err)
	}
	if posted["id"] != "req-7" || posted["choice_id"] != "yes" ||
		posted["choice_text"] != "Yes" || posted["custom_text"] != "with this twist" {
		t.Fatalf("unexpected answer payload: %#v", posted)
	}
}

func TestPostInterviewAnswerSurfacesErrorBody(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("not allowed"))
	}))

	msg := postInterviewAnswer(channelui.Interview{ID: "req-8"}, "no", "No", "")().(channelInterviewAnswerDoneMsg)
	if msg.err == nil || msg.err.Error() != "not allowed" {
		t.Fatalf("expected 'not allowed' error, got %v", msg.err)
	}
}

func TestPollActionsDecodes(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"actions":[{"id":"a1","kind":"github_pr_opened","summary":"opened"}]}`))
	}))

	msg := pollActions()().(channelActionsMsg)
	if len(msg.actions) != 1 || msg.actions[0].ID != "a1" {
		t.Fatalf("expected one action a1, got %#v", msg.actions)
	}
}

func TestPollSignalsDecodes(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"signals":[{"id":"s1","content":"latency spike"}]}`))
	}))

	msg := pollSignals()().(channelSignalsMsg)
	if len(msg.signals) != 1 || msg.signals[0].ID != "s1" {
		t.Fatalf("expected one signal s1, got %#v", msg.signals)
	}
}

func TestPollDecisionsDecodes(t *testing.T) {
	brokerStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"decisions":[{"id":"d1","summary":"ship"}]}`))
	}))

	msg := pollDecisions()().(channelDecisionsMsg)
	if len(msg.decisions) != 1 || msg.decisions[0].ID != "d1" {
		t.Fatalf("expected one decision d1, got %#v", msg.decisions)
	}
}

func TestPollOnNetworkFailuresReturnsZeroValueMessages(t *testing.T) {
	// Server immediately closed: every poll should swallow the error and
	// return an empty msg of its type, not panic.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // intentionally closed
	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)

	if msg := pollBroker("", "office")().(channelMsg); len(msg.messages) != 0 {
		t.Fatalf("expected empty channelMsg on network error, got %#v", msg)
	}
	if msg := pollMembers("office")().(channelMembersMsg); len(msg.members) != 0 {
		t.Fatalf("expected empty channelMembersMsg, got %#v", msg)
	}
	if msg := pollChannels()().(channelChannelsMsg); len(msg.channels) != 0 {
		t.Fatalf("expected empty channelChannelsMsg, got %#v", msg)
	}
}

func TestNormalizeBrokerURLRewritesLocalhost(t *testing.T) {
	t.Setenv("WUPHF_BROKER_BASE_URL", "http://broker.test:9000")
	if got := normalizeBrokerURL("http://127.0.0.1:7890/messages"); got != "http://broker.test:9000/messages" {
		t.Fatalf("expected 127.0.0.1 rewrite, got %q", got)
	}
	if got := normalizeBrokerURL("http://localhost:7890/messages"); got != "http://broker.test:9000/messages" {
		t.Fatalf("expected localhost rewrite, got %q", got)
	}
}

func TestBrokerURLPrefixesBase(t *testing.T) {
	t.Setenv("WUPHF_BROKER_BASE_URL", "http://broker.test:9000")
	if got := brokerURL("/health"); got != "http://broker.test:9000/health" {
		t.Fatalf("expected base+path, got %q", got)
	}
}

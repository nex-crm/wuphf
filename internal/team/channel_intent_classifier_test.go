package team

import (
	"strings"
	"testing"
)

// TestClassifyChannelIntent_Matches covers the full set of supported
// question-form context-ask patterns. Each row is a positive example: the
// classifier must match and extract the topic phrase shown in want.
func TestClassifyChannelIntent_Matches(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantKind  channelIntentKind
		wantTopic string
	}{
		{
			name:      "who has context on",
			body:      "who has context on our onboarding flow",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "our onboarding flow",
		},
		{
			name:      "who knows about (no question mark)",
			body:      "who knows about the billing migration",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the billing migration",
		},
		{
			name:      "who has notes on with trailing question mark",
			body:      "who has notes on the auth incident?",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the auth incident",
		},
		{
			name:      "does anyone know about",
			body:      "does anyone know about our pricing experiments",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "our pricing experiments",
		},
		{
			name:      "does anyone remember what we decided about",
			body:      "does anyone remember what we decided about retro cadence",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "retro cadence",
		},
		{
			name:      "anyone know about (informal)",
			body:      "anyone know about the deployment gotchas",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the deployment gotchas",
		},
		{
			name:      "what do we have on",
			body:      "what do we have on the API rate limits",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the API rate limits",
		},
		{
			name:      "what did we decide about",
			body:      "what did we decide about feature flag naming",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "feature flag naming",
		},
		{
			name:      "where can I find context on",
			body:      "where can I find context on our launch checklist",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "our launch checklist",
		},
		{
			name:      "do we have notes on",
			body:      "do we have notes on the incident response runbook",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the incident response runbook",
		},
		{
			name:      "case-insensitive opener",
			body:      "WHO HAS CONTEXT ON our ICP definition",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "our ICP definition",
		},
		{
			name:      "leading whitespace tolerated",
			body:      "   who has context on the founding story",
			wantKind:  ChannelIntentContextAsk,
			wantTopic: "the founding story",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyChannelIntent(tc.body)
			if !ok {
				t.Fatalf("expected match for %q, got none", tc.body)
			}
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Topic != tc.wantTopic {
				t.Fatalf("topic = %q, want %q", got.Topic, tc.wantTopic)
			}
		})
	}
}

// TestClassifyChannelIntent_NoMatch covers the negative cases: statement
// form, agent technical chatter, code fences, URLs-as-topics, and topics
// that are too short to be useful.
func TestClassifyChannelIntent_NoMatch(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "whitespace only", body: "   \n\t  "},
		{
			name: "statement form first person",
			body: "I have context on the onboarding flow",
		},
		{
			name: "statement form we know",
			body: "we know about the billing migration",
		},
		{
			name: "statement form anyone can",
			body: "anyone can see this",
		},
		{
			name: "we have docs (statement)",
			body: "we have good docs on the API rate limits",
		},
		{
			name: "no topic — single token",
			body: "who has context on X",
		},
		{
			name: "code fence wrapping the question",
			body: "```\nwho has context on our onboarding flow\n```",
		},
		{
			name: "inline backtick wrapping the verb",
			body: "`who has context on` our onboarding",
		},
		{
			name: "URL stripped leaves no topic",
			body: "who has context on https://example.com",
		},
		{
			name: "bare 'remember' — historical, not a question",
			body: "remember when we shipped the auth PR",
		},
		{
			name: "agent technical output: who has access",
			body: "who has access to the prod database",
		},
		{
			name: "agent technical output: what is X",
			body: "what is the current head sha",
		},
		{
			name: "wrong opener",
			body: "should we add notes on the launch checklist",
		},
		{
			name: "question form but no recognised verb",
			body: "who likes pizza on friday",
		},
		{
			name: "question on later line, log first",
			body: "ERROR: build failed at step 3\nstack trace blah\nwho has context on the build",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := classifyChannelIntent(tc.body); ok {
				t.Fatalf("expected no match for %q, got %+v", tc.body, got)
			}
		})
	}
}

// TestClassifyChannelIntent_TopicCleanup verifies trailing punctuation is
// dropped, whitespace is collapsed, and overlong topics are truncated.
func TestClassifyChannelIntent_TopicCleanup(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantTopic string
	}{
		{
			name:      "trailing question mark dropped",
			body:      "who has context on our onboarding flow?",
			wantTopic: "our onboarding flow",
		},
		{
			name:      "trailing period dropped",
			body:      "what do we know about the launch.",
			wantTopic: "the launch",
		},
		{
			name:      "internal whitespace collapsed",
			body:      "who has context on    our   onboarding   flow",
			wantTopic: "our onboarding flow",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyChannelIntent(tc.body)
			if !ok {
				t.Fatalf("expected match for %q", tc.body)
			}
			if got.Topic != tc.wantTopic {
				t.Fatalf("topic = %q, want %q", got.Topic, tc.wantTopic)
			}
		})
	}
}

// TestClassifyChannelIntent_TopicLengthCap exercises channelIntentTopicMaxLen.
// A topic longer than the cap is truncated; the result still passes the
// minimum-tokens guard.
func TestClassifyChannelIntent_TopicLengthCap(t *testing.T) {
	long := strings.Repeat("alpha beta ", 30) // ~330 chars, far above the cap
	body := "who has context on " + long
	got, ok := classifyChannelIntent(body)
	if !ok {
		t.Fatalf("expected match")
	}
	if len(got.Topic) > channelIntentTopicMaxLen {
		t.Fatalf("topic length = %d, want ≤ %d", len(got.Topic), channelIntentTopicMaxLen)
	}
	if len(strings.Fields(got.Topic)) < channelIntentTopicMinTokens {
		t.Fatalf("truncated topic %q lost minimum tokens", got.Topic)
	}
}

// TestRenderChannelIntentReply locks the optional reply format. Even though
// the reply path is OFF by default in PR 5, the renderer is a pure function
// and easy to pin so the format is reviewable independently.
func TestRenderChannelIntentReply(t *testing.T) {
	hits := []WikiSearchHit{
		{Path: "agents/pm/notebook/2026-05-06-icp.md", Line: 1, Snippet: "..."},
		{Path: "agents/eng/notebook/2026-05-05-onboarding.md", Line: 1, Snippet: "..."},
	}
	owners := []string{"pm", "eng"}
	out := renderChannelIntentReply("our onboarding flow", hits, owners)
	if !strings.Contains(out, "Found context on our onboarding flow") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "@pm") || !strings.Contains(out, "@eng") {
		t.Fatalf("expected both owners cited: %q", out)
	}
	if !strings.Contains(out, "agents/pm/notebook/2026-05-06-icp.md") {
		t.Fatalf("expected first path cited: %q", out)
	}
}

// TestRenderChannelIntentReply_Empty returns "" when no hits.
func TestRenderChannelIntentReply_Empty(t *testing.T) {
	if got := renderChannelIntentReply("topic", nil, nil); got != "" {
		t.Fatalf("expected empty reply, got %q", got)
	}
}

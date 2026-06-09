package team

import (
	"context"
	"errors"
	"testing"

	"github.com/nex-crm/wuphf/internal/packer"
)

// newTestDelegation builds a PackedDelegation with the given mention/thread text
// and destination. The packer normally seals delegations via render(); here we
// only need the exported fields the bridge reads (MentionText, ThreadContext,
// Injection.ChannelID, Injection.ThreadTS), so a literal is sufficient — the
// bridge is downstream of the seal check, which Deliver enforces, not Post.
func newTestDelegation(mention, thread, channelID, threadTS string) packer.PackedDelegation {
	return packer.PackedDelegation{
		MentionText:   mention,
		ThreadContext: thread,
		Injection: packer.InjectionRecord{
			ChannelID: channelID,
			ThreadTS:  threadTS,
		},
	}
}

func TestSlackBridgePostMentionOnly(t *testing.T) {
	api := newFakeSlackAPI()
	br := newSlackBridge(api)

	ts, err := br.Post(context.Background(), newTestDelegation("@pam please review", "", "C0123", ""), "key-1")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if ts == "" {
		t.Fatal("expected a non-empty message ts")
	}
	posts := api.snapshotPosts()
	if len(posts) != 1 {
		t.Fatalf("expected 1 post (mention only), got %d", len(posts))
	}
	if posts[0].ChannelID != "C0123" || posts[0].Text != "@pam please review" {
		t.Fatalf("mention post = %+v", posts[0])
	}
	if posts[0].ThreadTS != "" {
		t.Fatalf("expected mention to start a new thread, got thread_ts %q", posts[0].ThreadTS)
	}
}

func TestSlackBridgePostThreadContextAsReply(t *testing.T) {
	api := newFakeSlackAPI()
	br := newSlackBridge(api)

	// No existing thread ts: the mention starts the thread and the follow-up must
	// reply under the mention's returned ts.
	ts, err := br.Post(context.Background(), newTestDelegation("@pam mention", "full thread context here", "C0123", ""), "key-2")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts (mention + thread), got %d", len(posts))
	}
	if posts[0].Text != "@pam mention" {
		t.Fatalf("first post should be the mention, got %q", posts[0].Text)
	}
	if posts[1].Text != "full thread context here" {
		t.Fatalf("second post should be the thread context, got %q", posts[1].Text)
	}
	if posts[1].ThreadTS != ts {
		t.Fatalf("thread follow-up thread_ts = %q, want mention ts %q", posts[1].ThreadTS, ts)
	}
}

func TestSlackBridgePostIntoExistingThread(t *testing.T) {
	api := newFakeSlackAPI()
	br := newSlackBridge(api)

	// An existing thread ts: BOTH the mention and the thread context anchor to it.
	_, err := br.Post(context.Background(), newTestDelegation("@pam mention", "context", "C0123", "1699999999.0001"), "key-3")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].ThreadTS != "1699999999.0001" {
		t.Fatalf("mention thread_ts = %q, want the existing thread anchor", posts[0].ThreadTS)
	}
	if posts[1].ThreadTS != "1699999999.0001" {
		t.Fatalf("thread context thread_ts = %q, want the existing thread anchor", posts[1].ThreadTS)
	}
}

func TestSlackBridgePostEmptyChannel(t *testing.T) {
	br := newSlackBridge(newFakeSlackAPI())
	_, err := br.Post(context.Background(), newTestDelegation("@pam", "", "", ""), "key-4")
	if err == nil || !contains(err.Error(), "empty channel id") {
		t.Fatalf("expected empty channel error, got %v", err)
	}
}

func TestSlackBridgePostEmptyMention(t *testing.T) {
	br := newSlackBridge(newFakeSlackAPI())
	_, err := br.Post(context.Background(), newTestDelegation("   ", "thread", "C0123", ""), "key-5")
	if err == nil || !contains(err.Error(), "empty mention text") {
		t.Fatalf("expected empty mention error, got %v", err)
	}
}

func TestSlackBridgePostMentionAPIError(t *testing.T) {
	api := newFakeSlackAPI()
	api.postErr = errors.New("channel_not_found")
	br := newSlackBridge(api)

	_, err := br.Post(context.Background(), newTestDelegation("@pam", "ctx", "C0123", ""), "key-6")
	if err == nil || !contains(err.Error(), "post mention") || !contains(err.Error(), "channel_not_found") {
		t.Fatalf("expected wrapped mention error, got %v", err)
	}
	if len(api.snapshotPosts()) != 0 {
		t.Fatal("no post should be recorded when the mention call fails")
	}
}

func TestSlackBridgePostEscapesControlSequences(t *testing.T) {
	api := newFakeSlackAPI()
	br := newSlackBridge(api)

	// A tainted Ask could smuggle Slack control sequences into the egress text.
	// The bridge posts with escapeText=true so &<> are neutralized — no mass-ping
	// (<!channel>/<!here>) and no fake <url|label> link reaches the workspace.
	_, err := br.Post(context.Background(),
		newTestDelegation("ping <!channel> and <http://evil|click here>", "also <!here>", "C0123", ""), "key-escape")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	for _, p := range posts {
		if contains(p.Text, "<!channel>") || contains(p.Text, "<!here>") || contains(p.Text, "<http://evil|click here>") {
			t.Fatalf("egress text must escape Slack control sequences, got %q", p.Text)
		}
	}
	if !contains(posts[0].Text, "&lt;!channel&gt;") {
		t.Fatalf("expected escaped control sequence in mention, got %q", posts[0].Text)
	}
}

func TestSlackBridgePostThreadContextErrorReturnsMentionTS(t *testing.T) {
	api := newFakeSlackAPI()
	// Fail only the SECOND post (the thread follow-up); the mention succeeds.
	api.postErr = errors.New("thread_locked")
	api.postErrAt = 2
	br := newSlackBridge(api)

	ts, err := br.Post(context.Background(), newTestDelegation("@pam", "ctx", "C0123", ""), "key-7")
	if err == nil || !contains(err.Error(), "post thread context") {
		t.Fatalf("expected wrapped thread error, got %v", err)
	}
	// The mention landed, so its ts must still be returned for the audit anchor.
	if ts == "" {
		t.Fatal("expected the mention ts to be returned even when the follow-up fails")
	}
}

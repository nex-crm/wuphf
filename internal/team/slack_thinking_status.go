package team

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slack_thinking_status.go surfaces WUPHF's internal "agent is working" signal in
// Slack, so a reader sees "Scout is thinking…" in a task thread while a turn runs
// instead of dead air until the reply.
//
// Mechanism: a posted-then-deleted in-thread message. Slack's NATIVE AI status
// (assistant.threads.setStatus) only renders inside the 1:1 Assistant container
// (the dedicated assistant pane/DM), NOT a shared channel thread — and the office
// lives in a shared channel. So when an agent goes active we POST a transient
// "💭 <name> is thinking…" message into that agent's running task threads, and
// DELETE it when the agent goes idle (the real reply posts separately). This
// reproduces the native "working → answer" feel where the office actually lives.
//
// It subscribes to the broker activity stream (the same signal the web UI shows
// as "planning next step"). Best-effort: a post/delete failure is logged, never
// fatal, so the rest of the bridge is unaffected.

// slackTaskThreadRef locates a task's Slack thread root.
type slackTaskThreadRef struct {
	TaskID    string
	ChannelID string
	ThreadTS  string
}

// ActiveSlackTaskThreadsForOwner returns the Slack thread roots of executable
// (Running/Approved) tasks owned by slug that have a posted card — the threads
// where "owner is thinking…" belongs while the owner runs a turn.
func (b *Broker) ActiveSlackTaskThreadsForOwner(slug string) []slackTaskThreadRef {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []slackTaskThreadRef
	for i := range b.tasks {
		tk := &b.tasks[i]
		if !strings.EqualFold(strings.TrimSpace(tk.Owner), slug) {
			continue
		}
		if !isExecutableTeamTaskStatus(tk.LifecycleState) {
			continue
		}
		rec, ok := b.slackTaskCards[tk.ID]
		if !ok || rec.ChannelID == "" || rec.Timestamp == "" {
			continue
		}
		out = append(out, slackTaskThreadRef{TaskID: tk.ID, ChannelID: rec.ChannelID, ThreadTS: rec.Timestamp})
	}
	return out
}

// isThinkingActivity reports whether an activity snapshot means the agent is
// actively running a turn. The headless runners stamp Status "active" throughout
// a turn (thinking / text / tool_use) and "idle" when it ends.
func isThinkingActivity(snap agentActivitySnapshot) bool {
	return strings.EqualFold(strings.TrimSpace(snap.Status), "active")
}

// runAgentThinkingStatus mirrors the broker activity stream onto a transient
// in-thread "working…" message. It blocks until ctx is cancelled.
func (t *SlackTransport) runAgentThinkingStatus(ctx context.Context) error {
	if t.Broker == nil || t.api == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	activity, unsub := t.Broker.SubscribeActivity(256)
	defer unsub()

	// posted maps a thread (key = "chan|ts") to the message ts of the indicator
	// currently shown there, so the activity stream's many per-turn events only
	// ever post one indicator and delete it once.
	posted := map[string]string{}
	apply := func(ref slackTaskThreadRef, text string) {
		key := ref.ChannelID + "|" + ref.ThreadTS
		if text != "" {
			if _, already := posted[key]; already {
				return // indicator already shown for this thread
			}
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, ts, err := t.api.PostMessageContext(cctx, ref.ChannelID,
				slack.MsgOptionText(text, false),
				slack.MsgOptionTS(ref.ThreadTS),
			)
			cancel()
			if err != nil {
				log.Printf("[slack] thinking indicator post for %s: %v", ref.TaskID, err)
				return
			}
			posted[key] = ts
			return
		}
		// Idle: clear the indicator if one is up.
		ts, up := posted[key]
		if !up {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, _, err := t.api.DeleteMessageContext(cctx, ref.ChannelID, ts)
		cancel()
		if err != nil {
			log.Printf("[slack] thinking indicator clear for %s: %v", ref.TaskID, err)
			return
		}
		delete(posted, key)
	}

	for {
		select {
		case <-ctx.Done():
			// Clear any lingering indicators on shutdown (best-effort, detached ctx).
			for key, ts := range posted {
				if c, _, ok := splitThreadKey(key); ok {
					_, _, _ = t.api.DeleteMessageContext(context.Background(), c, ts)
				}
			}
			return ctx.Err()
		case snap, ok := <-activity:
			if !ok {
				return nil
			}
			refs := t.Broker.ActiveSlackTaskThreadsForOwner(snap.Slug)
			if len(refs) == 0 {
				continue
			}
			text := ""
			if isThinkingActivity(snap) {
				text = t.thinkingStatusText(snap.Slug)
			}
			for _, ref := range refs {
				apply(ref, text)
			}
		}
	}
}

func splitThreadKey(key string) (channelID, threadTS string, ok bool) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// thinkingStatusText renders the status line for an agent, e.g.
// "Scout (RevOps Agent) is thinking…", preferring the member's display name.
func (t *SlackTransport) thinkingStatusText(slug string) string {
	name := strings.TrimSpace(slug)
	if t.Broker != nil {
		if names := t.Broker.MemberDisplayNames(); names != nil {
			if n := strings.TrimSpace(names[slug]); n != "" {
				name = n
			}
		}
	}
	return fmt.Sprintf("💭 %s is thinking…", name)
}

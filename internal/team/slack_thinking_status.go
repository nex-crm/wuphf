package team

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slack_thinking_status.go surfaces WUPHF's internal "agent is working" signal as
// Slack's native AI status (assistant.threads.setStatus), so a Slack reader sees
// "Scout is thinking…" while a turn runs instead of dead air until the reply.
//
// It subscribes to the broker activity stream (the same signal the web UI shows
// as "planning next step") and, when an agent goes active, sets the status on
// that agent's running Slack task threads; it clears when the agent goes idle.
// Best-effort: a status failure (e.g. the app missing the assistant:write scope)
// is logged, never fatal, so the rest of the bridge is unaffected.

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

// runAgentThinkingStatus mirrors the broker activity stream onto Slack's native
// assistant status. It blocks until ctx is cancelled.
func (t *SlackTransport) runAgentThinkingStatus(ctx context.Context) error {
	if t.Broker == nil || t.api == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	activity, unsub := t.Broker.SubscribeActivity(256)
	defer unsub()

	// shown tracks the status text currently set per thread (key = "chan|ts"),
	// so we only call Slack when it actually changes — the activity stream emits
	// many events per turn.
	shown := map[string]string{}
	apply := func(ref slackTaskThreadRef, text string) {
		key := ref.ChannelID + "|" + ref.ThreadTS
		if shown[key] == text {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := t.api.SetAssistantThreadsStatusContext(cctx, slack.AssistantThreadsSetStatusParameters{
			ChannelID: ref.ChannelID,
			ThreadTS:  ref.ThreadTS,
			Status:    text,
		})
		cancel()
		if err != nil {
			log.Printf("[slack] thinking status for %s: %v", ref.TaskID, err)
			return
		}
		if text == "" {
			delete(shown, key)
		} else {
			shown[key] = text
		}
	}

	for {
		select {
		case <-ctx.Done():
			// Clear any lingering status on shutdown (best-effort, detached ctx).
			for key := range shown {
				if c, ts, ok := splitThreadKey(key); ok {
					_ = t.api.SetAssistantThreadsStatusContext(context.Background(), slack.AssistantThreadsSetStatusParameters{
						ChannelID: c, ThreadTS: ts, Status: "",
					})
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
	return fmt.Sprintf("%s is thinking…", name)
}

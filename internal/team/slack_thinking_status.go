package team

import (
	"context"
	"fmt"
	"log"
	"sort"
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
// where "owner is thinking…" belongs while the owner runs a turn. Sorted most-
// recently-updated first, so callers that want a single best-guess "thread the
// owner is working in now" (the thinking indicator) can take the first element
// without fanning out across every task the owner happens to still own.
func (b *Broker) ActiveSlackTaskThreadsForOwner(slug string) []slackTaskThreadRef {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	type scored struct {
		ref     slackTaskThreadRef
		updated string
	}
	var rows []scored
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
		rows = append(rows, scored{
			ref:     slackTaskThreadRef{TaskID: tk.ID, ChannelID: rec.ChannelID, ThreadTS: rec.Timestamp},
			updated: tk.UpdatedAt,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].updated > rows[j].updated })
	out := make([]slackTaskThreadRef, len(rows))
	for i := range rows {
		out[i] = rows[i].ref
	}
	return out
}

// isThinkingActivity reports whether an activity snapshot means the agent is
// actively running a turn. The headless runners stamp Status "active" throughout
// a turn (thinking / text / tool_use) and "idle" when it ends.
func isThinkingActivity(snap agentActivitySnapshot) bool {
	return strings.EqualFold(strings.TrimSpace(snap.Status), "active")
}

// runAgentThinkingStatus mirrors the broker activity stream onto Slack's NATIVE
// assistant status (assistant.threads.setStatus). It blocks until ctx is
// cancelled.
//
// Native status is the right mechanism here precisely because it costs ZERO
// channel messages: it shows "<name> is thinking…" in the thread's composer
// footer while a turn runs and clears the instant the agent goes idle, without
// posting (and then deleting) a real message. Posting status messages is the
// notification-overwhelm / rate-limit trap we are avoiding — a status line
// cannot be cleanly retracted and pings every thread follower. The call is
// best-effort: if the app lacks the Assistant feature / assistant:write scope
// the status simply does not render (logged at most once), and the office stays
// quiet rather than falling back to posting messages.
func (t *SlackTransport) runAgentThinkingStatus(ctx context.Context) error {
	if t.Broker == nil || t.api == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	activity, unsub := t.Broker.SubscribeActivity(256)
	defer unsub()

	// shown tracks the single thread (per OWNER) the status is currently set on,
	// so we set/clear exactly once per turn. Scoping per-owner (the most-recently-
	// updated active thread) keeps us from setting status on every task an owner
	// still holds.
	type statusRef struct{ channelID, threadTS string }
	shown := map[string]statusRef{}
	setStatus := func(channelID, threadTS, text string) error {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return t.api.SetAssistantThreadsStatusContext(cctx, slack.AssistantThreadsSetStatusParameters{
			ChannelID: channelID, ThreadTS: threadTS, Status: text,
		})
	}
	clear := func(slug string) {
		ref, up := shown[slug]
		if !up {
			return
		}
		if err := setStatus(ref.channelID, ref.threadTS, ""); err != nil {
			log.Printf("[slack] thinking status clear for %s: %v", slug, err)
			return
		}
		delete(shown, slug)
	}

	for {
		select {
		case <-ctx.Done():
			// Clear any lingering status on shutdown (best-effort, detached ctx).
			for _, ref := range shown {
				_ = t.api.SetAssistantThreadsStatusContext(context.Background(), slack.AssistantThreadsSetStatusParameters{
					ChannelID: ref.channelID, ThreadTS: ref.threadTS, Status: "",
				})
			}
			return ctx.Err()
		case snap, ok := <-activity:
			if !ok {
				return nil
			}
			slug := snap.Slug
			if !isThinkingActivity(snap) {
				clear(slug)
				continue
			}
			// Active. Set the status on the owner's most-recently-updated task
			// thread (refs[0]); if it's already set for this owner, do nothing.
			if _, already := shown[slug]; already {
				continue
			}
			refs := t.Broker.ActiveSlackTaskThreadsForOwner(slug)
			if len(refs) == 0 {
				continue
			}
			ref := refs[0]
			if err := setStatus(ref.ChannelID, ref.ThreadTS, t.thinkingStatusText(slug)); err != nil {
				log.Printf("[slack] thinking status for %s: %v", ref.TaskID, err)
				continue
			}
			shown[slug] = statusRef{channelID: ref.ChannelID, threadTS: ref.ThreadTS}
		}
	}
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

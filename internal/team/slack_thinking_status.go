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

// runAgentThinkingStatus mirrors the broker activity stream onto a transient
// in-thread "working…" message. It blocks until ctx is cancelled.
func (t *SlackTransport) runAgentThinkingStatus(ctx context.Context) error {
	if t.Broker == nil || t.api == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	activity, unsub := t.Broker.SubscribeActivity(256)
	defer unsub()

	// posted tracks at most ONE indicator per OWNER (key = owner slug), recording
	// where it was posted so it can be cleared. Scoping per-owner (not per-thread)
	// bounds the indicator to a single post + delete per turn: an owner with many
	// still-executable tasks would otherwise post/delete in every one of their
	// threads on every activity event and blow Slack's rate limit (observed live).
	type indicator struct{ channelID, ts string }
	posted := map[string]indicator{}
	clear := func(slug string) {
		ind, up := posted[slug]
		if !up {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, _, err := t.api.DeleteMessageContext(cctx, ind.channelID, ind.ts)
		cancel()
		if err != nil {
			log.Printf("[slack] thinking indicator clear for %s: %v", slug, err)
			return
		}
		delete(posted, slug)
	}

	for {
		select {
		case <-ctx.Done():
			// Clear any lingering indicators on shutdown (best-effort, detached ctx).
			for _, ind := range posted {
				_, _, _ = t.api.DeleteMessageContext(context.Background(), ind.channelID, ind.ts)
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
			// Active. Show one indicator in the owner's most-recently-updated task
			// thread (refs[0]); if one is already up for this owner, do nothing.
			if _, already := posted[slug]; already {
				continue
			}
			refs := t.Broker.ActiveSlackTaskThreadsForOwner(slug)
			if len(refs) == 0 {
				continue
			}
			ref := refs[0]
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, ts, err := t.api.PostMessageContext(cctx, ref.ChannelID,
				slack.MsgOptionText(t.thinkingStatusText(slug), false),
				slack.MsgOptionTS(ref.ThreadTS),
			)
			cancel()
			if err != nil {
				log.Printf("[slack] thinking indicator post for %s: %v", ref.TaskID, err)
				continue
			}
			posted[slug] = indicator{channelID: ref.ChannelID, ts: ts}
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
	return fmt.Sprintf("💭 %s is thinking…", name)
}

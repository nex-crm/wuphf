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
// Slack, so a reader sees "Scout is thinking…" while a turn runs instead of dead
// air until the reply.
//
// Mechanism: Slack's NATIVE AI status (assistant.threads.setStatus), which costs
// ZERO channel messages — it renders in the thread composer footer and clears the
// instant the agent goes idle, so it never pings followers or eats the rate
// budget the way a posted-then-deleted status line would. The catch is WHERE it
// renders: native status shows inside the 1:1 Assistant pane (a DM), and — for an
// app with the Assistant feature — on the app's own threads. So we set it on two
// surfaces per active turn: the owner's most-recent task thread (where shared
// task work lives) AND, for the office lead, the open Assistant pane (where a
// user chatting 1:1 with the office expects to see it). Either may be absent; we
// set whichever exist and clear them together on idle.
//
// It subscribes to the broker activity stream (the same signal the web UI shows
// as "planning next step"). Best-effort: a status failure is logged, never fatal,
// so the rest of the bridge is unaffected.

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

	// shown tracks the status surfaces currently lit for each OWNER, so we set on
	// a turn's start and clear them all on its end. We light at most two per turn:
	// the owner's most-recently-updated task thread and (for the lead) the open
	// Assistant pane — never every task an owner still holds.
	type statusRef struct{ channelID, threadTS string }
	shown := map[string][]statusRef{}
	setStatus := func(channelID, threadTS, text string) error {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return t.api.SetAssistantThreadsStatusContext(cctx, slack.AssistantThreadsSetStatusParameters{
			ChannelID: channelID, ThreadTS: threadTS, Status: text,
		})
	}
	clear := func(slug string) {
		refs, up := shown[slug]
		if !up {
			return
		}
		for _, ref := range refs {
			if err := setStatus(ref.channelID, ref.threadTS, ""); err != nil {
				log.Printf("[slack] thinking status clear for %s: %v", slug, err)
			}
		}
		delete(shown, slug)
	}

	for {
		select {
		case <-ctx.Done():
			// Clear any lingering status on shutdown (best-effort, detached ctx).
			for _, refs := range shown {
				for _, ref := range refs {
					_ = t.api.SetAssistantThreadsStatusContext(context.Background(), slack.AssistantThreadsSetStatusParameters{
						ChannelID: ref.channelID, ThreadTS: ref.threadTS, Status: "",
					})
				}
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
			// Active. Light status once per turn; if already lit for this owner,
			// do nothing.
			if _, already := shown[slug]; already {
				continue
			}
			text := t.thinkingStatusText(slug)
			var lit []statusRef
			// The owner's most-recently-updated task thread (shared channel work).
			if refs := t.Broker.ActiveSlackTaskThreadsForOwner(slug); len(refs) > 0 {
				ref := refs[0]
				if err := setStatus(ref.ChannelID, ref.ThreadTS, text); err != nil {
					log.Printf("[slack] thinking status for %s: %v", ref.TaskID, err)
				} else {
					lit = append(lit, statusRef{channelID: ref.ChannelID, threadTS: ref.ThreadTS})
				}
			}
			// The open Assistant pane (1:1 DM) bound to this owner — where native
			// status renders for a user chatting with the office. Only the lead
			// has one; everyone else returns ok=false.
			if channelID, threadTS, ok := t.Broker.AssistantPaneRef(slug); ok {
				if err := setStatus(channelID, threadTS, text); err != nil {
					log.Printf("[slack] thinking status (pane) for %s: %v", slug, err)
				} else {
					lit = append(lit, statusRef{channelID: channelID, threadTS: threadTS})
				}
			}
			if len(lit) > 0 {
				shown[slug] = lit
			}
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

package team

// slack_task_cards.go keeps one pinned Block Kit lifecycle card per ongoing
// task in the task's Slack-surfaced channel, so real Slack readers always see
// what the office is working on right now. The sync loop polls the broker
// (same cadence philosophy as broker_outbound_dispatch.go):
//
//   task enters an active lifecycle state  → post card + pin it
//   lifecycle state changes                → chat.update the SAME card in place
//   task reaches a terminal state          → final update + unpin
//
// Card↔task bindings persist in broker state (broker_slack_cards.go) so a
// restart updates the existing card instead of posting a duplicate. Cards are
// pure viewports — no interactive elements; the deep link opens the task in
// the web app.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

// slackTaskCardSyncInterval is the polling cadence for the card sync loop.
// Lifecycle transitions are human-scale events; 15s keeps the channel feeling
// live without burning Slack rate budget.
const slackTaskCardSyncInterval = 15 * time.Second

// slackCardActiveStates are the lifecycle states that count as "being worked
// on" — entering one of these gets the task a pinned card.
var slackCardActiveStates = map[string]bool{
	string(LifecycleStateRunning):           true,
	string(LifecycleStateReview):            true,
	string(LifecycleStateDecision):          true,
	string(LifecycleStateChangesRequested):  true,
	string(LifecycleStateBlockedOnPRMerge):  true,
	string(LifecycleStateQueuedBehindOwner): true,
}

// slackCardTerminalStates end a card's life: one final update, then unpin.
var slackCardTerminalStates = map[string]bool{
	string(LifecycleStateApproved): true,
	string(LifecycleStateRejected): true,
	string(LifecycleStateArchived): true,
	"done":                         true,
	"completed":                    true,
	"canceled":                     true,
	"cancelled":                    true,
}

// slackCardStateEmoji gives each state a glanceable marker; unknown states
// fall back to a neutral dot.
var slackCardStateEmoji = map[string]string{
	string(LifecycleStateRunning):           "🔨",
	string(LifecycleStateReview):            "🔍",
	string(LifecycleStateDecision):          "🧭",
	string(LifecycleStateChangesRequested):  "✏️",
	string(LifecycleStateBlockedOnPRMerge):  "⛔",
	string(LifecycleStateQueuedBehindOwner): "⏳",
	string(LifecycleStateApproved):          "✅",
	"done":                                  "✅",
	"completed":                             "✅",
	string(LifecycleStateRejected):          "🚫",
	string(LifecycleStateArchived):          "🗄️",
	"canceled":                              "🚫",
	"cancelled":                             "🚫",
}

// slackTaskCardState resolves the state a card renders: the lifecycle state
// when set (source of truth for the control loop), else the legacy status.
func slackTaskCardState(task *teamTask) string {
	if s := strings.ToLower(strings.TrimSpace(string(task.LifecycleState))); s != "" && s != string(LifecycleStateUnknown) {
		return s
	}
	return strings.ToLower(strings.TrimSpace(task.status))
}

// runTaskCardSync drives the card loop until ctx is cancelled. Started by the
// launcher alongside Run and the outbound dispatcher.
func (t *SlackTransport) runTaskCardSync(ctx context.Context) error {
	ticker := time.NewTicker(slackTaskCardSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		t.syncTaskCardsOnce(ctx)
	}
}

// syncTaskCardsOnce reconciles every task against its card record. Each Slack
// call is best-effort: a failure is logged and retried on the next tick (the
// record is only advanced after the call succeeds).
func (t *SlackTransport) syncTaskCardsOnce(ctx context.Context) {
	if t.Broker == nil || t.api == nil {
		return
	}
	for _, task := range t.Broker.AllTasks() {
		task := task
		channelID := t.channelIDForSlug(normalizeChannelSlug(task.Channel))
		if channelID == "" {
			continue
		}
		state := slackTaskCardState(&task)
		rec, exists := t.Broker.SlackTaskCard(task.ID)
		switch {
		case !exists:
			if !slackCardActiveStates[state] {
				continue // never card work that isn't ongoing (incl. old done tasks)
			}
			t.ensureTaskThreadRoot(ctx, task.ID)
		case rec.State != state:
			t.updateTaskCard(ctx, rec, &task, state)
		}
	}
}

// taskCardLock returns the per-task mutex that serializes thread-root posting
// for one task across the Send path and the card-sync loop.
func (t *SlackTransport) taskCardLock(taskID string) *sync.Mutex {
	v, _ := t.cardLocks.LoadOrStore(taskID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ensureTaskThreadRoot returns the Slack ts of the task's single thread-root
// card, posting (and pinning) it if it does not exist yet. Idempotent and
// concurrency-safe: the per-task lock + a re-check under it guarantee exactly
// one root per task even when Send and the sync loop race. Returns "" when the
// task or its bridged channel can't be resolved (caller then posts unthreaded).
func (t *SlackTransport) ensureTaskThreadRoot(ctx context.Context, taskID string) string {
	if t.Broker == nil || t.api == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	mu := t.taskCardLock(taskID)
	mu.Lock()
	defer mu.Unlock()
	if rec, ok := t.Broker.SlackTaskCard(taskID); ok {
		return rec.Timestamp
	}
	task := t.Broker.TaskByID(taskID)
	if task == nil {
		return ""
	}
	channelID := t.channelIDForSlug(normalizeChannelSlug(task.Channel))
	if channelID == "" {
		return ""
	}
	state := slackTaskCardState(task)

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	blocks := buildSlackTaskCardBlocks(task, state, t.Broker.WebURL())
	_, ts, err := t.api.PostMessageContext(cctx, channelID,
		slack.MsgOptionText(slackTaskCardFallback(task, state), false),
		slack.MsgOptionBlocks(blocks...),
	)
	if err != nil {
		log.Printf("[slack] task card post failed for %s: %v", taskID, err)
		return ""
	}
	rec := slackTaskCardRecord{ChannelID: channelID, Timestamp: ts, State: state}
	// Pinning is cosmetic-but-wanted; a pin failure (e.g. missing pins:write
	// scope) must not block the card itself.
	if err := t.api.AddPinContext(cctx, channelID, slack.NewRefToMessage(channelID, ts)); err != nil {
		log.Printf("[slack] task card pin failed for %s: %v", taskID, err)
	} else {
		rec.Pinned = true
	}
	t.Broker.SetSlackTaskCard(taskID, rec)
	return ts
}

func (t *SlackTransport) updateTaskCard(ctx context.Context, rec slackTaskCardRecord, task *teamTask, state string) {
	mu := t.taskCardLock(task.ID)
	mu.Lock()
	defer mu.Unlock()
	// Re-read under the lock: ensureTaskThreadRoot may have just created the
	// record on another goroutine.
	if cur, ok := t.Broker.SlackTaskCard(task.ID); ok {
		rec = cur
		if rec.State == state {
			return
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	blocks := buildSlackTaskCardBlocks(task, state, t.Broker.WebURL())
	if _, _, _, err := t.api.UpdateMessageContext(ctx, rec.ChannelID, rec.Timestamp,
		slack.MsgOptionText(slackTaskCardFallback(task, state), false),
		slack.MsgOptionBlocks(blocks...),
	); err != nil {
		log.Printf("[slack] task card update failed for %s: %v", task.ID, err)
		return
	}
	if slackCardTerminalStates[state] && rec.Pinned {
		if err := t.api.RemovePinContext(ctx, rec.ChannelID, slack.NewRefToMessage(rec.ChannelID, rec.Timestamp)); err != nil {
			// Leave rec.State UNADVANCED so the next sync tick re-enters this
			// path (state still differs) and retries the unpin. Persisting the
			// terminal state here would strand the pin forever — the steady
			// state is silent, so there'd be no later trigger to remove it.
			log.Printf("[slack] task card unpin failed for %s (will retry): %v", task.ID, err)
			return
		}
		rec.Pinned = false
	}
	rec.State = state
	t.Broker.SetSlackTaskCard(task.ID, rec)
}

// buildSlackTaskCardBlocks renders the lifecycle card. All dynamic fields are
// escaped; the deep link is built only from the broker's own WebURL.
func buildSlackTaskCardBlocks(task *teamTask, state, webURL string) []slack.Block {
	owner := strings.TrimSpace(task.Owner)
	if owner == "" {
		owner = "unassigned"
	}
	emoji := slackCardStateEmoji[state]
	if emoji == "" {
		emoji = "•"
	}
	body := fmt.Sprintf("%s *%s — %s*\n`%s` · owner _%s_",
		emoji, slackEscape(task.ID), slackEscape(task.Title),
		slackEscape(state), slackEscape(owner))
	if def := slackTaskDefinitionLines(task); def != "" {
		body += "\n" + def
	}
	if webURL != "" {
		body += fmt.Sprintf("\n<%s/tasks/%s|Open task in WUPHF →>", webURL, task.ID)
	}
	body += "\n_This task's work lives in this thread — reply here to take part._"
	return []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, body, false, false), nil, nil),
	}
}

// slackTaskDefinitionLines renders the task's definition into the thread-root
// card so the root states what the task IS: the structured Definition (goal +
// deliverables + success criteria) when one has been set, else the free-form
// Details. Clipped to stay glanceable.
func slackTaskDefinitionLines(task *teamTask) string {
	if def := task.Definition; def != nil {
		var parts []string
		if g := strings.TrimSpace(def.Goal); g != "" {
			parts = append(parts, "*Goal:* "+slackEscape(truncate(g, 280)))
		}
		if len(def.Deliverables) > 0 {
			var ds []string
			for _, d := range def.Deliverables {
				name := strings.TrimSpace(d.Name)
				if name == "" {
					continue
				}
				if f := strings.TrimSpace(d.Format); f != "" {
					ds = append(ds, fmt.Sprintf("%s (%s)", slackEscape(name), slackEscape(f)))
				} else {
					ds = append(ds, slackEscape(name))
				}
			}
			if len(ds) > 0 {
				parts = append(parts, "*Deliverables:* "+strings.Join(ds, ", "))
			}
		}
		if len(def.SuccessCriteria) > 0 {
			var cs []string
			for _, c := range def.SuccessCriteria {
				if c = strings.TrimSpace(c); c != "" {
					cs = append(cs, slackEscape(c))
				}
			}
			if len(cs) > 0 {
				parts = append(parts, "*Success:* "+strings.Join(cs, "; "))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if d := strings.TrimSpace(task.Details); d != "" {
		return slackEscape(truncate(d, 400))
	}
	return ""
}

// slackTaskCardFallback is the plain-text notification fallback for the card.
func slackTaskCardFallback(task *teamTask, state string) string {
	return fmt.Sprintf("Task %s — %s [%s]", task.ID, task.Title, state)
}

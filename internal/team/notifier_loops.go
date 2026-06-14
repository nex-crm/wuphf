package team

// notifier_loops.go owns the per-launcher notification poll loops
// (PLAN.md §C11) plus the panic-recovery wrapper they share.
// notifyAgentsLoop watches broker.Messages, notifyTaskActionsLoop
// watches the action ledger, notifyOfficeChangesLoop fans out roster
// changes to deliverOfficeChangeNotification. Split out of launcher.go
// so the notification orchestration sits in one file rather than
// scattered through the launcher's lifecycle code.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// notifyAgentsLoop subscribes to broker messages and pushes notifications immediately.
func (l *Launcher) notifyAgentsLoop() {
	if l.broker == nil {
		return
	}
	msgs, unsubscribe := l.broker.SubscribeMessages(128)
	defer unsubscribe()

	// Note: there is intentionally NO office-wide pending-interview gate
	// here. The old HasPendingInterview() skip silently dropped EVERY
	// message wake while any one agent waited on a human interview —
	// ICP-eval v3 [19:23:59]: one buried interview wedged the whole
	// office (a librarian dead to a direct @-mention for 7+ minutes).
	// The gate is now scoped per-target in sendChannelUpdate /
	// sendTaskUpdate: only the ASKING agent's new turns are suppressed
	// while its own interview is pending.
	for msg := range msgs {
		if msg.From == "system" {
			continue
		}
		l.safeDeliverMessage(msg)
	}
}

// safeDeliverMessage wraps deliverMessageNotification in a panic recover so a
// bad message doesn't take the whole broker down. Stack is written to stderr
// and logs/panics.log so we can diagnose the next occurrence.
//
// The panic-context line includes IDs and channel only — not the
// full message body. Bodies are attacker-controlled (CRM emails,
// calendar entries, agent output) and the panic log lands in
// ~/.wuphf/logs/panics.log which the user often shares verbatim
// when filing a bug. Drop the payload to avoid leaking secrets or
// personal data.
func (l *Launcher) safeDeliverMessage(msg channelMessage) {
	defer recoverPanicTo("deliverMessageNotification", messagePanicContext(msg))
	l.deliverMessageNotification(msg)
}

// messagePanicContext returns a redacted summary of msg suitable for
// inclusion in panic logs.
func messagePanicContext(msg channelMessage) string {
	return fmt.Sprintf("msg.id=%s msg.channel=%s msg.from=%s msg.kind=%s msg.tagged=%v",
		msg.ID, msg.Channel, msg.From, msg.Kind, msg.Tagged)
}

// recoverPanicTo is the shared panic-recovery body used by broker background
// goroutines. It logs the goroutine stack to stderr and to
// ~/.wuphf/logs/panics.log so the broker stays up even if a specific action
// path blows up. Call as: defer recoverPanicTo("loopName", "extra context").
func recoverPanicTo(site, extra string) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, 16<<10)
	n := runtime.Stack(buf, false)
	fmt.Fprintf(os.Stderr, "panic in %s: %v\n%s\n%s\n", site, r, extra, buf[:n])
	if home := config.RuntimeHomeDir(); home != "" {
		// MkdirAll first — on a fresh install (or after
		// `rm -rf ~/.wuphf`) the logs directory does not yet
		// exist, OpenFile alone would fail with ENOENT, and the
		// first-ever panic stack would be silently dropped exactly
		// when we most need it. 0o700 on the dir mirrors the 0o600
		// owner-only intent of the file itself.
		dir := filepath.Join(home, ".wuphf", "logs")
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			fmt.Fprintf(os.Stderr, "panic-log: cannot ensure %s: %v\n", dir, mkErr)
			return
		}
		// 0o600 (owner-only) — even though message bodies are now
		// redacted, panics.log still leaks routing metadata
		// (channel slugs, agent slugs) that's sensitive on shared
		// systems where wuphf runs under a service account whose
		// home is world-readable.
		if f, ferr := os.OpenFile(filepath.Join(dir, "panics.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); ferr == nil {
			_, _ = fmt.Fprintf(f, "%s panic in %s: %v\n%s\n%s\n\n", time.Now().UTC().Format(time.RFC3339), site, r, extra, buf[:n])
			_ = f.Close()
		}
	}
}

func (l *Launcher) notifyTaskActionsLoop() {
	if l.broker == nil {
		return
	}
	actions, unsubscribe := l.broker.SubscribeActions(128)
	defer unsubscribe()

	// Same scoping as notifyAgentsLoop: no office-wide pending-interview
	// drop — the per-target gate lives in sendTaskUpdate.
	for action := range actions {
		if action.Kind != "task_created" && action.Kind != "task_updated" &&
			action.Kind != "task_unblocked" && action.Kind != taskFollowUpActionKind {
			continue
		}
		task, ok := l.taskForAction(action)
		if !ok {
			continue
		}
		// Skip "done" tasks for task_created / task_updated — the agent that completed
		// the task should send a follow-up broadcast which wakes CEO via the message
		// loop. But for task_unblocked the task status is still "in_progress" (it was
		// just unblocked), so we must never skip it regardless of status. task_followup
		// exists PRECISELY for done tasks (a human posted after delivery), so it also
		// passes the done-skip.
		if action.Kind != "task_unblocked" && action.Kind != taskFollowUpActionKind &&
			strings.EqualFold(strings.TrimSpace(task.status), "done") {
			continue
		}
		func() {
			defer recoverPanicTo("deliverTaskNotification",
				fmt.Sprintf("action.kind=%s action.actor=%s action.channel=%s task.id=%s task.status=%s",
					action.Kind, action.Actor, action.Channel, task.ID, task.status))
			l.deliverTaskNotification(action, task)
		}()
	}
}

func (l *Launcher) notifyOfficeChangesLoop() {
	if l.broker == nil {
		return
	}
	changes, unsubscribe := l.broker.SubscribeOfficeChanges(128)
	defer unsubscribe()

	for evt := range changes {
		// Wrap each iteration in the same panic guard the other
		// subscriber loops use. Pre-fix a panic in respawnPanesAfterReseed
		// or deliverOfficeChangeNotification would kill this goroutine
		// silently and leave the launcher unable to react to subsequent
		// roster changes.
		func(evt officeChangeEvent) {
			defer recoverPanicTo("deliverOfficeChangeNotification",
				fmt.Sprintf("evt.kind=%s evt.slug=%s", evt.Kind, evt.Slug))
			// office_reseeded fires after onboarding rewrites the whole roster
			// (blueprint selection). The interactive claude panes were spawned
			// from the earlier default team and now point at slugs that are no
			// longer registered agents — messages typed into them go into a
			// dead shell. Respawn them against the new roster, outside the
			// interview guard so it can't be blocked by a half-complete wizard.
			if evt.Kind == "office_reseeded" {
				l.respawnPanesAfterReseed()
				return
			}
			// member_updated covers per-agent provider switches initiated
			// from the AgentProfilePanel runtime picker. The launcher needs
			// to drop any pane that was holding the old runtime so the
			// next dispatch routes through the headless path the new kind
			// expects. This is best-effort: the worker that picks up the
			// next turn already reads MemberEffectiveProviderKind, so a
			// failed pane kill at most leaves a stale claude window
			// running silently — it won't intercept future dispatches.
			if evt.Kind == "member_updated" {
				l.reconcileMemberRuntime(evt.Slug)
			}
			// No office-wide pending-interview drop (see notifyAgentsLoop);
			// the per-target gate lives in sendTaskUpdate.
			l.deliverOfficeChangeNotification(evt)
		}(evt)
	}
}

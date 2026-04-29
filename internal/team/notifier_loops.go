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
)

// notifyAgentsLoop subscribes to broker messages and pushes notifications immediately.
func (l *Launcher) notifyAgentsLoop() {
	if l.broker == nil {
		return
	}
	msgs, unsubscribe := l.broker.SubscribeMessages(128)
	defer unsubscribe()

	for msg := range msgs {
		if l.broker.HasPendingInterview() {
			continue
		}
		if msg.From == "system" {
			continue
		}
		l.safeDeliverMessage(msg)
	}
}

// safeDeliverMessage wraps deliverMessageNotification in a panic recover so a
// bad message doesn't take the whole broker down. Stack is written to stderr
// and logs/panics.log so we can diagnose the next occurrence.
func (l *Launcher) safeDeliverMessage(msg channelMessage) {
	defer recoverPanicTo("deliverMessageNotification", fmt.Sprintf("msg=%+v", msg))
	l.deliverMessageNotification(msg)
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
	if home, err := os.UserHomeDir(); err == nil {
		if f, ferr := os.OpenFile(filepath.Join(home, ".wuphf", "logs", "panics.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
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

	for action := range actions {
		if l.broker.HasPendingInterview() {
			continue
		}
		if action.Kind != "task_created" && action.Kind != "task_updated" && action.Kind != "task_unblocked" {
			continue
		}
		task, ok := l.taskForAction(action)
		if !ok {
			continue
		}
		// Skip "done" tasks for task_created / task_updated — the agent that completed
		// the task should send a follow-up broadcast which wakes CEO via the message
		// loop. But for task_unblocked the task status is still "in_progress" (it was
		// just unblocked), so we must never skip it regardless of status.
		if action.Kind != "task_unblocked" && strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		func() {
			defer recoverPanicTo("deliverTaskNotification", fmt.Sprintf("action=%+v task=%+v", action, task))
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
		// office_reseeded fires after onboarding rewrites the whole roster
		// (blueprint selection). The interactive claude panes were spawned
		// from the earlier default team and now point at slugs that are no
		// longer registered agents — messages typed into them go into a
		// dead shell. Respawn them against the new roster, outside the
		// interview guard so it can't be blocked by a half-complete wizard.
		if evt.Kind == "office_reseeded" {
			l.respawnPanesAfterReseed()
			continue
		}
		if l.broker.HasPendingInterview() {
			continue
		}
		l.deliverOfficeChangeNotification(evt)
	}
}

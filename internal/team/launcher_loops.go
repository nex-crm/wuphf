package team

// launcher_loops.go owns the long-running poll loops that the
// Launcher kicks off in goroutines: watchChannelPaneLoop watches the
// channel TUI pane and respawns it on death; primeVisibleAgents
// clears claude's startup interactivity (delegate to paneLifecycle
// + replay-catchup); pollOneRelayEventsLoop +
// fetchAndRecordOneRelayEvents polls the OneRelay backlog and
// records signals/decisions/actions against the broker. Plus the
// small channel-pane helpers (channelPaneStatus + captureDeadChannelPane).
// Split out of launcher.go so the goroutine entry points sit in one
// file rather than scattered through the orchestrator.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

func (l *Launcher) watchChannelPaneLoop(channelCmd string) {
	unhealthyCount := 0
	var deadSince time.Time
	snapshotWritten := false
	for {
		time.Sleep(2 * time.Second)

		status, err := l.panes().ChannelPaneStatus()
		if err != nil {
			if isNoSessionError(err.Error()) {
				return
			}
			continue
		}
		if !channelPaneNeedsRespawn(status) {
			unhealthyCount = 0
			deadSince = time.Time{}
			snapshotWritten = false
			continue
		}
		unhealthyCount++
		if unhealthyCount < 2 {
			continue
		}
		if deadSince.IsZero() {
			deadSince = time.Now()
		}
		if !snapshotWritten {
			_ = l.panes().CaptureDeadChannelPane(status)
			snapshotWritten = true
		}
		if time.Since(deadSince) < channelRespawnDelay {
			continue
		}
		unhealthyCount = 0
		deadSince = time.Time{}
		snapshotWritten = false
		l.panes().RespawnChannelPane(channelCmd, l.cwd)
	}
}

func (l *Launcher) channelPaneStatus() (string, error) {
	return l.panes().ChannelPaneStatus()
}

// captureDeadChannelPane delegates to paneLifecycle (PLAN.md §C5c).
func (l *Launcher) captureDeadChannelPane(status string) error {
	return l.panes().CaptureDeadChannelPane(status)
}

// primeVisibleAgents clears Claude startup interactivity in newly spawned panes and
// replays a catch-up channel nudge once they are actually ready to read it.
// primeVisibleAgents delegates the pane-priming loop to paneLifecycle
// (PLAN.md §C5e), then runs the launcher-side replay-catchup so the
// first message that arrived during claude's startup window isn't lost
// behind the trust prompt.
func (l *Launcher) primeVisibleAgents() {
	l.panes().PrimeVisibleAgents()
	if l.broker == nil {
		return
	}
	msgs := l.broker.Messages()
	if len(msgs) > 0 {
		latest := msgs[len(msgs)-1]
		l.deliverMessageNotification(latest)
	}
	l.resumeInFlightWork()
}

func (l *Launcher) pollOneRelayEventsLoop() {
	if l.broker == nil {
		return
	}
	provider := action.NewOneCLIFromEnv()
	if _, err := provider.ListRelays(context.Background(), action.ListRelaysOptions{Limit: 1}); err != nil {
		return
	}
	const defaultInterval = time.Minute
	time.Sleep(25 * time.Second)
	for {
		// one-relay-events is read-only in v1 (interval_override is not
		// user-settable), but Enabled is honored so a future admin path
		// stays consistent with other system crons.
		enabled, _ := l.broker.SchedulerJobControl("one-relay-events", defaultInterval)
		if !enabled {
			l.updateSchedulerJob("one-relay-events", "One relay events", defaultInterval, time.Now().UTC().Add(defaultInterval), "disabled")
			time.Sleep(defaultInterval)
			continue
		}
		l.updateSchedulerJob("one-relay-events", "One relay events", defaultInterval, time.Now().UTC(), "running")
		l.fetchAndRecordOneRelayEvents(provider)
		l.updateSchedulerJob("one-relay-events", "One relay events", defaultInterval, time.Now().UTC().Add(defaultInterval), "sleeping")
		time.Sleep(defaultInterval)
	}
}

func (l *Launcher) fetchAndRecordOneRelayEvents(provider *action.OneCLI) {
	if l.broker == nil || provider == nil {
		return
	}
	result, err := provider.ListRelayEvents(context.Background(), action.RelayEventsOptions{Limit: 20})
	if err != nil {
		return
	}
	if len(result.Events) == 0 {
		return
	}
	var signals []officeSignal
	for _, event := range result.Events {
		title := strings.TrimSpace(event.EventType)
		if title == "" {
			title = "Relay event"
		}
		content := fmt.Sprintf("One relay received %s on %s.", strings.TrimSpace(event.EventType), strings.TrimSpace(event.Platform))
		signals = append(signals, officeSignal{
			ID:         strings.TrimSpace(event.ID),
			Source:     "one",
			Kind:       "relay_event",
			Title:      title,
			Content:    content,
			Channel:    "general",
			Owner:      "ceo",
			Confidence: "medium",
			Urgency:    "medium",
		})
	}
	records, err := l.broker.RecordSignals(signals)
	if err != nil || len(records) == 0 {
		return
	}
	for _, record := range records {
		_ = l.broker.RecordAction(
			"external_trigger_received",
			"one",
			record.Channel,
			"one",
			truncateSummary(record.Title+" "+record.Content, 140),
			record.ID,
			[]string{record.ID},
			"",
		)
	}
}

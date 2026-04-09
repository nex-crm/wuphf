package team

import (
	"fmt"
	"strings"
	"time"
)

type headlessProgressMetrics struct {
	TotalMs      int64
	FirstEventMs int64
	FirstTextMs  int64
	FirstToolMs  int64
}

func (l *Launcher) updateHeadlessProgress(slug string, status string, activity string, detail string, metrics headlessProgressMetrics) {
	if l == nil || l.broker == nil {
		return
	}
	l.broker.UpdateAgentActivity(agentActivitySnapshot{
		Slug:         slug,
		Status:       strings.TrimSpace(status),
		Activity:     strings.TrimSpace(activity),
		Detail:       strings.TrimSpace(detail),
		LastTime:     time.Now().UTC().Format(time.RFC3339),
		TotalMs:      metrics.TotalMs,
		FirstEventMs: metrics.FirstEventMs,
		FirstTextMs:  metrics.FirstTextMs,
		FirstToolMs:  metrics.FirstToolMs,
	})
}

func formatHeadlessLatencySummary(metrics headlessProgressMetrics) string {
	var parts []string
	if metrics.FirstTextMs >= 0 {
		parts = append(parts, fmt.Sprintf("ttft %dms", metrics.FirstTextMs))
	} else if metrics.FirstEventMs >= 0 {
		parts = append(parts, fmt.Sprintf("first event %dms", metrics.FirstEventMs))
	}
	if metrics.FirstToolMs >= 0 {
		parts = append(parts, fmt.Sprintf("first tool %dms", metrics.FirstToolMs))
	}
	if metrics.TotalMs >= 0 {
		parts = append(parts, fmt.Sprintf("done %dms", metrics.TotalMs))
	}
	return strings.Join(parts, " · ")
}

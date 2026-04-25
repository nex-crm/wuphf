package team

// broker_lint_cron.go manages the daily lint run scheduled at a configured
// local time. The goroutine is started from ensureWikiWorker after the wiki
// worker and index are up. It cancels cleanly when the broker's context
// is done.
//
// Lint runs cost real money (contradiction judging shells out to the user's
// LLM CLI), so the cron is opt-in: absent env var = disabled.
//
// Environment variables:
//   WUPHF_LINT_CRON   — "HH:MM" (24-hour, local time) to enable a daily run
//                        at that time. Unset or empty = cron disabled.

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// startLintCron launches a goroutine that sleeps until the next HH:MM in the
// local timezone and then runs the Lint suite once per day. The goroutine
// stops when ctx is cancelled (broker shutdown).
//
// The cron is opt-in. Set WUPHF_LINT_CRON=HH:MM to enable a daily run.
// Unset / empty / "disabled" all mean no cron — no LLM calls happen unless
// a human explicitly clicks "Check wiki health" in the UI.
func (b *Broker) startLintCron(ctx context.Context, idx *WikiIndex, worker *WikiWorker) {
	schedule := strings.TrimSpace(os.Getenv("WUPHF_LINT_CRON"))
	if schedule == "" || schedule == "disabled" {
		log.Printf("wiki lint cron: disabled (set WUPHF_LINT_CRON=HH:MM to enable a daily run)")
		return
	}

	hour, minute, err := parseLintCronTime(schedule)
	if err != nil {
		log.Printf("wiki lint cron: invalid WUPHF_LINT_CRON %q: %v — cron disabled", schedule, err)
		return
	}

	go func() {
		for {
			next := nextOccurrence(time.Now(), hour, minute)
			log.Printf("wiki lint cron: next run at %s", next.Format(time.RFC3339))

			select {
			case <-ctx.Done():
				log.Printf("wiki lint cron: stopping (context cancelled)")
				return
			case <-time.After(time.Until(next)):
			}

			// Re-check context after waking (broker may have shut down
			// while we were sleeping).
			select {
			case <-ctx.Done():
				return
			default:
			}

			b.runLintCronOnce(ctx, idx, worker)
		}
	}()
}

// runLintCronOnce executes a single lint run and logs the outcome.
// Never panics — errors are logged and the cron continues.
func (b *Broker) runLintCronOnce(ctx context.Context, idx *WikiIndex, worker *WikiWorker) {
	log.Printf("wiki lint cron: running daily lint")
	prov := &brokerLintProvider{}
	l := NewLint(idx, worker, prov)

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	report, err := l.Run(runCtx)
	if err != nil {
		log.Printf("wiki lint cron: run error: %v", err)
		return
	}

	critical := 0
	warnings := 0
	for _, f := range report.Findings {
		switch f.Severity {
		case "critical":
			critical++
		case "warning":
			warnings++
		}
	}
	log.Printf("wiki lint cron: complete — %d critical, %d warnings, %d findings total (report: wiki/.lint/report-%s.md)",
		critical, warnings, len(report.Findings), report.Date)
}

// parseLintCronTime parses a "HH:MM" string into (hour, minute). Returns an
// error when the format or range is invalid.
func parseLintCronTime(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour %q", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute %q", parts[1])
	}
	return h, m, nil
}

// nextOccurrence returns the next wall-clock time when the local clock shows
// HH:MM. If that time is in the past today it returns tomorrow's occurrence.
func nextOccurrence(now time.Time, hour, minute int) time.Time {
	// Candidate: today at HH:MM.
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if candidate.After(now) {
		return candidate
	}
	// Tomorrow at HH:MM.
	return candidate.Add(24 * time.Hour)
}

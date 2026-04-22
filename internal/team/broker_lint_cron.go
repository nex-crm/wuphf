package team

// broker_lint_cron.go manages the daily lint run scheduled at a configured
// local time (default 09:00). The goroutine is started from ensureWikiWorker
// after the wiki worker and index are up. It cancels cleanly when the broker's
// context is done.
//
// Environment variables:
//   WUPHF_LINT_CRON   — "HH:MM" (24-hour, local time). Default "09:00".
//                        Set to empty string to disable the cron entirely.

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultLintCronTime is the HH:MM at which the lint cron fires if no env
// override is set.
const defaultLintCronTime = "09:00"

// startLintCron launches a goroutine that sleeps until the next HH:MM in the
// local timezone and then runs the Lint suite once per day. The goroutine
// stops when ctx is cancelled (broker shutdown).
//
// When WUPHF_LINT_CRON is empty the goroutine exits immediately without
// running lint — this is the intended path for dev and tests.
func (b *Broker) startLintCron(ctx context.Context, idx *WikiIndex, worker *WikiWorker) {
	schedule := os.Getenv("WUPHF_LINT_CRON")
	if schedule == "" {
		schedule = defaultLintCronTime // use default if unset
	}
	// Sentinel: caller explicitly disabled cron.
	if strings.TrimSpace(schedule) == "disabled" {
		log.Printf("wiki lint cron: disabled via WUPHF_LINT_CRON=disabled")
		return
	}

	hour, minute, err := parseLintCronTime(schedule)
	if err != nil {
		log.Printf("wiki lint cron: invalid WUPHF_LINT_CRON %q: %v — using default %s", schedule, err, defaultLintCronTime)
		hour, minute = 9, 0
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

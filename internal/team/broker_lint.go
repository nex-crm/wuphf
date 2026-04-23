package team

// broker_lint.go wires the Lint runner into the broker HTTP layer.
//
// Routes:
//   POST /wiki/lint/run    — runs all 5 lint checks, returns LintReport JSON
//   POST /wiki/lint/resolve — resolves one contradiction finding
//
// Both routes gate on the markdown backend (worker must be running). Non-markdown
// backends return 503 with a short explanation.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// brokerLintProvider wraps provider.RunConfiguredOneShot as a QueryProvider.
type brokerLintProvider struct{}

func (p *brokerLintProvider) Query(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	return provider.RunConfiguredOneShot(systemPrompt, userPrompt, "")
}

// handleLintRun answers POST /wiki/lint/run.
// On success: returns the LintReport JSON (200).
// When the wiki worker is unavailable: 503.
func (b *Broker) handleLintRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, "wiki backend unavailable", http.StatusServiceUnavailable)
		return
	}

	idx := b.WikiIndex()
	prov := &brokerLintProvider{}
	l := NewLint(idx, worker, prov)

	report, err := l.Run(r.Context())
	if err != nil {
		log.Printf("wiki lint: run error: %v", err)
		http.Error(w, fmt.Sprintf("lint run failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

// lintResolveRequest is the JSON body for POST /wiki/lint/resolve.
//
// The client echoes back the full LintFinding it received from /wiki/lint/run
// so the broker does not need to persist structured findings separately from
// the markdown report. ReportDate is kept for audit-log correlation only.
type lintResolveRequest struct {
	// ReportDate is the YYYY-MM-DD of the lint report that contains the finding.
	ReportDate string `json:"report_date"`
	// FindingIdx is the 0-based index into report.findings (audit use only).
	FindingIdx int `json:"finding_idx"`
	// Finding is the full LintFinding to resolve. Required.
	Finding *LintFinding `json:"finding"`
	// Winner is "A", "B", or "Both".
	Winner string `json:"winner"`
}

// lintResolveResponse is the JSON body on success.
type lintResolveResponse struct {
	CommitSHA string `json:"commit_sha"`
	Message   string `json:"message"`
}

// handleLintResolve answers POST /wiki/lint/resolve.
// Body: lintResolveRequest JSON.
// On success: lintResolveResponse JSON (200).
func (b *Broker) handleLintResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, "wiki backend unavailable", http.StatusServiceUnavailable)
		return
	}

	var req lintResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if req.ReportDate == "" {
		http.Error(w, "report_date is required", http.StatusBadRequest)
		return
	}
	if req.Winner != "A" && req.Winner != "B" && req.Winner != "Both" {
		http.Error(w, "winner must be A, B, or Both", http.StatusBadRequest)
		return
	}
	if req.Finding == nil {
		http.Error(w, "finding is required", http.StatusBadRequest)
		return
	}
	if req.Finding.Type != "contradictions" {
		http.Error(w, "finding type must be 'contradictions'", http.StatusBadRequest)
		return
	}
	if len(req.Finding.FactIDs) < 2 {
		http.Error(w, "finding must include at least 2 fact_ids", http.StatusBadRequest)
		return
	}

	// Wrap the client-supplied finding in a one-element report so
	// ResolveContradiction's index-based API is preserved.
	report := LintReport{
		Date:     req.ReportDate,
		Findings: []LintFinding{*req.Finding},
	}

	idx := b.WikiIndex()
	prov := &brokerLintProvider{}
	l := NewLint(idx, worker, prov)

	// Resolve under the caller's identity. If no identity in the registry
	// use the synthetic human fallback.
	identity := b.resolveCallerIdentity(r)

	if err := l.ResolveContradiction(r.Context(), report, 0, req.Winner, identity); err != nil {
		log.Printf("wiki lint: resolve error: %v", err)
		http.Error(w, fmt.Sprintf("resolve failed: %v", err), http.StatusInternalServerError)
		return
	}
	worker.WaitForIdle()

	sha, shaErr := worker.Repo().HeadSHA(r.Context())
	if shaErr != nil {
		log.Printf("wiki lint: resolve head-sha lookup failed: %v", shaErr)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lintResolveResponse{
		CommitSHA: sha,
		Message:   fmt.Sprintf("Contradiction %d resolved as winner=%s", req.FindingIdx, req.Winner),
	})
}

// resolveCallerIdentity extracts the HumanIdentity from the request context.
// Falls back to the synthetic human identity when none is registered.
func (b *Broker) resolveCallerIdentity(r *http.Request) HumanIdentity {
	// Use the process-wide human identity registry.
	return brokerHumanIdentityRegistry().Local()
}

// handleLintRunChat is the broker handler that posts a lint summary chat
// message when /lint is issued from the web composer. It runs lint and posts
// the summary to the channel via broker.PostMessage.
func (b *Broker) handleLintRunChat(channel string) {
	worker := b.WikiWorker()
	if worker == nil {
		return
	}

	idx := b.WikiIndex()
	prov := &brokerLintProvider{}
	l := NewLint(idx, worker, prov)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := l.Run(ctx)
	if err != nil {
		log.Printf("wiki lint chat: run error: %v", err)
		if _, postErr := b.PostMessage("archivist", channel, fmt.Sprintf("Lint run failed: %v", err), nil, ""); postErr != nil {
			log.Printf("wiki lint chat: post error: %v", postErr)
		}
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

	msg := fmt.Sprintf("Lint found %d critical, %d warnings. View full report at /wiki/.lint/report-%s",
		critical, warnings, report.Date)
	if _, err := b.PostMessage("archivist", channel, msg, nil, ""); err != nil {
		log.Printf("wiki lint chat: post summary error: %v", err)
	}
}

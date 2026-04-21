package team

// broker_playbook.go wires the v1.3 playbook compiler surface onto the
// broker — execution log, HTTP handlers, SSE fanout.
//
// Route map (registered in broker.go):
//
//	GET  /playbook/list              — enumerate compiled playbooks + runnable-by agents
//	POST /playbook/compile           — manually recompile a specific playbook
//	POST /playbook/execution         — append one execution entry
//	GET  /playbook/executions?slug=  — list executions for a slug, newest-first
//
// SSE event fanned out via /events:
//
//	playbook:execution_recorded      — { slug, path, commit_sha, recorded_by, timestamp }

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// playbookSubscribersMu guards lazy init of the subscriber map. We do NOT
// add another field to the Broker struct — instead we stash the map in a
// package-level registry keyed by broker pointer. Simplest zero-cost way to
// extend without touching broker.go's long constructor.
//
// Rationale: broker.go is already on the long side and has a carefully
// curated field order; the playbook surface does not need to participate
// in any existing method's locking story except fanout, so a side map
// keeps the diff narrow.
var (
	playbookSubscribersMu        sync.Mutex
	playbookSubscribersByBroker  = map[*Broker]map[int]chan PlaybookExecutionRecordedEvent{}
	playbookSynthSubsByBroker    = map[*Broker]map[int]chan PlaybookSynthesizedEvent{}
	playbookExecutionLogByBroker = map[*Broker]*ExecutionLog{}
)

// SubscribePlaybookExecutionEvents returns a channel of execution-recorded
// events plus an unsubscribe func. Mirrors SubscribeEntityFactEvents.
func (b *Broker) SubscribePlaybookExecutionEvents(buffer int) (<-chan PlaybookExecutionRecordedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	subs := playbookSubscribersByBroker[b]
	if subs == nil {
		subs = make(map[int]chan PlaybookExecutionRecordedEvent)
		playbookSubscribersByBroker[b] = subs
	}
	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.mu.Unlock()
	ch := make(chan PlaybookExecutionRecordedEvent, buffer)
	subs[id] = ch
	return ch, func() {
		playbookSubscribersMu.Lock()
		defer playbookSubscribersMu.Unlock()
		if subs := playbookSubscribersByBroker[b]; subs != nil {
			if existing, ok := subs[id]; ok {
				delete(subs, id)
				close(existing)
			}
		}
	}
}

// PublishPlaybookExecutionRecorded fans out the SSE event. Implements the
// playbookEventPublisher interface consumed by WikiWorker.
func (b *Broker) PublishPlaybookExecutionRecorded(evt PlaybookExecutionRecordedEvent) {
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	for _, ch := range playbookSubscribersByBroker[b] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// PlaybookExecutionLog returns the active ExecutionLog, or nil before
// ensurePlaybookExecutionLog has run. Exposed so handler code can share
// the one instance the worker initialized.
func (b *Broker) PlaybookExecutionLog() *ExecutionLog {
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	return playbookExecutionLogByBroker[b]
}

// SetPlaybookExecutionLog wires a log from tests.
func (b *Broker) SetPlaybookExecutionLog(log *ExecutionLog) {
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	playbookExecutionLogByBroker[b] = log
}

// ensurePlaybookExecutionLog constructs the ExecutionLog once the wiki
// worker is online. Idempotent.
func (b *Broker) ensurePlaybookExecutionLog() {
	b.mu.Lock()
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	playbookSubscribersMu.Lock()
	if _, ok := playbookExecutionLogByBroker[b]; !ok {
		playbookExecutionLogByBroker[b] = NewExecutionLog(worker)
	}
	playbookSubscribersMu.Unlock()
}

// PlaybookSummary is one row returned by GET /playbook/list.
type PlaybookSummary struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	SourcePath       string   `json:"source_path"`
	SkillPath        string   `json:"skill_path"`
	SkillExists      bool     `json:"skill_exists"`
	ExecutionLogPath string   `json:"execution_log_path"`
	ExecutionCount   int      `json:"execution_count"`
	RunnableByAgents []string `json:"runnable_by_agents"`
}

// handlePlaybookList is GET /playbook/list.
//
//	resp: { "playbooks": [ PlaybookSummary, ... ] }
func (b *Broker) handlePlaybookList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	root := worker.Repo().Root()
	dir := filepath.Join(root, "team", "playbooks")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]PlaybookSummary, 0, len(entries))
	execLog := b.PlaybookExecutionLog()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		if !slugPattern.MatchString(slug) {
			continue
		}
		sourcePath := playbookSourceRel(slug)
		skillPath := CompiledSkillRelPath(slug)
		skillFull := filepath.Join(root, filepath.FromSlash(skillPath))
		skillExists := false
		if _, err := os.Stat(skillFull); err == nil {
			skillExists = true
		}
		title := slug
		if body, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			if h := headerLineFrom(stripFrontmatter(string(body))); h != "" {
				title = h
			}
		}
		execCount := 0
		if execLog != nil {
			if entries, err := execLog.List(slug); err == nil {
				execCount = len(entries)
			}
		}
		rows = append(rows, PlaybookSummary{
			Slug:             slug,
			Title:            title,
			SourcePath:       sourcePath,
			SkillPath:        skillPath,
			SkillExists:      skillExists,
			ExecutionLogPath: ExecutionLogRelPath(slug),
			ExecutionCount:   execCount,
			// v1.3 scope: every agent can invoke every compiled playbook.
			// Per-agent gating is called out as out-of-scope in the task brief.
			RunnableByAgents: []string{"*"},
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	writeJSON(w, http.StatusOK, map[string]any{"playbooks": rows})
}

// handlePlaybookCompile is POST /playbook/compile.
//
//	body: { "slug": "churn-prevention" }
//	resp: { "slug", "skill_path", "commit_sha" }
func (b *Broker) handlePlaybookCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	var body struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	slug := strings.TrimSpace(body.Slug)
	if !slugPattern.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)})
		return
	}
	// Require the source to exist — compiling a phantom playbook would
	// commit garbage that the next wiki write would recompile over.
	sourceFull := filepath.Join(worker.Repo().Root(), filepath.FromSlash(playbookSourceRel(slug)))
	if _, err := os.Stat(sourceFull); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("source playbook %s does not exist", playbookSourceRel(slug))})
		return
	}
	sha, _, err := worker.EnqueuePlaybookCompile(r.Context(), slug, ArchivistAuthor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug":       slug,
		"skill_path": CompiledSkillRelPath(slug),
		"commit_sha": sha,
	})
}

// handlePlaybookExecution is POST /playbook/execution.
//
//	body: { "slug", "outcome", "summary", "notes"?, "recorded_by"? }
//	resp: { "execution_id", "execution_count" }
func (b *Broker) handlePlaybookExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.ensurePlaybookExecutionLog()
	log := b.PlaybookExecutionLog()
	if log == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playbook backend is not active"})
		return
	}
	var body struct {
		Slug       string `json:"slug"`
		Outcome    string `json:"outcome"`
		Summary    string `json:"summary"`
		Notes      string `json:"notes"`
		RecordedBy string `json:"recorded_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	recordedBy := strings.TrimSpace(body.RecordedBy)
	if recordedBy == "" {
		recordedBy = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if recordedBy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "recorded_by or X-WUPHF-Agent header is required"})
		return
	}
	slug := strings.TrimSpace(body.Slug)
	outcome := PlaybookOutcome(strings.TrimSpace(body.Outcome))
	if err := ValidateExecutionInput(slug, outcome, body.Summary, body.Notes, recordedBy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	entry, err := log.Append(r.Context(), slug, outcome, body.Summary, body.Notes, recordedBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	all, _ := log.List(slug)

	// Close the loop: notify the synthesizer so it can debounce + enqueue a
	// synthesis when the threshold is crossed. Non-blocking; any errors are
	// already logged inside OnExecutionRecorded.
	if synth := b.PlaybookSynthesizer(); synth != nil {
		synth.OnExecutionRecorded(slug)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id":    entry.ID,
		"execution_count": len(all),
	})
}

// handlePlaybookExecutionsList is GET /playbook/executions?slug=.
func (b *Broker) handlePlaybookExecutionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.ensurePlaybookExecutionLog()
	log := b.PlaybookExecutionLog()
	if log == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playbook backend is not active"})
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if !slugPattern.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)})
		return
	}
	entries, err := log.List(slug)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []Execution{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": entries})
}

// ── Playbook synthesizer wiring ──────────────────────────────────────────
//
// The compounding-intelligence loop — executions accumulate, a broker-level
// LLM synthesis worker periodically updates the "What we've learned"
// section of the source playbook, and the existing auto-recompile hook
// regenerates the SKILL.md so the next agent starts smarter.
//
// Kept in this file (not a new file) so the merge-surface with the parallel
// editable-wiki branch stays additive. The synthesizer itself lives in
// playbook_synthesizer.go.

// PlaybookSynthesizer returns the active synthesizer or nil before Start
// has wired it.
func (b *Broker) PlaybookSynthesizer() *PlaybookSynthesizer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.playbookSynthesizer
}

// SetPlaybookSynthesizer wires a synthesizer from tests. Must be called
// after ensurePlaybookExecutionLog would have run.
func (b *Broker) SetPlaybookSynthesizer(synth *PlaybookSynthesizer) {
	b.mu.Lock()
	b.playbookSynthesizer = synth
	b.mu.Unlock()
}

// ensurePlaybookSynthesizer initializes the playbook synthesizer once the
// execution log is online. Idempotent.
func (b *Broker) ensurePlaybookSynthesizer() {
	b.mu.Lock()
	if b.playbookSynthesizer != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	execLog := b.PlaybookExecutionLog()
	if execLog == nil {
		return
	}
	cfg := PlaybookSynthesizerConfig{
		Threshold: resolvePlaybookSynthThresholdFromEnv(),
		Timeout:   resolvePlaybookSynthTimeoutFromEnv(),
	}
	synth := NewPlaybookSynthesizer(worker, execLog, b, cfg)
	synth.Start(context.Background())
	b.mu.Lock()
	b.playbookSynthesizer = synth
	b.mu.Unlock()
}

func resolvePlaybookSynthThresholdFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_PLAYBOOK_SYNTHESIS_THRESHOLD"))
	if raw == "" {
		return DefaultPlaybookSynthesisThreshold
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return DefaultPlaybookSynthesisThreshold
	}
	return n
}

func resolvePlaybookSynthTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_PLAYBOOK_SYNTHESIS_TIMEOUT"))
	if raw == "" {
		return DefaultPlaybookSynthesisTimeout
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return DefaultPlaybookSynthesisTimeout
	}
	return time.Duration(secs) * time.Second
}

// SubscribePlaybookSynthesizedEvents returns a channel of playbook-synthesis
// events plus an unsubscribe func. Mirrors SubscribePlaybookExecutionEvents.
func (b *Broker) SubscribePlaybookSynthesizedEvents(buffer int) (<-chan PlaybookSynthesizedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	subs := playbookSynthSubsByBroker[b]
	if subs == nil {
		subs = make(map[int]chan PlaybookSynthesizedEvent)
		playbookSynthSubsByBroker[b] = subs
	}
	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.mu.Unlock()
	ch := make(chan PlaybookSynthesizedEvent, buffer)
	subs[id] = ch
	return ch, func() {
		playbookSubscribersMu.Lock()
		defer playbookSubscribersMu.Unlock()
		if subs := playbookSynthSubsByBroker[b]; subs != nil {
			if existing, ok := subs[id]; ok {
				delete(subs, id)
				close(existing)
			}
		}
	}
}

// PublishPlaybookSynthesized fans out a synthesis event. Implements the
// playbookSynthEventPublisher interface consumed by PlaybookSynthesizer.
func (b *Broker) PublishPlaybookSynthesized(evt PlaybookSynthesizedEvent) {
	playbookSubscribersMu.Lock()
	defer playbookSubscribersMu.Unlock()
	for _, ch := range playbookSynthSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// handlePlaybookSynthesize is POST /playbook/synthesize.
//
//	body: { "slug": "churn-prevention", "actor_slug"?: "..." }
//	resp: { "synthesis_id", "queued_at" }
//
// On-demand trigger for humans or agents who just landed a particularly
// useful outcome. Always goes through the debounce/coalesce path, so
// repeated clicks don't stack work.
func (b *Broker) handlePlaybookSynthesize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.ensurePlaybookExecutionLog()
	b.ensurePlaybookSynthesizer()
	synth := b.PlaybookSynthesizer()
	if synth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playbook synthesizer is not active"})
		return
	}
	var body struct {
		Slug      string `json:"slug"`
		ActorSlug string `json:"actor_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	slug := strings.TrimSpace(body.Slug)
	if !slugPattern.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)})
		return
	}
	actor := strings.TrimSpace(body.ActorSlug)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if actor == "" {
		actor = "human"
	}
	id, err := synth.SynthesizeNow(r.Context(), slug, actor)
	if err != nil {
		if errors.Is(err, ErrPlaybookSynthQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrPlaybookSynthesizerStopped) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("playbook synth: on-demand enqueue for %s by %s: %v", slug, actor, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"synthesis_id": id,
		"queued_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// handlePlaybookSynthesisStatus is GET /playbook/synthesis-status?slug=.
//
//	resp: {
//	  "slug": "...",
//	  "execution_count": 7,
//	  "last_synthesized_ts": "2026-04-21T10:00:00Z",
//	  "last_synthesized_sha": "abc1234",
//	  "executions_since_last_synthesis": 2,
//	  "threshold": 3,
//	  "source_path": "team/playbooks/churn-prevention.md"
//	}
//
// Powers the "Last synthesis: 2h ago · 7 executions" badge in the web UI.
// Pulled from the frontmatter the synthesizer stamps on every commit, so
// the status is always consistent with the source of truth on disk.
func (b *Broker) handlePlaybookSynthesisStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if !slugPattern.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)})
		return
	}
	sourceRel := playbookSourceRel(slug)
	bytes, err := readArticle(worker.Repo(), sourceRel)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("source playbook %s does not exist", sourceRel)})
		return
	}
	sha, ts, lastCount := parseSynthesisFrontmatter(string(bytes))
	tsStr := ""
	if !ts.IsZero() {
		tsStr = ts.UTC().Format(time.RFC3339)
	}
	execCount := 0
	if execLog := b.PlaybookExecutionLog(); execLog != nil {
		if entries, err := execLog.List(slug); err == nil {
			execCount = len(entries)
		}
	}
	since := execCount - lastCount
	if since < 0 {
		since = 0
	}
	threshold := DefaultPlaybookSynthesisThreshold
	if synth := b.PlaybookSynthesizer(); synth != nil {
		threshold = synth.Threshold()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug":                            slug,
		"source_path":                     sourceRel,
		"execution_count":                 execCount,
		"last_synthesized_ts":             tsStr,
		"last_synthesized_sha":            sha,
		"executions_since_last_synthesis": since,
		"threshold":                       threshold,
	})
}

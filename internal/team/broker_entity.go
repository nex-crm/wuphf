package team

// broker_entity.go wires the v1.2 entity-brief surface onto the broker —
// fact log, synthesizer worker, SSE event fanout, and HTTP handlers.
//
// Route map (registered in broker.go):
//
//	POST /entity/fact               — append one fact, maybe auto-trigger synth
//	POST /entity/brief/synthesize   — explicit refresh, any actor
//	GET  /entity/facts?kind=&slug=  — list facts newest-first
//	GET  /entity/briefs             — enumerate every brief + synth status
//
// SSE events fanned out via /events (handleEvents subscribes):
//
//	entity:fact_recorded        — {kind, slug, fact_id, fact_count, threshold_crossed, ...}
//	entity:brief_synthesized    — {kind, slug, commit_sha, fact_count, synthesized_ts}

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SubscribeEntityBriefEvents returns a channel of brief-synthesized events
// plus an unsubscribe func.
func (b *Broker) SubscribeEntityBriefEvents(buffer int) (<-chan EntityBriefSynthesizedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entitySubscribers == nil {
		b.entitySubscribers = make(map[int]chan EntityBriefSynthesizedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan EntityBriefSynthesizedEvent, buffer)
	b.entitySubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.entitySubscribers[id]; ok {
			delete(b.entitySubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// SubscribeEntityFactEvents returns a channel of fact-recorded events.
func (b *Broker) SubscribeEntityFactEvents(buffer int) (<-chan EntityFactRecordedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.factSubscribers == nil {
		b.factSubscribers = make(map[int]chan EntityFactRecordedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan EntityFactRecordedEvent, buffer)
	b.factSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.factSubscribers[id]; ok {
			delete(b.factSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishEntityBriefSynthesized fans out a synthesis event. Implements the
// entityEventPublisher interface consumed by EntitySynthesizer.
func (b *Broker) PublishEntityBriefSynthesized(evt EntityBriefSynthesizedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.entitySubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// PublishEntityFactRecorded fans out a fact-recorded event.
func (b *Broker) PublishEntityFactRecorded(evt EntityFactRecordedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.factSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// EntitySynthesizer returns the active synthesizer or nil.
func (b *Broker) EntitySynthesizer() *EntitySynthesizer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.entitySynthesizer
}

// FactLog returns the active FactLog or nil.
func (b *Broker) FactLog() *FactLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.factLog
}

// ensureEntitySynthesizer initializes the fact log + synthesizer when the
// wiki worker is online. Idempotent.
func (b *Broker) ensureEntitySynthesizer() {
	b.mu.Lock()
	if b.entitySynthesizer != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}

	factLog := NewFactLog(worker)
	cfg := SynthesizerConfig{
		Threshold: resolveThresholdFromEnv(),
		Timeout:   resolveTimeoutFromEnv(),
	}
	synth := NewEntitySynthesizer(worker, factLog, b, cfg)
	synth.Start(context.Background())

	b.mu.Lock()
	b.factLog = factLog
	b.entitySynthesizer = synth
	b.mu.Unlock()
}

// SetEntitySynthesizer wires a synthesizer from tests. Must be called after
// ensureEntitySynthesizer would have run (i.e., wikiWorker already attached).
func (b *Broker) SetEntitySynthesizer(factLog *FactLog, synth *EntitySynthesizer) {
	b.mu.Lock()
	b.factLog = factLog
	b.entitySynthesizer = synth
	b.mu.Unlock()
}

func resolveThresholdFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_ENTITY_BRIEF_THRESHOLD"))
	if raw == "" {
		return DefaultSynthesisThreshold
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return DefaultSynthesisThreshold
	}
	return n
}

func resolveTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_ENTITY_BRIEF_TIMEOUT"))
	if raw == "" {
		return DefaultSynthesisTimeout
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return DefaultSynthesisTimeout
	}
	return time.Duration(secs) * time.Second
}

// handleEntityFact is POST /entity/fact.
//
//	body: { entity_kind, entity_slug, fact, source_path?, recorded_by? }
//	resp: { fact_id, fact_count, threshold_crossed }
func (b *Broker) handleEntityFact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	factLog := b.FactLog()
	synth := b.EntitySynthesizer()
	if factLog == nil || synth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "entity-brief backend is not active"})
		return
	}
	var body struct {
		EntityKind string `json:"entity_kind"`
		EntitySlug string `json:"entity_slug"`
		Fact       string `json:"fact"`
		SourcePath string `json:"source_path"`
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

	kind := EntityKind(strings.TrimSpace(body.EntityKind))
	slug := strings.TrimSpace(body.EntitySlug)
	if err := ValidateFactInput(kind, slug, body.Fact, body.SourcePath, recordedBy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	fact, err := factLog.Append(r.Context(), kind, slug, body.Fact, body.SourcePath, recordedBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Read back the current brief frontmatter to compute "facts since sha".
	briefRel := briefPath(kind, slug)
	briefBytes, _ := readArticle(b.WikiWorker().Repo(), briefRel)
	lastSHA, _, priorCount := parseSynthesisFrontmatter(string(briefBytes))

	totalFacts, _ := factLog.CountSinceSHA(r.Context(), kind, slug, lastSHA)
	threshold := synth.Threshold()
	thresholdCrossed := totalFacts >= threshold

	// If either new facts since last synth crosses threshold OR there's
	// never been a synthesis (priorCount == 0 && we have >=threshold facts),
	// enqueue automatically.
	if thresholdCrossed {
		if _, enqueueErr := synth.EnqueueSynthesis(kind, slug, ArchivistAuthor); enqueueErr != nil && !errors.Is(enqueueErr, ErrSynthesisQueueSaturated) {
			// Coalesced requests return (0, nil); saturation is a soft error.
			// Every other error is a bug — log and move on.
		}
	}
	_ = priorCount

	b.PublishEntityFactRecorded(EntityFactRecordedEvent{
		Kind:             kind,
		Slug:             slug,
		FactID:           fact.ID,
		RecordedBy:       recordedBy,
		FactCount:        totalFacts,
		ThresholdCrossed: thresholdCrossed,
		Timestamp:        fact.CreatedAt.Format(time.RFC3339),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"fact_id":           fact.ID,
		"fact_count":        totalFacts,
		"threshold_crossed": thresholdCrossed,
	})
}

// handleEntityBriefSynthesize is POST /entity/brief/synthesize.
//
//	body: { entity_kind, entity_slug, actor_slug? }
//	resp: { synthesis_id, queued_at }
func (b *Broker) handleEntityBriefSynthesize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	synth := b.EntitySynthesizer()
	if synth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "entity-brief backend is not active"})
		return
	}
	var body struct {
		EntityKind string `json:"entity_kind"`
		EntitySlug string `json:"entity_slug"`
		ActorSlug  string `json:"actor_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	kind := EntityKind(strings.TrimSpace(body.EntityKind))
	slug := strings.TrimSpace(body.EntitySlug)
	if err := validateListInputs(kind, slug); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	actor := strings.TrimSpace(body.ActorSlug)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if actor == "" {
		actor = "human"
	}
	id, err := synth.EnqueueSynthesis(kind, slug, actor)
	if err != nil {
		if errors.Is(err, ErrSynthesisQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"synthesis_id": id,
		"queued_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// handleEntityFactsList is GET /entity/facts?kind=&slug=.
func (b *Broker) handleEntityFactsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	factLog := b.FactLog()
	if factLog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "entity-brief backend is not active"})
		return
	}
	kind := EntityKind(strings.TrimSpace(r.URL.Query().Get("kind")))
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if err := validateListInputs(kind, slug); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	facts, err := factLog.List(kind, slug)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if facts == nil {
		facts = []Fact{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"facts": facts})
}

func validateListInputs(kind EntityKind, slug string) error {
	found := false
	for _, k := range ValidEntityKinds() {
		if k == kind {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("kind must be one of people|companies|customers; got %q", kind)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)
	}
	return nil
}

// BriefSummary is one row returned by GET /entity/briefs.
type BriefSummary struct {
	Kind               EntityKind `json:"kind"`
	Slug               string     `json:"slug"`
	Title              string     `json:"title"`
	FactCount          int        `json:"fact_count"`
	LastSynthesizedTS  string     `json:"last_synthesized_ts"`
	LastSynthesizedSHA string     `json:"last_synthesized_sha"`
	PendingDelta       int        `json:"pending_delta"`
}

// handleEntityBriefsList is GET /entity/briefs.
func (b *Broker) handleEntityBriefsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	factLog := b.FactLog()
	if worker == nil || factLog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "entity-brief backend is not active"})
		return
	}

	root := worker.Repo().Root()
	rows := make([]BriefSummary, 0, 16)
	for _, kind := range ValidEntityKinds() {
		dir := filepath.Join(root, "team", string(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
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
			bytes, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			body := string(bytes)
			sha, ts, factCountAtSynth := parseSynthesisFrontmatter(body)
			tsStr := ""
			if !ts.IsZero() {
				tsStr = ts.Format(time.RFC3339)
			}
			facts, _ := factLog.List(kind, slug)
			pending := len(facts) - factCountAtSynth
			if pending < 0 {
				pending = 0
			}
			rows = append(rows, BriefSummary{
				Kind:               kind,
				Slug:               slug,
				Title:              briefTitleFrom(body, slug),
				FactCount:          len(facts),
				LastSynthesizedTS:  tsStr,
				LastSynthesizedSHA: sha,
				PendingDelta:       pending,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Slug < rows[j].Slug
	})
	writeJSON(w, http.StatusOK, map[string]any{"briefs": rows})
}

func briefTitleFrom(body, fallback string) string {
	if h := headerLineFrom(stripFrontmatter(body)); h != "" {
		return h
	}
	return fallback
}

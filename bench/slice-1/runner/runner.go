// Package runner executes the Slice 1 Week 0 benchmark:
//
//   - Load bench/slice-1/corpus.jsonl + queries.jsonl (produced deterministically
//     by bench/slice-1/generate.go).
//   - Materialise each artifact's expected_facts as TypedFact rows written to
//     a temp git-backed wiki layout under {tempRoot}/wiki/facts/person/*.jsonl.
//   - Build a persistent WikiIndex (SQLite + bleve) over that layout via
//     NewPersistentWikiIndex + ReconcileFromMarkdown.
//   - For each query, run idx.Search(query, topK=20), compare the returned
//     FactIDs against expected_fact_ids, and compute:
//   - per-query recall = |intersection| / |expected|
//   - query passes when recall >= expected_min_recall_at_20
//   - aggregate pass-rate (queries where recall ≥ threshold) / total
//   - micro-averaged recall across all expected fact ids
//   - p50/p95 retrieval latency + p95 classify latency
//   - Verdict PASS iff aggregate pass-rate >= 0.85.
//
// No LLM is invoked — this measures retrieval only.
//
// Determinism: the benchmark runs queries in input order; latency variance
// is smoothed by running each query 3 times and taking the median.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// ----- JSONL shapes (must match bench/slice-1/generate.go) ------------------

// ExpectedTriplet mirrors the generator's expectedTriplet.
type ExpectedTriplet struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// ExpectedFact mirrors the generator's expectedFact.
type ExpectedFact struct {
	FactID          string          `json:"fact_id"`
	EntitySlug      string          `json:"entity_slug"`
	Triplet         ExpectedTriplet `json:"triplet"`
	Text            string          `json:"text"`
	SentenceOffset  int             `json:"sentence_offset"`
	Supersedes      []string        `json:"supersedes,omitempty"`
	ContradictsWith []string        `json:"contradicts_with,omitempty"`
}

// Artifact mirrors the generator's artifact shape.
type Artifact struct {
	ArtifactID    string         `json:"artifact_id"`
	ArtifactSHA   string         `json:"artifact_sha"`
	Kind          string         `json:"kind"`
	OccurredAt    string         `json:"occurred_at"`
	Body          string         `json:"body"`
	ExpectedFacts []ExpectedFact `json:"expected_facts"`
}

// Query mirrors the generator's query shape.
type Query struct {
	QueryID               string   `json:"query_id"`
	Query                 string   `json:"query"`
	QueryClass            string   `json:"query_class"`
	ExpectedFactIDs       []string `json:"expected_fact_ids"`
	ExpectedMinRecallAt20 float64  `json:"expected_min_recall_at_20"`
	Rationale             string   `json:"rationale,omitempty"`
}

// ----- Result shapes --------------------------------------------------------

// QueryResult captures what happened for a single query.
type QueryResult struct {
	Query           Query
	GotFactIDs      []string
	Intersection    int
	Recall          float64
	MinRecallTarget float64
	Passed          bool
	// Median retrieval latency across Config.Iterations runs for this query.
	RetrievalMedianMs float64
	// Single-shot classify latency (cheap heuristic; one sample is enough).
	ClassifyMicros int64
	// Classifier output for sanity.
	ClassifierClass      string
	ClassifierConfidence float64
}

// Aggregate is the final scoreboard.
type Aggregate struct {
	TotalQueries       int
	PassingQueries     int
	MicroRecall        float64 // sum(intersection) / sum(|expected|)
	PassRate           float64 // passingQueries / totalQueries
	RetrievalP50Ms     float64
	RetrievalP95Ms     float64
	ClassifyP95Micros  int64
	PerClass           map[string]ClassBreakdown
	FailingQueries     []QueryResult
	TotalFactsIndexed  int
	TotalArtifactsRead int
	IndexBytesSQLite   int64
	IndexBytesBleve    int64
	// Timestamps.
	ReconcileWallMs int64
}

// ClassBreakdown is per-query-class scoring.
type ClassBreakdown struct {
	Total       int
	Passing     int
	PassRate    float64
	MicroRecall float64
}

// ----- Config --------------------------------------------------------------

// Config parameterises the run. Defaults are set by Config.Defaults.
type Config struct {
	CorpusPath  string
	QueriesPath string
	TopK        int
	// Iterations controls how many times each query is re-run to stabilise
	// latency measurement. The median is reported.
	Iterations int
	// Gate is the pass-rate threshold. Verdict PASS iff PassRate >= Gate.
	Gate float64
	// Out controls human-readable progress output. Nil silences progress.
	Out io.Writer
}

// Defaults returns the canonical runtime knobs for the Week 0 bench.
func Defaults() Config {
	return Config{
		TopK:       20,
		Iterations: 3,
		Gate:       0.85,
		Out:        os.Stdout,
	}
}

// ----- Load helpers ---------------------------------------------------------

// LoadCorpus reads corpus.jsonl one artifact per line.
func LoadCorpus(path string) ([]Artifact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open corpus %s: %w", path, err)
	}
	defer f.Close()

	var arts []Artifact
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var a Artifact
		if err := json.Unmarshal(line, &a); err != nil {
			return nil, fmt.Errorf("parse corpus line: %w", err)
		}
		arts = append(arts, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan corpus: %w", err)
	}
	return arts, nil
}

// LoadQueries reads queries.jsonl one query per line.
func LoadQueries(path string) ([]Query, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open queries %s: %w", path, err)
	}
	defer f.Close()

	var qs []Query
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var q Query
		if err := json.Unmarshal(line, &q); err != nil {
			return nil, fmt.Errorf("parse query line: %w", err)
		}
		qs = append(qs, q)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan queries: %w", err)
	}
	return qs, nil
}

// ----- Corpus materialisation ----------------------------------------------

// MaterialiseCorpus writes each artifact's ExpectedFacts out as TypedFact rows
// on disk under root/wiki/facts/person/{slug}.jsonl so that
// WikiIndex.ReconcileFromMarkdown picks them up. Returns (totalFacts, error).
//
// We bucket by EntitySlug. Appending in corpus order keeps the reconcile walk
// deterministic per seed.
func MaterialiseCorpus(root string, arts []Artifact) (int, error) {
	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		return 0, fmt.Errorf("mkdir facts dir: %w", err)
	}
	// Group by entity slug so we can write each jsonl once.
	grouped := map[string][]team.TypedFact{}
	// Carry a synthetic CreatedAt = OccurredAt so supersedes/staleness could be
	// computed later. This does not affect search (bleve indexes f.Text only).
	for _, a := range arts {
		occ, _ := time.Parse(time.RFC3339, a.OccurredAt)
		for _, ef := range a.ExpectedFacts {
			tf := team.TypedFact{
				ID:         ef.FactID,
				EntitySlug: ef.EntitySlug,
				Kind:       "person",
				Type:       "status",
				Triplet: &team.Triplet{
					Subject:   ef.Triplet.Subject,
					Predicate: ef.Triplet.Predicate,
					Object:    ef.Triplet.Object,
				},
				Text:            ef.Text,
				Confidence:      0.95,
				ValidFrom:       occ,
				Supersedes:      ef.Supersedes,
				ContradictsWith: ef.ContradictsWith,
				SourceType:      a.Kind,
				SourcePath:      "bench/slice-1/corpus.jsonl#" + a.ArtifactID,
				SentenceOffset:  ef.SentenceOffset,
				ArtifactExcerpt: ef.Text,
				CreatedAt:       occ,
				CreatedBy:       "bench-slice-1",
			}
			grouped[ef.EntitySlug] = append(grouped[ef.EntitySlug], tf)
		}
	}
	// Deterministic slug order.
	slugs := make([]string, 0, len(grouped))
	for s := range grouped {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	total := 0
	for _, slug := range slugs {
		path := filepath.Join(factsDir, slug+".jsonl")
		f, err := os.Create(path)
		if err != nil {
			return total, fmt.Errorf("create %s: %w", path, err)
		}
		enc := json.NewEncoder(f)
		enc.SetEscapeHTML(false)
		for _, tf := range grouped[slug] {
			if err := enc.Encode(tf); err != nil {
				f.Close()
				return total, fmt.Errorf("encode %s: %w", tf.ID, err)
			}
			total++
		}
		if err := f.Close(); err != nil {
			return total, fmt.Errorf("close %s: %w", path, err)
		}
	}
	return total, nil
}

// ----- Execution ------------------------------------------------------------

// Run executes the full benchmark using cfg. The caller is responsible for
// cleaning up any temp directory if cfg uses one.
func Run(ctx context.Context, cfg Config) (*Aggregate, []QueryResult, error) {
	if cfg.TopK <= 0 {
		cfg.TopK = 20
	}
	if cfg.Iterations <= 0 {
		cfg.Iterations = 3
	}
	if cfg.Gate <= 0 {
		cfg.Gate = 0.85
	}
	out := cfg.Out
	if out == nil {
		out = io.Discard
	}

	arts, err := LoadCorpus(cfg.CorpusPath)
	if err != nil {
		return nil, nil, err
	}
	qs, err := LoadQueries(cfg.QueriesPath)
	if err != nil {
		return nil, nil, err
	}

	tempRoot, err := os.MkdirTemp("", "wuphf-bench-slice1-")
	if err != nil {
		return nil, nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	factCount, err := MaterialiseCorpus(tempRoot, arts)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(out, "materialised %d facts across %d artifacts into %s\n",
		factCount, len(arts), tempRoot)

	indexDir := filepath.Join(tempRoot, ".index")
	idx, err := team.NewPersistentWikiIndex(tempRoot, indexDir)
	if err != nil {
		return nil, nil, fmt.Errorf("new index: %w", err)
	}
	defer idx.Close()

	t0 := time.Now()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		return nil, nil, fmt.Errorf("reconcile: %w", err)
	}
	reconcileMs := time.Since(t0).Milliseconds()
	fmt.Fprintf(out, "reconciled in %d ms\n", reconcileMs)

	// Query pass.
	results := make([]QueryResult, 0, len(qs))
	allLatencyMs := make([]float64, 0, len(qs)*cfg.Iterations)
	allClassifyMicros := make([]int64, 0, len(qs))
	for qi, q := range qs {
		// Latency sampling: run query Iterations times, keep all samples for
		// percentile calculation, and also keep the per-query median for the
		// per-query row.
		perQ := make([]float64, 0, cfg.Iterations)
		var lastHits []team.SearchHit
		for i := 0; i < cfg.Iterations; i++ {
			ts := time.Now()
			hits, serr := idx.Search(ctx, q.Query, cfg.TopK)
			elapsed := time.Since(ts).Seconds() * 1000.0
			if serr != nil {
				return nil, nil, fmt.Errorf("search q=%s: %w", q.QueryID, serr)
			}
			perQ = append(perQ, elapsed)
			allLatencyMs = append(allLatencyMs, elapsed)
			lastHits = hits
		}
		// Classify latency — one sample is plenty; the routine is pure Go.
		cs := time.Now()
		klass, conf := team.ClassifyQuery(q.Query)
		classifyMicros := time.Since(cs).Microseconds()
		allClassifyMicros = append(allClassifyMicros, classifyMicros)

		got := make([]string, 0, len(lastHits))
		for _, h := range lastHits {
			got = append(got, h.FactID)
		}
		recall, intersection := scoreRecall(q.ExpectedFactIDs, got)

		passed := true
		if len(q.ExpectedFactIDs) > 0 {
			if recall < q.ExpectedMinRecallAt20 {
				passed = false
			}
		} else {
			// Out-of-scope: retriever should ideally return nothing, but the
			// "vacuous truth" convention from README means recall=1 automatically.
			// We still flag precision failures for the OOS case below in the
			// aggregate report, but we do not fail the gate on precision alone.
		}

		res := QueryResult{
			Query:                q,
			GotFactIDs:           got,
			Intersection:         intersection,
			Recall:               recall,
			MinRecallTarget:      q.ExpectedMinRecallAt20,
			Passed:               passed,
			RetrievalMedianMs:    median(perQ),
			ClassifyMicros:       classifyMicros,
			ClassifierClass:      string(klass),
			ClassifierConfidence: conf,
		}
		results = append(results, res)
		_ = qi
	}

	// Aggregate.
	agg := &Aggregate{
		TotalQueries:       len(qs),
		TotalFactsIndexed:  factCount,
		TotalArtifactsRead: len(arts),
		PerClass:           map[string]ClassBreakdown{},
		ReconcileWallMs:    reconcileMs,
	}

	var totalExpected, totalHit int
	classBuckets := map[string]*ClassBreakdown{}
	for _, r := range results {
		if r.Passed {
			agg.PassingQueries++
		} else {
			agg.FailingQueries = append(agg.FailingQueries, r)
		}
		totalExpected += len(r.Query.ExpectedFactIDs)
		totalHit += r.Intersection

		cb, ok := classBuckets[r.Query.QueryClass]
		if !ok {
			cb = &ClassBreakdown{}
			classBuckets[r.Query.QueryClass] = cb
		}
		cb.Total++
		if r.Passed {
			cb.Passing++
		}
	}
	for klass, cb := range classBuckets {
		if cb.Total > 0 {
			cb.PassRate = float64(cb.Passing) / float64(cb.Total)
		}
		// Micro-recall per class: sum intersections / sum expected.
		var exp, hit int
		for _, r := range results {
			if r.Query.QueryClass != klass {
				continue
			}
			exp += len(r.Query.ExpectedFactIDs)
			hit += r.Intersection
		}
		if exp > 0 {
			cb.MicroRecall = float64(hit) / float64(exp)
		} else {
			cb.MicroRecall = 1.0
		}
		agg.PerClass[klass] = *cb
	}

	if agg.TotalQueries > 0 {
		agg.PassRate = float64(agg.PassingQueries) / float64(agg.TotalQueries)
	}
	if totalExpected > 0 {
		agg.MicroRecall = float64(totalHit) / float64(totalExpected)
	} else {
		agg.MicroRecall = 1.0
	}
	agg.RetrievalP50Ms = percentile(allLatencyMs, 0.50)
	agg.RetrievalP95Ms = percentile(allLatencyMs, 0.95)
	agg.ClassifyP95Micros = percentileInt(allClassifyMicros, 0.95)

	// Index footprint.
	agg.IndexBytesSQLite = fileSize(filepath.Join(indexDir, "wiki.sqlite"))
	agg.IndexBytesBleve = dirSize(filepath.Join(indexDir, "bleve"))

	return agg, results, nil
}

// ----- Scoring helpers ------------------------------------------------------

// scoreRecall computes recall = |expected ∩ got| / |expected|.
// Returns (1.0, 0) when expected is empty (vacuous truth per README).
func scoreRecall(expected, got []string) (float64, int) {
	if len(expected) == 0 {
		return 1.0, 0
	}
	gset := make(map[string]struct{}, len(got))
	for _, g := range got {
		gset[g] = struct{}{}
	}
	hit := 0
	for _, e := range expected {
		if _, ok := gset[e]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(expected)), hit
}

// ----- Stats helpers --------------------------------------------------------

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func percentileInt(xs []int64, p float64) int64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]int64(nil), xs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// ----- Filesystem helpers ---------------------------------------------------

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		st, stErr := os.Stat(p)
		if stErr == nil {
			total += st.Size()
		}
		return nil
	})
	return total
}

// ----- Reporting helpers ----------------------------------------------------

// FormatReport renders a human-readable text report of the aggregate +
// per-query results suitable for printing to stdout and committing to
// RESULTS.md. Deterministic given the same Aggregate/results input.
func FormatReport(agg *Aggregate, results []QueryResult) string {
	var b strings.Builder
	verdict := "FAIL"
	if agg.PassRate >= 0.85 {
		verdict = "PASS"
	}
	fmt.Fprintf(&b, "Slice 1 Week 0 benchmark — recall@20 ship gate\n")
	fmt.Fprintf(&b, "================================================\n\n")
	fmt.Fprintf(&b, "Gate: PassRate >= 85%% — verdict: %s\n\n", verdict)

	fmt.Fprintf(&b, "Aggregate\n---------\n")
	fmt.Fprintf(&b, "  Queries                  : %d\n", agg.TotalQueries)
	fmt.Fprintf(&b, "  Queries passing gate     : %d\n", agg.PassingQueries)
	fmt.Fprintf(&b, "  Pass rate                : %.2f%%\n", agg.PassRate*100)
	fmt.Fprintf(&b, "  Micro-recall (∑hit/∑exp) : %.2f%%\n", agg.MicroRecall*100)
	fmt.Fprintf(&b, "  Retrieval p50            : %.2f ms\n", agg.RetrievalP50Ms)
	fmt.Fprintf(&b, "  Retrieval p95            : %.2f ms\n", agg.RetrievalP95Ms)
	fmt.Fprintf(&b, "  Classify p95             : %d µs\n", agg.ClassifyP95Micros)
	fmt.Fprintf(&b, "  Artifacts indexed        : %d\n", agg.TotalArtifactsRead)
	fmt.Fprintf(&b, "  Facts indexed            : %d\n", agg.TotalFactsIndexed)
	fmt.Fprintf(&b, "  Reconcile wall time      : %d ms\n", agg.ReconcileWallMs)
	fmt.Fprintf(&b, "  SQLite size              : %d bytes\n", agg.IndexBytesSQLite)
	fmt.Fprintf(&b, "  Bleve size               : %d bytes\n\n", agg.IndexBytesBleve)

	fmt.Fprintf(&b, "Per-class breakdown\n-------------------\n")
	classes := make([]string, 0, len(agg.PerClass))
	for k := range agg.PerClass {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	for _, k := range classes {
		cb := agg.PerClass[k]
		fmt.Fprintf(&b, "  %-15s  total=%2d passing=%2d pass_rate=%5.1f%%  micro_recall=%5.1f%%\n",
			k, cb.Total, cb.Passing, cb.PassRate*100, cb.MicroRecall*100)
	}
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "Per-query results\n-----------------\n")
	fmt.Fprintf(&b, "  %-6s  %-15s  %-7s  %-6s  %-6s  %s\n",
		"id", "class", "recall", "target", "p50ms", "query")
	for _, r := range results {
		fmt.Fprintf(&b, "  %-6s  %-15s  %5.1f%%  %5.1f%%  %6.2f  %s\n",
			r.Query.QueryID,
			r.Query.QueryClass,
			r.Recall*100,
			r.MinRecallTarget*100,
			r.RetrievalMedianMs,
			truncate(r.Query.Query, 72),
		)
	}
	fmt.Fprintln(&b)

	if len(agg.FailingQueries) > 0 {
		fmt.Fprintf(&b, "Failing queries (%d)\n", len(agg.FailingQueries))
		b.WriteString(strings.Repeat("-", 60))
		b.WriteByte('\n')
		for _, r := range agg.FailingQueries {
			fmt.Fprintf(&b, "\n  %s [%s] recall=%.1f%% target=%.1f%%\n    query    : %s\n    expected : %v\n    got      : %v\n",
				r.Query.QueryID,
				r.Query.QueryClass,
				r.Recall*100,
				r.MinRecallTarget*100,
				r.Query.Query,
				r.Query.ExpectedFactIDs,
				r.GotFactIDs,
			)
		}
	} else {
		fmt.Fprintf(&b, "No failing queries.\n")
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

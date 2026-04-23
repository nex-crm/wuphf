package team

// wiki_lint.go implements the daily wiki health check described in
// docs/specs/WIKI-SCHEMA.md §9.
//
// Five checks run in order:
//   1. Contradictions  — critical
//   2. Orphans         — warning
//   3. Stale claims    — warning
//   4. Missing cross-refs — info
//   5. Dedup review    — info
//
// Run() serializes all writes through WikiWorker so the single-writer
// invariant is preserved. The report is committed under the synthetic
// "archivist" identity at wiki/.lint/report-YYYY-MM-DD.md.
//
// QueryProvider abstracts the LLM call so tests can inject a deterministic
// fake without shelling out to a real provider.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LintProvider is the narrow interface the lint runner needs for LLM
// judgment calls. Production code wires in a closure over
// provider.RunConfiguredOneShot; tests substitute a deterministic fake.
type LintProvider interface {
	// Query sends systemPrompt + userPrompt to the configured LLM and
	// returns the raw response string or an error.
	Query(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LintFinding is one item in a LintReport.
type LintFinding struct {
	// Severity is "critical" | "warning" | "info".
	Severity string `json:"severity"`
	// Type is one of: contradictions | orphans | stale | missing_crossrefs | dedup_review.
	Type string `json:"type"`
	// EntitySlug is the entity this finding relates to.
	EntitySlug string `json:"entity_slug,omitempty"`
	// FactIDs are the IDs of the facts involved in this finding.
	FactIDs []string `json:"fact_ids,omitempty"`
	// Summary is a human-readable description of the finding.
	Summary string `json:"summary"`
	// ResolveActions lists the choices presented to the user for contradiction
	// findings: e.g. ["Fact A (id: abc123)", "Fact B (id: def456)", "Both"].
	// Empty for non-contradiction findings.
	ResolveActions []string `json:"resolve_actions,omitempty"`
}

// LintReport is the full output of one lint run.
type LintReport struct {
	// Date is the YYYY-MM-DD the lint ran.
	Date string `json:"date"`
	// Findings is the ordered list of findings (critical first, then warning, then info).
	Findings []LintFinding `json:"findings"`
}

// Lint runs all five checks against the wiki index and commits the report.
type Lint struct {
	index    *WikiIndex
	worker   *WikiWorker
	provider LintProvider
	now      func() time.Time
}

// NewLint constructs a Lint runner. All three arguments are required.
func NewLint(idx *WikiIndex, worker *WikiWorker, prov LintProvider) *Lint {
	return &Lint{
		index:    idx,
		worker:   worker,
		provider: prov,
		now:      time.Now,
	}
}

// Run executes all five lint checks, builds the report, commits it via
// WikiWorker, and returns the report. The returned report reflects only what
// was found — callers do not need to read the committed markdown to iterate
// findings.
func (l *Lint) Run(ctx context.Context) (LintReport, error) {
	date := l.now().Format("2006-01-02")
	report := LintReport{Date: date}

	var findings []LintFinding

	// Check 1: Contradictions (critical)
	contradictions, err := l.checkContradictions(ctx)
	if err != nil {
		log.Printf("wiki lint: contradictions check error: %v", err)
	}
	findings = append(findings, contradictions...)

	// Check 2: Orphans (warning)
	orphans, err := l.checkOrphans(ctx)
	if err != nil {
		log.Printf("wiki lint: orphans check error: %v", err)
	}
	findings = append(findings, orphans...)

	// Check 3: Stale claims (warning)
	stale, err := l.checkStale(ctx)
	if err != nil {
		log.Printf("wiki lint: stale check error: %v", err)
	}
	findings = append(findings, stale...)

	// Check 4: Missing cross-refs (info)
	crossrefs, err := l.checkMissingCrossRefs(ctx)
	if err != nil {
		log.Printf("wiki lint: crossrefs check error: %v", err)
	}
	findings = append(findings, crossrefs...)

	// Check 5: Dedup review (info)
	dedup, err := l.checkDedupReview(ctx)
	if err != nil {
		log.Printf("wiki lint: dedup check error: %v", err)
	}
	findings = append(findings, dedup...)

	report.Findings = findings

	// Commit the report via WikiWorker under archivist identity.
	if err := l.commitReport(ctx, report); err != nil {
		return report, fmt.Errorf("wiki lint: commit report: %w", err)
	}

	return report, nil
}

// ResolveContradiction resolves the contradiction at findings[findingIdx].
//
//   - winner == "A": appends supersedes:[B.id] to fact A; sets valid_until on B to now.
//   - winner == "B": mirrored.
//   - winner == "Both": appends contradicts_with:[other.id] to each; both stay valid.
//
// All writes go through WikiWorker under the caller's git identity.
func (l *Lint) ResolveContradiction(ctx context.Context, report LintReport, findingIdx int, winner string, identity HumanIdentity) error {
	if findingIdx < 0 || findingIdx >= len(report.Findings) {
		return fmt.Errorf("wiki lint: finding index %d out of range (0-%d)", findingIdx, len(report.Findings)-1)
	}
	finding := report.Findings[findingIdx]
	if finding.Type != "contradictions" {
		return fmt.Errorf("wiki lint: finding %d is type %q, not contradictions", findingIdx, finding.Type)
	}
	if len(finding.FactIDs) < 2 {
		return fmt.Errorf("wiki lint: contradiction finding %d has fewer than 2 fact IDs", findingIdx)
	}

	idA := finding.FactIDs[0]
	idB := finding.FactIDs[1]

	switch winner {
	case "A":
		return l.resolveWin(ctx, finding.EntitySlug, idA, idB, identity)
	case "B":
		return l.resolveWin(ctx, finding.EntitySlug, idB, idA, identity)
	case "Both":
		return l.resolveBoth(ctx, finding.EntitySlug, idA, idB, identity)
	default:
		return fmt.Errorf("wiki lint: winner must be A, B, or Both; got %q", winner)
	}
}

// resolveWin makes `winID` the surviving fact and `loseID` the superseded one.
// It appends supersedes:[loseID] to the winner's fact record and sets
// valid_until on the loser to now. Both writes go through the WikiWorker.
func (l *Lint) resolveWin(ctx context.Context, entitySlug, winID, loseID string, identity HumanIdentity) error {
	now := l.now().UTC().Format(time.RFC3339)

	// Mutate in the fact log. We walk all known fact paths for this entity slug
	// and rewrite the relevant lines.
	if err := l.mutateFact(ctx, entitySlug, winID, func(f *TypedFact) {
		// Append loseID to supersedes list (deduplicate).
		for _, s := range f.Supersedes {
			if s == loseID {
				return
			}
		}
		f.Supersedes = append(f.Supersedes, loseID)
	}, identity); err != nil {
		return fmt.Errorf("wiki lint resolve: update winner %s: %w", winID, err)
	}

	if err := l.mutateFact(ctx, entitySlug, loseID, func(f *TypedFact) {
		if f.ValidUntil == nil {
			t, _ := time.Parse(time.RFC3339, now)
			f.ValidUntil = &t
		}
	}, identity); err != nil {
		return fmt.Errorf("wiki lint resolve: set valid_until on loser %s: %w", loseID, err)
	}

	return nil
}

// resolveBoth marks both facts as aware of each other's existence without
// declaring a winner. Lint stops flagging the cluster once contradicts_with
// is set on both sides.
func (l *Lint) resolveBoth(ctx context.Context, entitySlug, idA, idB string, identity HumanIdentity) error {
	if err := l.mutateFact(ctx, entitySlug, idA, func(f *TypedFact) {
		for _, c := range f.ContradictsWith {
			if c == idB {
				return
			}
		}
		f.ContradictsWith = append(f.ContradictsWith, idB)
	}, identity); err != nil {
		return fmt.Errorf("wiki lint resolve both: update fact %s: %w", idA, err)
	}

	if err := l.mutateFact(ctx, entitySlug, idB, func(f *TypedFact) {
		for _, c := range f.ContradictsWith {
			if c == idA {
				return
			}
		}
		f.ContradictsWith = append(f.ContradictsWith, idA)
	}, identity); err != nil {
		return fmt.Errorf("wiki lint resolve both: update fact %s: %w", idB, err)
	}

	return nil
}

// mutateFact finds the fact with `id` across all JSONL files for `entitySlug`,
// applies `mutate`, and rewrites the file via WikiWorker. It walks the two
// canonical fact-log locations (wiki/facts/ and team/entities/).
func (l *Lint) mutateFact(ctx context.Context, entitySlug, id string, mutate func(*TypedFact), identity HumanIdentity) error {
	if l.worker == nil {
		return ErrWorkerStopped
	}
	root := l.worker.repo.Root()

	// Candidate fact-log directories per §3.
	dirs := []string{
		filepath.Join(root, "wiki", "facts"),
		filepath.Join(root, "team", "entities"),
	}

	for _, dir := range dirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable dirs
			}
			if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			if found, writeErr := l.rewriteFactInFile(ctx, root, path, id, mutate, identity); writeErr != nil {
				return writeErr
			} else if found {
				return filepath.SkipAll
			}
			return nil
		}); err != nil && err != filepath.SkipAll {
			return err
		}
	}
	return nil
}

// rewriteFactInFile scans one JSONL file for the target fact ID, applies the
// mutation, rewrites the full file, and commits via WikiWorker. Returns true
// when the fact was found.
func (l *Lint) rewriteFactInFile(ctx context.Context, root, absPath, id string, mutate func(*TypedFact), identity HumanIdentity) (bool, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false, nil
	}

	lines := strings.Split(string(data), "\n")
	found := false
	var buf bytes.Buffer
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var f TypedFact
		if err := json.Unmarshal([]byte(trimmed), &f); err != nil {
			buf.WriteString(line + "\n")
			continue
		}
		if f.ID == id {
			found = true
			mutate(&f)
			mutated, err := json.Marshal(f)
			if err != nil {
				return true, fmt.Errorf("wiki lint: marshal mutated fact %s: %w", id, err)
			}
			buf.Write(mutated)
			buf.WriteByte('\n')
		} else {
			buf.WriteString(trimmed + "\n")
		}
	}

	if !found {
		return false, nil
	}

	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return true, fmt.Errorf("wiki lint: rel path: %w", err)
	}
	rel = filepath.ToSlash(rel)

	authorSlug := identity.Slug
	if authorSlug == "" {
		authorSlug = ArchivistAuthor
	}
	_, _, err = l.worker.EnqueueFactLog(ctx, authorSlug, rel, buf.String(), fmt.Sprintf("lint: resolve contradiction on fact %s", id))
	if err != nil {
		return true, fmt.Errorf("wiki lint: commit mutation: %w", err)
	}
	return true, nil
}

// --- Five lint checks --------------------------------------------------------

// checkContradictions groups entity facts by (subject, predicate) and runs the
// LLM judge on any cluster with ≥ 2 facts.
func (l *Lint) checkContradictions(ctx context.Context) ([]LintFinding, error) {
	if l.index == nil {
		return nil, nil
	}
	root := l.worker.repo.Root()

	// Walk all entities by reading brief files.
	entities, err := l.allEntitySlugs(root)
	if err != nil {
		return nil, err
	}

	var findings []LintFinding
	for _, slug := range entities {
		facts, err := l.index.ListFactsForEntity(ctx, slug)
		if err != nil {
			continue
		}

		// Group by (subject, predicate).
		type clusterKey struct{ Subject, Predicate string }
		clusters := make(map[clusterKey][]TypedFact)
		for _, f := range facts {
			if f.Triplet == nil {
				continue
			}
			k := clusterKey{Subject: f.Triplet.Subject, Predicate: f.Triplet.Predicate}
			clusters[k] = append(clusters[k], f)
		}

		for key, cluster := range clusters {
			if len(cluster) < 2 {
				continue
			}
			// Skip if all have contradicts_with already set (previously acknowledged).
			if allAcknowledged(cluster) {
				continue
			}

			contradicts, reason, err := l.judgeCluster(ctx, slug, key.Subject, key.Predicate, cluster)
			if err != nil {
				log.Printf("wiki lint: judge error for %s %s/%s: %v", slug, key.Subject, key.Predicate, err)
				continue
			}
			if !contradicts {
				continue
			}

			ids := make([]string, 0, len(cluster))
			for _, f := range cluster {
				ids = append(ids, f.ID)
			}
			// Build resolve action labels using first two facts.
			factA := cluster[0]
			factB := cluster[1]
			findings = append(findings, LintFinding{
				Severity:   "critical",
				Type:       "contradictions",
				EntitySlug: slug,
				FactIDs:    ids,
				Summary:    fmt.Sprintf("Contradiction on %s/%s for %s: %s", key.Subject, key.Predicate, slug, reason),
				ResolveActions: []string{
					fmt.Sprintf("Fact A (id: %s): %s", factA.ID, shortText(factA.Text, 60)),
					fmt.Sprintf("Fact B (id: %s): %s", factB.ID, shortText(factB.Text, 60)),
					"Both",
				},
			})
		}
	}
	return findings, nil
}

// allAcknowledged returns true when every fact in the cluster already has
// contradicts_with set, meaning a human previously chose "Both".
func allAcknowledged(cluster []TypedFact) bool {
	for _, f := range cluster {
		if len(f.ContradictsWith) == 0 {
			return false
		}
	}
	return true
}

// judgeCluster sends the cluster to the LLM lint judge and returns whether it
// contradicts plus the reason.
func (l *Lint) judgeCluster(ctx context.Context, entitySlug, subject, predicate string, cluster []TypedFact) (bool, string, error) {
	// Build the user prompt manually (the template is checked in as a .tmpl
	// file for agents; the Go runner uses a simplified inline expansion).
	var sb strings.Builder
	sb.WriteString("Entity: ")
	sb.WriteString(entitySlug)
	sb.WriteString("\nSubject: ")
	sb.WriteString(subject)
	sb.WriteString("\nPredicate: ")
	sb.WriteString(predicate)
	sb.WriteString("\nFacts in cluster:\n")
	for _, f := range cluster {
		sb.WriteString(fmt.Sprintf("- ID: %s | Text: %s | ValidFrom: %s | ValidUntil: %v | Source: %s\n",
			f.ID, f.Text, f.ValidFrom.Format(time.RFC3339), formatValidUntil(f.ValidUntil), f.SourcePath))
	}
	sb.WriteString("\nRespond with exactly {\"contradicts\": true|false, \"reason\": \"...\"}")

	systemPrompt := "You are the WUPHF lint judge. Given a cluster of facts sharing (subject, predicate), determine if there is a real semantic contradiction. Read docs/specs/WIKI-SCHEMA.md §10.4 rules: temporal validity, specificity, and paraphrase are NOT contradictions. Real contradictions are currently-valid, mutually-exclusive claims."

	raw, err := l.provider.Query(ctx, systemPrompt, sb.String())
	if err != nil {
		return false, "", fmt.Errorf("judge LLM call: %w", err)
	}

	var result struct {
		Contradicts bool   `json:"contradicts"`
		Reason      string `json:"reason"`
	}
	// Find the JSON block in the response.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return false, "", fmt.Errorf("LLM response has no JSON block: %q", raw)
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return false, "", fmt.Errorf("parse LLM response: %w", err)
	}
	return result.Contradicts, result.Reason, nil
}

// checkOrphans finds briefs with no inbound graph edge AND no fact activity in 90 days.
func (l *Lint) checkOrphans(ctx context.Context) ([]LintFinding, error) {
	if l.index == nil {
		return nil, nil
	}
	root := l.worker.repo.Root()
	entities, err := l.allEntitySlugs(root)
	if err != nil {
		return nil, err
	}

	cutoff := l.now().Add(-90 * 24 * time.Hour)
	var findings []LintFinding

	for _, slug := range entities {
		edges, err := l.index.ListEdgesForEntity(ctx, slug)
		if err != nil {
			continue
		}
		if len(edges) > 0 {
			continue // has graph edges — not an orphan
		}

		facts, err := l.index.ListFactsForEntity(ctx, slug)
		if err != nil {
			continue
		}
		hasRecentActivity := false
		for _, f := range facts {
			anchor := f.CreatedAt
			if !f.ValidFrom.IsZero() {
				anchor = f.ValidFrom
			}
			if anchor.After(cutoff) {
				hasRecentActivity = true
				break
			}
		}
		if hasRecentActivity {
			continue
		}

		findings = append(findings, LintFinding{
			Severity:   "warning",
			Type:       "orphans",
			EntitySlug: slug,
			Summary:    fmt.Sprintf("Orphan: %s has no graph edges and no fact activity in the last 90 days", slug),
		})
	}
	return findings, nil
}

// checkStale finds facts with Staleness > 30 that were never reinforced.
func (l *Lint) checkStale(ctx context.Context) ([]LintFinding, error) {
	if l.index == nil {
		return nil, nil
	}
	root := l.worker.repo.Root()
	entities, err := l.allEntitySlugs(root)
	if err != nil {
		return nil, err
	}

	now := l.now()
	var findings []LintFinding

	for _, slug := range entities {
		facts, err := l.index.ListFactsForEntity(ctx, slug)
		if err != nil {
			continue
		}
		for _, f := range facts {
			if f.ReinforcedAt != nil {
				continue // reinforced — skip
			}
			if Staleness(f, now) > 30 {
				findings = append(findings, LintFinding{
					Severity:   "warning",
					Type:       "stale",
					EntitySlug: slug,
					FactIDs:    []string{f.ID},
					Summary:    fmt.Sprintf("Stale claim on %s (staleness %.1f): %s", slug, Staleness(f, now), shortText(f.Text, 80)),
				})
			}
		}
	}
	return findings, nil
}

// checkMissingCrossRefs finds entity pairs that co-occur as subject/object in
// ≥ 3 facts but lack a typed graph edge.
func (l *Lint) checkMissingCrossRefs(ctx context.Context) ([]LintFinding, error) {
	if l.index == nil {
		return nil, nil
	}
	root := l.worker.repo.Root()
	entities, err := l.allEntitySlugs(root)
	if err != nil {
		return nil, err
	}

	// Count co-occurrences across all entity facts.
	type pair struct{ A, B string }
	coOccurrences := make(map[pair]int)

	for _, slug := range entities {
		facts, err := l.index.ListFactsForEntity(ctx, slug)
		if err != nil {
			continue
		}
		for _, f := range facts {
			if f.Triplet == nil {
				continue
			}
			obj := f.Triplet.Object
			if obj == "" || obj == slug {
				continue
			}
			// Normalize the pair so A < B lexicographically.
			a, b := slug, obj
			if a > b {
				a, b = b, a
			}
			coOccurrences[pair{A: a, B: b}]++
		}
	}

	// Find pairs with ≥ 3 co-occurrences but no edge.
	var findings []LintFinding
	for p, count := range coOccurrences {
		if count < 3 {
			continue
		}
		// Check if there's already a graph edge between them.
		edgesA, _ := l.index.ListEdgesForEntity(ctx, p.A)
		hasEdge := false
		for _, e := range edgesA {
			if e.Object == p.B || e.Subject == p.B {
				hasEdge = true
				break
			}
		}
		if hasEdge {
			continue
		}
		findings = append(findings, LintFinding{
			Severity: "info",
			Type:     "missing_crossrefs",
			Summary:  fmt.Sprintf("Missing cross-ref: %s and %s co-occur in %d facts but have no graph edge", p.A, p.B, count),
		})
	}
	return findings, nil
}

// checkDedupReview stubs the Jaro-Winkler borderline-merge audit check.
// Full implementation requires a merge audit log that is not yet in the
// infrastructure. The stub emits no findings but is wired in so the report
// section renders.
//
// TODO: implement when wiki/.dedup-audit.jsonl is available (tracks merges
// with Jaro-Winkler scores so borderline 0.90-0.93 merges can be surfaced here).
func (l *Lint) checkDedupReview(_ context.Context) ([]LintFinding, error) {
	// No-op stub. When the dedup audit log lands, walk it for merges in the
	// last 7 days with scores in the [0.90, 0.93) range and emit info findings.
	return nil, nil
}

// --- Report formatting -------------------------------------------------------

// commitReport formats the LintReport as a markdown article and writes it
// via the WikiWorker under the archivist identity.
func (l *Lint) commitReport(ctx context.Context, report LintReport) error {
	if l.worker == nil {
		return ErrWorkerStopped
	}

	content := formatLintReport(report)
	path := fmt.Sprintf("wiki/.lint/report-%s.md", report.Date)
	commitMsg := fmt.Sprintf("lint: daily report %s — %d findings", report.Date, len(report.Findings))

	_, _, err := l.worker.EnqueueLintReport(ctx, ArchivistAuthor, path, content, commitMsg)
	return err
}

// formatLintReport renders the report as a Wikipedia-article-shaped markdown
// document with sections in the fixed order defined by §4.6.
func formatLintReport(report LintReport) string {
	var sb strings.Builder

	countBySev := func(sev string) int {
		n := 0
		for _, f := range report.Findings {
			if f.Severity == sev {
				n++
			}
		}
		return n
	}
	countByType := func(typ string) int {
		n := 0
		for _, f := range report.Findings {
			if f.Type == typ {
				n++
			}
		}
		return n
	}

	sb.WriteString(fmt.Sprintf("# Lint report — %s\n\n", report.Date))
	sb.WriteString(fmt.Sprintf("Daily health check. %d critical, %d warnings, %d info.\n\n",
		countBySev("critical"), countBySev("warning"), countBySev("info")))

	// Fixed section order per §4.6.
	sections := []struct {
		Type  string
		Title string
	}{
		{"contradictions", "Contradictions"},
		{"orphans", "Orphans"},
		{"stale", "Stale claims"},
		{"missing_crossrefs", "Missing cross-refs"},
		{"dedup_review", "Dedup review"},
	}

	for _, sec := range sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", sec.Title))
		n := countByType(sec.Type)
		if n == 0 {
			sb.WriteString("None.\n\n")
			continue
		}
		for _, f := range report.Findings {
			if f.Type != sec.Type {
				continue
			}
			sb.WriteString(fmt.Sprintf("- **[%s]** %s", strings.ToUpper(f.Severity[:1])+f.Severity[1:], f.Summary))
			if f.EntitySlug != "" {
				sb.WriteString(fmt.Sprintf(" — [[%s]]", f.EntitySlug))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Sources\n\n")
	sb.WriteString("Generated by the WUPHF archivist lint runner. Source: wiki fact logs and graph.log.\n")

	return sb.String()
}

// --- Helpers -----------------------------------------------------------------

// allEntitySlugs returns the slugs of all entity briefs found in team/.
func (l *Lint) allEntitySlugs(root string) ([]string, error) {
	teamDir := filepath.Join(root, "team")
	info, err := os.Stat(teamDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	var slugs []string
	err = filepath.WalkDir(teamDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(teamDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Skip lint reports, catalog, and other non-entity files.
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) != 2 {
			return nil
		}
		slug := strings.TrimSuffix(parts[1], ".md")
		// Skip hidden files and generated indexes.
		if strings.HasPrefix(parts[0], ".") || strings.HasPrefix(slug, "_") || slug == "all" {
			return nil
		}
		slugs = append(slugs, slug)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(slugs)
	return slugs, nil
}

func shortText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func formatValidUntil(t *time.Time) string {
	if t == nil {
		return "null"
	}
	return t.Format(time.RFC3339)
}

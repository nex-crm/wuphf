package team

// learnings.go is the typed, wiki-backed memory layer for reusable team
// learnings. It complements playbooks: playbooks stay procedural, while
// learnings are scoped, searchable observations that can be retrieved before
// future agent work.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	MaxLearningInsightLen     = 4000
	MaxLearningKeyLen         = 80
	MaxLearningScopeLen       = 128
	DefaultLearningLimit      = 20
	MaxLearningLimit          = 100
	maxLearningJSONLLineBytes = 1024 * 1024
)

var (
	ErrInvalidLearning       = errors.New("team learnings: invalid learning")
	ErrLearningLogNotRunning = errors.New("team learnings: worker is not attached")
	learningKeyPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	learningScopePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9:_./-]*$`)
)

type LearningType string

const (
	LearningTypePattern      LearningType = "pattern"
	LearningTypePitfall      LearningType = "pitfall"
	LearningTypePreference   LearningType = "preference"
	LearningTypeArchitecture LearningType = "architecture"
	LearningTypeTool         LearningType = "tool"
	LearningTypeOperational  LearningType = "operational"
)

func ValidLearningTypes() []LearningType {
	return []LearningType{
		LearningTypePattern,
		LearningTypePitfall,
		LearningTypePreference,
		LearningTypeArchitecture,
		LearningTypeTool,
		LearningTypeOperational,
	}
}

type LearningSource string

const (
	LearningSourceUserStated LearningSource = "user-stated"
	LearningSourceObserved   LearningSource = "observed"
	LearningSourceInferred   LearningSource = "inferred"
	LearningSourceExecution  LearningSource = "execution"
	LearningSourceSynthesis  LearningSource = "synthesis"
	LearningSourceCrossAgent LearningSource = "cross-agent"
	LearningSourceCrossModel LearningSource = "cross-model"
)

func ValidLearningSources() []LearningSource {
	return []LearningSource{
		LearningSourceUserStated,
		LearningSourceObserved,
		LearningSourceInferred,
		LearningSourceExecution,
		LearningSourceSynthesis,
		LearningSourceCrossAgent,
		LearningSourceCrossModel,
	}
}

type LearningRecord struct {
	ID           string         `json:"id"`
	Type         LearningType   `json:"type"`
	Key          string         `json:"key"`
	Insight      string         `json:"insight"`
	Confidence   int            `json:"confidence"`
	Source       LearningSource `json:"source"`
	Trusted      bool           `json:"trusted"`
	Scope        string         `json:"scope"`
	PlaybookSlug string         `json:"playbook_slug,omitempty"`
	ExecutionID  string         `json:"execution_id,omitempty"`
	TaskID       string         `json:"task_id,omitempty"`
	Files        []string       `json:"files,omitempty"`
	Entities     []string       `json:"entities,omitempty"`
	CreatedBy    string         `json:"created_by"`
	CreatedAt    time.Time      `json:"created_at"`
	Supersedes   string         `json:"supersedes,omitempty"`
}

type LearningSearchFilters struct {
	Query        string
	Scope        string
	Type         LearningType
	Source       LearningSource
	Trusted      *bool
	PlaybookSlug string
	File         string
	Limit        int
}

type LearningSearchResult struct {
	LearningRecord
	EffectiveConfidence int `json:"effective_confidence"`
}

type LearningLog struct {
	worker *WikiWorker
	mu     sync.Mutex
}

func NewLearningLog(worker *WikiWorker) *LearningLog {
	return &LearningLog{worker: worker}
}

func ValidateLearningInput(rec LearningRecord) error {
	if !isValidLearningType(rec.Type) {
		return fmt.Errorf("type must be one of pattern|pitfall|preference|architecture|tool|operational; got %q", rec.Type)
	}
	if !learningKeyPattern.MatchString(rec.Key) {
		return fmt.Errorf("key must match ^[a-z0-9][a-z0-9_-]*$; got %q", rec.Key)
	}
	if len(rec.Key) > MaxLearningKeyLen {
		return fmt.Errorf("key must be <= %d chars; got %d", MaxLearningKeyLen, len(rec.Key))
	}
	insight := strings.TrimSpace(rec.Insight)
	if insight == "" {
		return fmt.Errorf("insight is required")
	}
	if len(insight) > MaxLearningInsightLen {
		return fmt.Errorf("insight must be <= %d chars; got %d", MaxLearningInsightLen, len(insight))
	}
	if containsInstructionLikeLearning(insight) {
		return fmt.Errorf("insight looks like an instruction override; record evidence, not prompt-control text")
	}
	if rec.Confidence < 1 || rec.Confidence > 10 {
		return fmt.Errorf("confidence must be between 1 and 10; got %d", rec.Confidence)
	}
	if !isValidLearningSource(rec.Source) {
		return fmt.Errorf("source must be one of user-stated|observed|inferred|execution|synthesis|cross-agent|cross-model; got %q", rec.Source)
	}
	scope := strings.TrimSpace(rec.Scope)
	if scope == "" {
		return fmt.Errorf("scope is required")
	}
	if len(scope) > MaxLearningScopeLen || !learningScopePattern.MatchString(scope) || strings.Contains(scope, "..") {
		return fmt.Errorf("scope must be a safe scoped key; got %q", rec.Scope)
	}
	createdBy := strings.TrimSpace(rec.CreatedBy)
	if createdBy == "" {
		return fmt.Errorf("created_by is required")
	}
	if !slugPattern.MatchString(createdBy) {
		return fmt.Errorf("created_by must match ^[a-z0-9][a-z0-9-]*$; got %q", rec.CreatedBy)
	}
	if rec.PlaybookSlug != "" && !slugPattern.MatchString(rec.PlaybookSlug) {
		return fmt.Errorf("playbook_slug must match ^[a-z0-9][a-z0-9-]*$; got %q", rec.PlaybookSlug)
	}
	for _, p := range rec.Files {
		if err := validateLearningFileRef(p); err != nil {
			return err
		}
	}
	return nil
}

func (l *LearningLog) Append(ctx context.Context, rec LearningRecord) (LearningRecord, error) {
	if l == nil || l.worker == nil {
		return LearningRecord{}, ErrLearningLogNotRunning
	}
	rec.Type = LearningType(strings.TrimSpace(string(rec.Type)))
	rec.Key = strings.TrimSpace(rec.Key)
	rec.Insight = strings.TrimSpace(rec.Insight)
	rec.Source = LearningSource(strings.TrimSpace(string(rec.Source)))
	rec.Scope = strings.TrimSpace(rec.Scope)
	if rec.Scope == "" {
		rec.Scope = "repo"
	}
	rec.CreatedBy = strings.TrimSpace(rec.CreatedBy)
	rec.PlaybookSlug = strings.TrimSpace(rec.PlaybookSlug)
	rec.ExecutionID = strings.TrimSpace(rec.ExecutionID)
	rec.TaskID = strings.TrimSpace(rec.TaskID)
	rec.Supersedes = strings.TrimSpace(rec.Supersedes)
	rec.Files = cleanStringList(rec.Files)
	rec.Entities = cleanStringList(rec.Entities)
	if rec.Source == LearningSourceUserStated {
		rec.Trusted = true
	} else {
		rec.Trusted = false
	}
	if err := ValidateLearningInput(rec); err != nil {
		return LearningRecord{}, fmt.Errorf("%w: %w", ErrInvalidLearning, err)
	}
	rec.ID = uuid.NewString()
	rec.CreatedAt = time.Now().UTC()

	line, err := json.Marshal(rec)
	if err != nil {
		return LearningRecord{}, fmt.Errorf("team learnings: marshal: %w", err)
	}
	if len(line) >= maxLearningJSONLLineBytes {
		return LearningRecord{}, fmt.Errorf("%w: learning exceeds %d-byte JSONL line limit", ErrInvalidLearning, maxLearningJSONLLineBytes)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	existing, err := l.readExistingLocked()
	if err != nil {
		return LearningRecord{}, err
	}
	buf := make([]byte, 0, len(existing)+len(line)+1)
	if len(existing) > 0 {
		buf = append(buf, existing...)
		if !strings.HasSuffix(string(existing), "\n") {
			buf = append(buf, '\n')
		}
	}
	buf = append(buf, line...)
	buf = append(buf, '\n')

	records := parseLearningJSONL(buf, TeamLearningsJSONLPath)
	page := RenderTeamLearningsMarkdown(records)
	msg := fmt.Sprintf("learning: %s/%s", rec.Type, rec.Key)
	if _, _, err := l.worker.EnqueueTeamLearning(ctx, rec.CreatedBy, TeamLearningsJSONLPath, string(buf), page, msg); err != nil {
		return LearningRecord{}, fmt.Errorf("team learnings: enqueue: %w", err)
	}
	return rec, nil
}

func (l *LearningLog) Search(filters LearningSearchFilters) ([]LearningSearchResult, error) {
	if l == nil || l.worker == nil {
		return nil, ErrLearningLogNotRunning
	}
	if filters.Limit <= 0 {
		filters.Limit = DefaultLearningLimit
	}
	if filters.Limit > MaxLearningLimit {
		filters.Limit = MaxLearningLimit
	}
	records, err := l.readAll()
	if err != nil {
		return nil, err
	}
	deduped := dedupeLearnings(records)
	query := strings.ToLower(strings.TrimSpace(filters.Query))
	scope := strings.TrimSpace(filters.Scope)
	playbookSlug := strings.TrimSpace(filters.PlaybookSlug)
	file := strings.TrimSpace(filters.File)
	results := make([]LearningSearchResult, 0, len(deduped))
	for _, rec := range deduped {
		if filters.Type != "" && rec.Type != filters.Type {
			continue
		}
		if filters.Source != "" && rec.Source != filters.Source {
			continue
		}
		if filters.Trusted != nil && rec.Trusted != *filters.Trusted {
			continue
		}
		if scope != "" && rec.Scope != scope {
			continue
		}
		if playbookSlug != "" && rec.PlaybookSlug != playbookSlug && rec.Scope != "playbook:"+playbookSlug {
			continue
		}
		if file != "" && !stringSliceContains(rec.Files, file) {
			continue
		}
		if query != "" && !learningMatchesQuery(rec, query) {
			continue
		}
		results = append(results, LearningSearchResult{
			LearningRecord:      rec,
			EffectiveConfidence: effectiveLearningConfidence(rec, time.Now().UTC()),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].EffectiveConfidence != results[j].EffectiveConfidence {
			return results[i].EffectiveConfidence > results[j].EffectiveConfidence
		}
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	if len(results) > filters.Limit {
		results = results[:filters.Limit]
	}
	return results, nil
}

func (l *LearningLog) readAll() ([]LearningRecord, error) {
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(TeamLearningsJSONLPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("team learnings: read: %w", err)
	}
	return parseLearningJSONL(bytes, TeamLearningsJSONLPath), nil
}

func (l *LearningLog) readExistingLocked() ([]byte, error) {
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(TeamLearningsJSONLPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("team learnings: read existing: %w", err)
	}
	return bytes, nil
}

func RenderTeamLearningsMarkdown(records []LearningRecord) string {
	results := make([]LearningSearchResult, 0, len(records))
	for _, rec := range dedupeLearnings(records) {
		results = append(results, LearningSearchResult{
			LearningRecord:      rec,
			EffectiveConfidence: effectiveLearningConfidence(rec, time.Now().UTC()),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		if results[i].EffectiveConfidence != results[j].EffectiveConfidence {
			return results[i].EffectiveConfidence > results[j].EffectiveConfidence
		}
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	var b strings.Builder
	b.WriteString("# Team Learnings\n\n")
	b.WriteString("> Generated from `team/learnings/index.jsonl`. Record durable memory through `team_learning_record`; do not edit this generated page by hand.\n\n")
	if len(results) == 0 {
		b.WriteString("_No learnings recorded yet._\n")
		return b.String()
	}
	currentType := LearningType("")
	for _, result := range results {
		rec := result.LearningRecord
		if rec.Type != currentType {
			currentType = rec.Type
			b.WriteString("## ")
			b.WriteString(titleCaseHyphen(string(currentType)))
			b.WriteString("\n\n")
		}
		b.WriteString("### ")
		b.WriteString(rec.Key)
		b.WriteString("\n\n")
		b.WriteString(markdownQuoteBlock(rec.Insight))
		b.WriteString("\n\n")
		b.WriteString("- Scope: `")
		b.WriteString(rec.Scope)
		b.WriteString("`\n")
		b.WriteString(fmt.Sprintf("- Confidence: %d/10", rec.Confidence))
		if result.EffectiveConfidence != rec.Confidence {
			b.WriteString(fmt.Sprintf(" (effective %d/10)", result.EffectiveConfidence))
		}
		b.WriteString("\n")
		b.WriteString("- Source: `")
		b.WriteString(string(rec.Source))
		b.WriteString("`\n")
		b.WriteString("- Trusted: ")
		if rec.Trusted {
			b.WriteString("yes\n")
		} else {
			b.WriteString("no\n")
		}
		b.WriteString("- Recorded: ")
		b.WriteString(rec.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString(" by `")
		b.WriteString(rec.CreatedBy)
		b.WriteString("`\n")
		if rec.PlaybookSlug != "" {
			b.WriteString("- Playbook: `")
			b.WriteString(rec.PlaybookSlug)
			b.WriteString("`\n")
		}
		if rec.ExecutionID != "" {
			b.WriteString("- Execution: `")
			b.WriteString(markdownInlineText(rec.ExecutionID))
			b.WriteString("`\n")
		}
		if len(rec.Files) > 0 {
			b.WriteString("- Files: ")
			b.WriteString(joinBackticked(rec.Files))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseLearningJSONL(bytes []byte, relPath string) []LearningRecord {
	scanner := bufio.NewScanner(strings.NewReader(string(bytes)))
	scanner.Buffer(make([]byte, 64*1024), maxLearningJSONLLineBytes)
	lineNo := 0
	out := make([]LearningRecord, 0, 16)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec LearningRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			log.Printf("team learnings: skip malformed line %d in %s: %v", lineNo, relPath, err)
			continue
		}
		if rec.ID == "" || rec.Type == "" || rec.Key == "" || rec.Insight == "" {
			log.Printf("team learnings: skip underspecified line %d in %s", lineNo, relPath)
			continue
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("team learnings: scanner error in %s after line %d: %v", relPath, lineNo, err)
	}
	return out
}

func dedupeLearnings(records []LearningRecord) []LearningRecord {
	byKey := make(map[string]LearningRecord, len(records))
	for _, rec := range records {
		key := rec.Scope + "|" + string(rec.Type) + "|" + rec.Key
		existing, ok := byKey[key]
		if !ok || rec.CreatedAt.After(existing.CreatedAt) || rec.CreatedAt.Equal(existing.CreatedAt) {
			byKey[key] = rec
		}
	}
	out := make([]LearningRecord, 0, len(byKey))
	for _, rec := range byKey {
		out = append(out, rec)
	}
	return out
}

func effectiveLearningConfidence(rec LearningRecord, now time.Time) int {
	if rec.Trusted || rec.Source == LearningSourceUserStated || rec.CreatedAt.IsZero() {
		return rec.Confidence
	}
	ageDays := int(now.Sub(rec.CreatedAt).Hours() / 24)
	if ageDays <= 0 {
		return rec.Confidence
	}
	decay := ageDays / 30
	effective := rec.Confidence - decay
	if effective < 1 {
		return 1
	}
	return effective
}

func learningMatchesQuery(rec LearningRecord, query string) bool {
	fields := []string{
		rec.Key,
		rec.Insight,
		rec.Scope,
		string(rec.Type),
		string(rec.Source),
		rec.PlaybookSlug,
		rec.TaskID,
	}
	fields = append(fields, rec.Files...)
	fields = append(fields, rec.Entities...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

func containsInstructionLikeLearning(insight string) bool {
	lower := strings.ToLower(insight)
	bad := []string{
		"ignore previous instructions",
		"ignore all previous",
		"you are now",
		"always output no findings",
		"skip security",
		"skip review",
		"skip checks",
		"override:",
		"system:",
		"assistant:",
		"user:",
		"do not report",
		"do not flag",
		"do not mention",
		"approve all",
		"approve every",
	}
	for _, phrase := range bad {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func validateLearningFileRef(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	if filepath.IsAbs(p) || strings.Contains(p, "..") || strings.HasPrefix(p, "~") {
		return fmt.Errorf("files must be safe relative paths; got %q", path)
	}
	return nil
}

func isValidLearningType(t LearningType) bool {
	for _, valid := range ValidLearningTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

func isValidLearningSource(s LearningSource) bool {
	for _, valid := range ValidLearningSources() {
		if s == valid {
			return true
		}
	}
	return false
}

func cleanStringList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func stringSliceContains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func joinBackticked(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, "`"+markdownInlineText(item)+"`")
	}
	return strings.Join(out, ", ")
}

func markdownInlineText(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	s = strings.ReplaceAll(s, "`", "&#96;")
	return s
}

func markdownQuoteBlock(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ">"
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("> ")
		b.WriteString(markdownInlineText(line))
	}
	return b.String()
}

func titleCaseHyphen(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

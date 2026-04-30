package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type contextHarnessEvalFixture struct {
	Scenario          string `json:"scenario"`
	TaskType          string `json:"task_type"`
	LookupQuery       string `json:"lookup_query"`
	RetrievalContract struct {
		RequiredCapabilities  []string `json:"required_capabilities"`
		RequiresSemanticIndex bool     `json:"requires_semantic_index"`
		RequiresExternalNet   bool     `json:"requires_external_network"`
	} `json:"retrieval_contract"`
	Runs []passportProcessEvalRun `json:"runs"`
}

type passportProcessEvalRun struct {
	ID                             string   `json:"id"`
	Prompt                         string   `json:"prompt"`
	AgentSlug                      string   `json:"agent_slug"`
	NotebookPath                   string   `json:"notebook_path"`
	WikiPath                       string   `json:"wiki_path"`
	OfficialSource                 string   `json:"official_source"`
	MustStartWithoutPriorArtifacts bool     `json:"must_start_without_prior_artifacts"`
	ExpectedPriorCitations         []string `json:"expected_prior_citations"`
	PromotionSkipReason            string   `json:"promotion_skip_reason"`
}

type contextHarnessEvalEvent struct {
	RunID        string
	Tool         string
	Path         string
	Citations    []string
	SkipReason   string
	Capabilities []string
}

type contextHarnessEvalRunner struct {
	t             *testing.T
	worker        *WikiWorker
	events        []contextHarnessEvalEvent
	workflowTasks map[string]*teamTask
}

func TestContextHarnessPassportProcessEvalRequiresPriorArtifactBeforeResearch(t *testing.T) {
	fixture := loadContextHarnessEvalFixture(t)
	if len(fixture.Runs) != 2 {
		t.Fatalf("fixture must contain exactly two runs, got %d", len(fixture.Runs))
	}

	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	runner := &contextHarnessEvalRunner{
		t:             t,
		worker:        worker,
		workflowTasks: make(map[string]*teamTask),
	}

	for _, run := range fixture.Runs {
		runner.runPassportProcessTask(fixture.LookupQuery, run)
	}

	first := fixture.Runs[0]
	second := fixture.Runs[1]
	assertFirstRunCapturesPromotesAndCites(t, runner.events, first)
	assertSecondRunCitesPriorArtifactsBeforeResearch(t, runner.events, second)
	assertPromotedRunMemoryWorkflowSatisfied(t, runner.workflowTasks[first.ID], first)
	assertSecondRunMemoryWorkflowRecordedPriorCitations(t, runner.workflowTasks[second.ID], second)
}

func TestContextHarnessPassportProcessEvalDoesNotRequireSemanticSearch(t *testing.T) {
	fixture := loadContextHarnessEvalFixture(t)
	if fixture.RetrievalContract.RequiresSemanticIndex {
		t.Fatal("passport-process eval must pass on the markdown harness without a semantic index")
	}
	if fixture.RetrievalContract.RequiresExternalNet {
		t.Fatal("passport-process eval must not make live network calls")
	}

	banned := []string{"vector", "rrf", "embedding", "embeddings", "semantic", "pgvector"}
	for _, capability := range fixture.RetrievalContract.RequiredCapabilities {
		normalized := strings.ToLower(capability)
		for _, term := range banned {
			if strings.Contains(normalized, term) {
				t.Fatalf("required capability %q must not require %s on this branch", capability, term)
			}
		}
	}
}

func (r *contextHarnessEvalRunner) runPassportProcessTask(query string, run passportProcessEvalRun) {
	r.t.Helper()
	task := newPassportProcessEvalWorkflowTask(run)
	r.workflowTasks[run.ID] = task

	prior := r.literalContextLookup(run.ID, query)
	priorPaths := citationPaths(prior)
	recordMemoryWorkflowLookup(task, run.AgentSlug, query, prior, evalTimestamp("01"))
	if run.MustStartWithoutPriorArtifacts && len(priorPaths) != 0 {
		r.t.Fatalf("%s should start without prior artifacts, got %v", run.ID, priorPaths)
	}

	if len(run.ExpectedPriorCitations) > 0 {
		for _, want := range run.ExpectedPriorCitations {
			if !slices.Contains(priorPaths, want) {
				r.t.Fatalf("%s lookup missing prior artifact %q; got %v", run.ID, want, priorPaths)
			}
		}
		r.events = append(r.events, contextHarnessEvalEvent{
			RunID:     run.ID,
			Tool:      "context_cite",
			Citations: append([]string(nil), run.ExpectedPriorCitations...),
		})
	}

	r.events = append(r.events, contextHarnessEvalEvent{
		RunID: run.ID,
		Tool:  "fresh_research",
		Path:  run.OfficialSource,
	})

	notebookContent := passportProcessNotebookContent(run, query)
	if _, _, err := r.worker.NotebookWrite(context.Background(), run.AgentSlug, run.NotebookPath, notebookContent, "create", "eval: capture passport process note"); err != nil {
		r.t.Fatalf("%s capture notebook: %v", run.ID, err)
	}
	recordMemoryWorkflowCapture(task, run.AgentSlug, MemoryWorkflowArtifact{
		Backend: "markdown",
		Source:  "notebook",
		Path:    run.NotebookPath,
		Title:   "Passport Process",
	}, evalTimestamp("02"))
	r.events = append(r.events, contextHarnessEvalEvent{
		RunID: run.ID,
		Tool:  "context_capture",
		Path:  run.NotebookPath,
	})

	if run.WikiPath != "" {
		promotion := &Promotion{
			ID:         "eval-" + run.ID,
			SourceSlug: run.AgentSlug,
			SourcePath: run.NotebookPath,
			TargetPath: run.WikiPath,
			Rationale:  "Promote reusable passport process research for repeated-task recall.",
		}
		if _, err := r.worker.Repo().ApplyPromotion(context.Background(), promotion, "human"); err != nil {
			r.t.Fatalf("%s promote notebook: %v", run.ID, err)
		}
		recordMemoryWorkflowPromotion(task, run.AgentSlug, MemoryWorkflowArtifact{
			Backend:     "markdown",
			Source:      "promotion",
			Path:        run.WikiPath,
			PromotionID: promotion.ID,
			Title:       "Passport Process",
		}, evalTimestamp("03"))
		r.events = append(r.events, contextHarnessEvalEvent{
			RunID: run.ID,
			Tool:  "context_promote",
			Path:  run.WikiPath,
		})
	} else {
		if strings.TrimSpace(run.PromotionSkipReason) == "" {
			r.t.Fatalf("%s must either promote or record an explicit promotion skip reason", run.ID)
		}
		// MemoryWorkflow currently records promotion artifacts, while this eval
		// keeps the explicit skip reason in the deterministic trace.
		r.events = append(r.events, contextHarnessEvalEvent{
			RunID:      run.ID,
			Tool:       "context_promote",
			SkipReason: run.PromotionSkipReason,
		})
	}

	citations := []string{run.NotebookPath}
	if run.WikiPath != "" {
		citations = append(citations, run.WikiPath)
	}
	citations = append(citations, run.ExpectedPriorCitations...)
	r.events = append(r.events, contextHarnessEvalEvent{
		RunID:     run.ID,
		Tool:      "final_answer",
		Citations: uniqueStrings(citations),
	})
}

func (r *contextHarnessEvalRunner) literalContextLookup(runID, query string) []ContextCitation {
	r.t.Helper()
	r.events = append(r.events, contextHarnessEvalEvent{
		RunID:        runID,
		Tool:         "context_lookup",
		Capabilities: []string{"literal_notebook_search", "literal_wiki_search"},
	})

	var citations []ContextCitation
	slugs, err := r.worker.AgentsWithNotebooks()
	if err != nil {
		r.t.Fatalf("%s list notebooks: %v", runID, err)
	}
	for _, slug := range slugs {
		hits, err := r.worker.NotebookSearch(slug, query)
		if err != nil {
			r.t.Fatalf("%s notebook search %s: %v", runID, slug, err)
		}
		for _, hit := range hits {
			citations = append(citations, ContextCitation{
				Backend:   "markdown",
				Source:    "notebook",
				Path:      hit.Path,
				LineStart: hit.Line,
				LineEnd:   hit.Line,
				Snippet:   hit.Snippet,
			})
		}
	}

	hits, err := searchArticles(r.worker.Repo(), query)
	if err != nil {
		r.t.Fatalf("%s wiki search: %v", runID, err)
	}
	for _, hit := range hits {
		citations = append(citations, ContextCitation{
			Backend:   "markdown",
			Source:    "wiki",
			Path:      hit.Path,
			LineStart: hit.Line,
			LineEnd:   hit.Line,
			Snippet:   hit.Snippet,
		})
	}
	return uniqueCitationsByPath(citations)
}

func assertFirstRunCapturesPromotesAndCites(t *testing.T, events []contextHarnessEvalEvent, run passportProcessEvalRun) {
	t.Helper()
	for _, tool := range []string{"context_lookup", "fresh_research", "context_capture", "context_promote", "final_answer"} {
		if eventIndex(events, run.ID, tool) < 0 {
			t.Fatalf("%s missing %s event in trace: %+v", run.ID, tool, events)
		}
	}
	if eventIndex(events, run.ID, "context_lookup") > eventIndex(events, run.ID, "fresh_research") {
		t.Fatalf("%s must search prior memory before fresh research", run.ID)
	}
	final := eventByTool(t, events, run.ID, "final_answer")
	if !slices.Contains(final.Citations, run.NotebookPath) {
		t.Fatalf("%s final answer must cite captured notebook %q; got %v", run.ID, run.NotebookPath, final.Citations)
	}
	if !slices.Contains(final.Citations, run.WikiPath) {
		t.Fatalf("%s final answer must cite promoted wiki artifact %q; got %v", run.ID, run.WikiPath, final.Citations)
	}
}

func assertSecondRunCitesPriorArtifactsBeforeResearch(t *testing.T, events []contextHarnessEvalEvent, run passportProcessEvalRun) {
	t.Helper()
	citeIdx := eventIndex(events, run.ID, "context_cite")
	researchIdx := eventIndex(events, run.ID, "fresh_research")
	if citeIdx < 0 {
		t.Fatalf("%s must cite prior context before fresh research", run.ID)
	}
	if researchIdx < 0 {
		t.Fatalf("%s missing fresh research event", run.ID)
	}
	if citeIdx > researchIdx {
		t.Fatalf("%s cited prior context after fresh research; cite=%d research=%d", run.ID, citeIdx, researchIdx)
	}
	cite := eventByTool(t, events, run.ID, "context_cite")
	final := eventByTool(t, events, run.ID, "final_answer")
	for _, want := range run.ExpectedPriorCitations {
		if !slices.Contains(cite.Citations, want) {
			t.Fatalf("%s context_cite missing prior artifact %q; got %v", run.ID, want, cite.Citations)
		}
		if !slices.Contains(final.Citations, want) {
			t.Fatalf("%s final answer missing prior artifact %q; got %v", run.ID, want, final.Citations)
		}
	}
	promote := eventByTool(t, events, run.ID, "context_promote")
	if strings.TrimSpace(promote.SkipReason) == "" && strings.TrimSpace(promote.Path) == "" {
		t.Fatalf("%s must either promote a new artifact or record an explicit skip reason", run.ID)
	}
}

func assertPromotedRunMemoryWorkflowSatisfied(t *testing.T, task *teamTask, run passportProcessEvalRun) {
	t.Helper()
	if task == nil || task.MemoryWorkflow == nil {
		t.Fatalf("%s missing memory workflow", run.ID)
	}
	wf := task.MemoryWorkflow
	if !wf.Required || wf.Status != MemoryWorkflowStatusSatisfied {
		t.Fatalf("%s workflow should be satisfied after lookup/capture/promote, got %+v", run.ID, wf)
	}
	if wf.Lookup.Status != MemoryWorkflowStepStatusSatisfied || wf.Capture.Status != MemoryWorkflowStepStatusSatisfied || wf.Promote.Status != MemoryWorkflowStepStatusSatisfied {
		t.Fatalf("%s workflow steps not satisfied: %+v", run.ID, wf)
	}
	if !memoryWorkflowHasCapturePath(wf, run.NotebookPath) {
		t.Fatalf("%s workflow missing notebook capture %q: %+v", run.ID, run.NotebookPath, wf.Captures)
	}
	if !memoryWorkflowHasPromotionPath(wf, run.WikiPath) {
		t.Fatalf("%s workflow missing wiki promotion %q: %+v", run.ID, run.WikiPath, wf.Promotions)
	}
}

func assertSecondRunMemoryWorkflowRecordedPriorCitations(t *testing.T, task *teamTask, run passportProcessEvalRun) {
	t.Helper()
	if task == nil || task.MemoryWorkflow == nil {
		t.Fatalf("%s missing memory workflow", run.ID)
	}
	for _, want := range run.ExpectedPriorCitations {
		if !memoryWorkflowHasCitationPath(task.MemoryWorkflow, want) {
			t.Fatalf("%s workflow missing prior citation %q: %+v", run.ID, want, task.MemoryWorkflow.Citations)
		}
	}
}

func passportProcessNotebookContent(run passportProcessEvalRun, query string) string {
	return "# Passport Process\n\n" +
		"Reusable task memory for the passport process.\n\n" +
		"## Current Notes\n\n" +
		"- Lookup query: " + query + "\n" +
		"- Task prompt: " + run.Prompt + "\n" +
		"- Official source fixture: " + run.OfficialSource + "\n\n" +
		"---\n\n" +
		"## Timeline\n\n" +
		"- 2026-04-30: Captured deterministic process-research notes without live network calls.\n"
}

func newPassportProcessEvalWorkflowTask(run passportProcessEvalRun) *teamTask {
	task := &teamTask{
		ID:        run.ID,
		Channel:   "eval",
		Title:     run.Prompt,
		Details:   "Repeated passport process research should consult prior notebook/wiki artifacts before fresh research.",
		Owner:     run.AgentSlug,
		Status:    "in_progress",
		CreatedBy: "eval",
		TaskType:  "process_research",
		CreatedAt: evalTimestamp("00"),
		UpdatedAt: evalTimestamp("00"),
	}
	syncTaskMemoryWorkflow(task, evalTimestamp("00"))
	return task
}

func loadContextHarnessEvalFixture(t *testing.T) contextHarnessEvalFixture {
	t.Helper()
	path := filepath.Join("testdata", "context_harness", "passport_process_repeated.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var fixture contextHarnessEvalFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	if fixture.Scenario == "" || fixture.LookupQuery == "" || fixture.TaskType == "" {
		t.Fatalf("fixture missing required scenario metadata: %+v", fixture)
	}
	return fixture
}

func citationPaths(citations []ContextCitation) []string {
	paths := make([]string, 0, len(citations))
	for _, citation := range citations {
		if citation.Path != "" {
			paths = append(paths, citation.Path)
		}
	}
	return uniqueStrings(paths)
}

func uniqueCitationsByPath(citations []ContextCitation) []ContextCitation {
	seen := make(map[string]bool, len(citations))
	out := make([]ContextCitation, 0, len(citations))
	for _, citation := range citations {
		key := citation.Path
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, citation)
	}
	return out
}

func memoryWorkflowHasCitationPath(wf *MemoryWorkflow, path string) bool {
	if wf == nil {
		return false
	}
	for _, citation := range wf.Citations {
		if citation.Path == path {
			return true
		}
	}
	return false
}

func memoryWorkflowHasPromotionPath(wf *MemoryWorkflow, path string) bool {
	if wf == nil {
		return false
	}
	for _, artifact := range wf.Promotions {
		if artifact.Path == path && !artifact.Missing {
			return true
		}
	}
	return false
}

func memoryWorkflowHasCapturePath(wf *MemoryWorkflow, path string) bool {
	if wf == nil {
		return false
	}
	for _, artifact := range wf.Captures {
		if artifact.Path == path && !artifact.Missing {
			return true
		}
	}
	return false
}

func eventIndex(events []contextHarnessEvalEvent, runID, tool string) int {
	for i, event := range events {
		if event.RunID == runID && event.Tool == tool {
			return i
		}
	}
	return -1
}

func eventByTool(t *testing.T, events []contextHarnessEvalEvent, runID, tool string) contextHarnessEvalEvent {
	t.Helper()
	for _, event := range events {
		if event.RunID == runID && event.Tool == tool {
			return event
		}
	}
	t.Fatalf("%s missing %s event in trace: %+v", runID, tool, events)
	return contextHarnessEvalEvent{}
}

func evalTimestamp(suffix string) string {
	return "2026-04-30T10:00:" + suffix + "Z"
}

package team

// wiki_query.go — QueryHandler: the core cited-answer loop for /lookup.
//
// Data flow:
//
//   QueryHandler.Answer(ctx, QueryRequest)
//     │
//     ├─ ClassifyQuery → class, confidence
//     │
//     ├─ class==general && conf>=0.8 → return refusal (0 LLM calls)
//     │
//     ├─ idx.Search(ctx, query, topK) → []SearchHit
//     │     └─ store.GetFact per hit → TypedFact + Staleness → QuerySource
//     │
//     ├─ render prompts/answer_query.tmpl → prompt string
//     │
//     ├─ provider.RunPrompt(ctx, ...) → JSON response
//     │
//     └─ json.Unmarshal → QueryAnswer
//
// Timeout handling: if the provider context expires, a QueryAnswer with
// Coverage="none" and a non-empty Sources list is returned (the caller gets
// whatever sources had been assembled before the timeout).
//
// Parse failure: logged and returned with Coverage="none" — never panics.
//
// The prompt is embedded at build time via go:embed so the binary is
// self-contained (no disk read at query time beyond the index).

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/answer_query.tmpl
var answerQueryTmpl string

// QueryProvider is the narrow interface the query handler uses to invoke an
// LLM. Tests substitute a fake to avoid any real network call.
type QueryProvider interface {
	RunPrompt(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// QueryRequest carries all inputs to QueryHandler.Answer.
type QueryRequest struct {
	Query       string        // natural-language question
	RequestedBy string        // slug of agent or human asking
	TopK        int           // default 20 if zero
	Timeout     time.Duration // default 10s if zero
}

// QueryAnswer is the structured response returned to the caller.
type QueryAnswer struct {
	QueryClass     QueryClass    `json:"query_class"`
	AnswerMarkdown string        `json:"answer_markdown"`
	SourcesCited   []int         `json:"sources_cited"`
	Sources        []QuerySource `json:"sources"`
	Confidence     float64       `json:"confidence"`
	Coverage       string        `json:"coverage"` // complete | partial | none
	Notes          string        `json:"notes,omitempty"`
	LatencyMs      int64         `json:"latency_ms"`
}

// QuerySource is one entry in the sources list passed to the LLM and returned
// in QueryAnswer.Sources. Field names mirror the template variables.
type QuerySource struct {
	Kind       string  `json:"kind"`
	SlugOrID   string  `json:"slug_or_id"`
	Title      string  `json:"title"`
	Excerpt    string  `json:"excerpt"`
	ValidFrom  string  `json:"valid_from,omitempty"`
	ValidUntil string  `json:"valid_until,omitempty"`
	Staleness  float64 `json:"staleness"`
	SourcePath string  `json:"source_path,omitempty"`
}

// llmQueryAnswer is the JSON shape the LLM emits, mapped from answer_query.tmpl.
type llmQueryAnswer struct {
	QueryClass     string  `json:"query_class"`
	AnswerMarkdown string  `json:"answer_markdown"`
	SourcesCited   []int   `json:"sources_cited"`
	Confidence     float64 `json:"confidence"`
	Coverage       string  `json:"coverage"`
	Notes          string  `json:"notes,omitempty"`
}

// templateVars holds the variables rendered into answer_query.tmpl.
type templateVars struct {
	Query      string
	QueryClass string
	Now        string
	Sources    []QuerySource
}

// QueryHandler orchestrates the full cited-answer loop.
type QueryHandler struct {
	index    *WikiIndex
	provider QueryProvider
	// tmpl is parsed once at construction; the embedding guarantees it is
	// always valid at compile time.
	tmpl *template.Template
}

// NewQueryHandler constructs a QueryHandler backed by the given index and
// provider. The embedded prompt template is parsed at construction time so
// errors surface immediately rather than at query time.
func NewQueryHandler(idx *WikiIndex, p QueryProvider) *QueryHandler {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}
	tmpl, err := template.New("answer_query").Funcs(funcMap).Parse(answerQueryTmpl)
	if err != nil {
		// Template is embedded and known-valid; panic at startup is correct.
		panic(fmt.Sprintf("wiki_query: failed to parse embedded template: %v", err))
	}
	return &QueryHandler{index: idx, provider: p, tmpl: tmpl}
}

// generalRefusalText is the exact text from answer_query.tmpl rule 7.
const generalRefusalText = "I don't have information about that. I can help with questions about people, companies, and activities in your workspace."

// Answer runs the full query pipeline and returns a QueryAnswer.
//
// It never returns a non-nil error for recoverable conditions (timeout, parse
// failure, out-of-scope query); those are signaled via QueryAnswer.Coverage
// and QueryAnswer.AnswerMarkdown. A non-nil error is returned only when the
// index itself is unavailable.
func (h *QueryHandler) Answer(ctx context.Context, req QueryRequest) (QueryAnswer, error) {
	start := time.Now()

	if req.TopK <= 0 {
		req.TopK = 20
	}
	if req.Timeout <= 0 {
		req.Timeout = 10 * time.Second
	}

	// Step 1: classify the query.
	class, conf := ClassifyQuery(req.Query)

	// Step 2: short-circuit out-of-scope queries without an LLM call.
	if class == QueryClassGeneral && conf >= 0.8 {
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: generalRefusalText,
			SourcesCited:   []int{},
			Sources:        []QuerySource{},
			Confidence:     conf,
			Coverage:       "none",
			LatencyMs:      time.Since(start).Milliseconds(),
		}, nil
	}

	// Step 3: retrieve top-K facts from the index.
	searchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	hits, err := h.index.Search(searchCtx, req.Query, req.TopK)
	if err != nil && ctx.Err() == nil {
		// Non-timeout index error — surface to caller.
		return QueryAnswer{}, fmt.Errorf("wiki_query: search: %w", err)
	}

	now := time.Now()
	sources := make([]QuerySource, 0, len(hits))
	for _, hit := range hits {
		fact, ok, fetchErr := h.index.GetFact(ctx, hit.FactID)
		if fetchErr != nil || !ok {
			// Best-effort: if one fact is missing, skip it rather than abort.
			src := QuerySource{
				Kind:       "fact",
				SlugOrID:   hit.FactID,
				Title:      hit.Entity,
				Excerpt:    hit.Snippet,
				Staleness:  0,
				SourcePath: "",
			}
			sources = append(sources, src)
			continue
		}
		src := hydrateFact(fact, now)
		if hit.Snippet != "" && src.Excerpt == "" {
			src.Excerpt = hit.Snippet
		}
		sources = append(sources, src)
	}

	// Step 4: check for timeout before invoking the LLM.
	if searchCtx.Err() != nil {
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: "The wiki query took too long to complete. The sources below were retrieved before the timeout.",
			SourcesCited:   []int{},
			Sources:        sources,
			Confidence:     0,
			Coverage:       "none",
			LatencyMs:      time.Since(start).Milliseconds(),
		}, nil
	}

	// Step 5: render the prompt template.
	//
	// Security: both the user-submitted Query and each Source.Excerpt reach
	// the LLM inside answer_query.tmpl. Source.Excerpt carries verbatim
	// artifact content through two hops (extraction → fact → source excerpt);
	// req.Query is attacker-controlled at hop zero (any authenticated user
	// can submit a hostile string). Escape both at the interpolation site so
	// an injection cannot hijack the answer prompt. See prompt_escape.go.
	promptSources := make([]QuerySource, len(sources))
	for i, src := range sources {
		src.Excerpt = EscapeForPromptBody(src.Excerpt)
		promptSources[i] = src
	}
	vars := templateVars{
		Query:      EscapeForPromptBody(req.Query),
		QueryClass: string(class),
		Now:        now.UTC().Format(time.RFC3339),
		Sources:    promptSources,
	}
	var promptBuf bytes.Buffer
	if err := h.tmpl.Execute(&promptBuf, vars); err != nil {
		log.Printf("wiki_query: template execute: %v", err)
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: fmt.Sprintf("Internal error rendering query prompt: %v", err),
			SourcesCited:   []int{},
			Sources:        sources,
			Confidence:     0,
			Coverage:       "none",
			LatencyMs:      time.Since(start).Milliseconds(),
		}, nil
	}

	// Step 6: invoke the provider.
	providerCtx, providerCancel := context.WithTimeout(ctx, req.Timeout)
	defer providerCancel()

	raw, provErr := h.provider.RunPrompt(providerCtx, "", promptBuf.String())
	latency := time.Since(start).Milliseconds()

	if providerCtx.Err() != nil {
		// Timeout mid-LLM call.
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: "The wiki query took too long to complete. The sources below were retrieved before the timeout.",
			SourcesCited:   []int{},
			Sources:        sources,
			Confidence:     0,
			Coverage:       "none",
			LatencyMs:      latency,
		}, nil
	}
	if provErr != nil {
		log.Printf("wiki_query: provider error: %v", provErr)
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: fmt.Sprintf("Failed to get an answer from the wiki: %v", provErr),
			SourcesCited:   []int{},
			Sources:        sources,
			Confidence:     0,
			Coverage:       "none",
			LatencyMs:      latency,
		}, nil
	}

	// Step 7: parse the JSON response.
	parsed, parseErr := parseProviderResponse(raw)
	if parseErr != nil {
		log.Printf("wiki_query: parse LLM response: %v", parseErr)
		return QueryAnswer{
			QueryClass:     class,
			AnswerMarkdown: fmt.Sprintf("The wiki returned an unparseable response: %v", parseErr),
			SourcesCited:   []int{},
			Sources:        sources,
			Confidence:     0,
			Coverage:       "none",
			LatencyMs:      latency,
		}, nil
	}

	// Step 8: validate sources_cited. The LLM may hallucinate citation
	// indices outside the provided range; those would silently vanish in
	// the renderer but could also enable confusion about coverage. Drop
	// invalid entries and record the drop in Notes so operators can see
	// the hallucination rate.
	cleanCites, dropped := filterValidCitations(parsed.SourcesCited, len(sources))
	notes := parsed.Notes
	if len(dropped) > 0 {
		msg := fmt.Sprintf("dropped invalid citations: %v", dropped)
		if notes != "" {
			notes = notes + " | " + msg
		} else {
			notes = msg
		}
	}

	return QueryAnswer{
		QueryClass:     QueryClass(parsed.QueryClass),
		AnswerMarkdown: parsed.AnswerMarkdown,
		SourcesCited:   cleanCites,
		Sources:        sources,
		Confidence:     parsed.Confidence,
		Coverage:       parsed.Coverage,
		Notes:          notes,
		LatencyMs:      latency,
	}, nil
}

// filterValidCitations enforces the §10.3 contract that sources_cited
// indices are 1-indexed and must be a subset of [1..sourceCount]. Any
// index outside that range is dropped and reported to the caller so the
// validation failure is observable.
func filterValidCitations(cites []int, sourceCount int) (clean []int, dropped []int) {
	if len(cites) == 0 {
		return []int{}, nil
	}
	clean = make([]int, 0, len(cites))
	for _, c := range cites {
		if c >= 1 && c <= sourceCount {
			clean = append(clean, c)
		} else {
			dropped = append(dropped, c)
		}
	}
	return clean, dropped
}

// hydrateFact converts a TypedFact into a QuerySource for the prompt template.
func hydrateFact(f TypedFact, now time.Time) QuerySource {
	kind := f.Kind
	if kind == "" {
		kind = "fact"
	}
	slug := f.EntitySlug
	if slug == "" {
		slug = f.ID
	}
	title := slug
	if f.Triplet != nil && f.Triplet.Subject != "" {
		title = f.Triplet.Subject
	}

	excerpt := strings.TrimSpace(f.Text)
	if len(excerpt) > 300 {
		excerpt = excerpt[:300] + "…"
	}

	var validFrom, validUntil string
	if !f.ValidFrom.IsZero() {
		validFrom = f.ValidFrom.UTC().Format("2006-01-02")
	}
	if f.ValidUntil != nil && !f.ValidUntil.IsZero() {
		validUntil = f.ValidUntil.UTC().Format("2006-01-02")
	}

	return QuerySource{
		Kind:       kind,
		SlugOrID:   slug,
		Title:      title,
		Excerpt:    excerpt,
		ValidFrom:  validFrom,
		ValidUntil: validUntil,
		Staleness:  Staleness(f, now),
		SourcePath: f.SourcePath,
	}
}

// parseProviderResponse extracts a JSON block from the raw LLM output.
// The template instructs the LLM to return ONLY a JSON object, but it may
// include a markdown code fence. We strip that and unmarshal.
func parseProviderResponse(raw string) (llmQueryAnswer, error) {
	raw = strings.TrimSpace(raw)
	// Strip optional markdown code fence.
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		// Drop opening ``` line and closing ``` line.
		end := len(lines) - 1
		for end > 0 && strings.TrimSpace(lines[end]) == "```" {
			end--
		}
		if len(lines) > 2 {
			raw = strings.Join(lines[1:end+1], "\n")
		}
	}

	// Find the outermost JSON object.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return llmQueryAnswer{}, fmt.Errorf("no JSON object found in response (len=%d)", len(raw))
	}
	jsonStr := raw[start : end+1]

	var out llmQueryAnswer
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return llmQueryAnswer{}, fmt.Errorf("unmarshal: %w (raw: %.120s)", err, jsonStr)
	}
	return out, nil
}

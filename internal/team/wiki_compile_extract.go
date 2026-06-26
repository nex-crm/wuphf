package team

// wiki_compile_extract.go — Phase 1 of the compile engine: read ONE immutable
// source record and ask the LLM for the durable, encyclopedic concepts a
// teammate would want a lasting wiki page about. The LLM call is a single
// prompted-JSON round trip through the PamRunner seam; parsing mirrors
// wiki_extractor.parseExtractionResponse (trim, strip fences, slice first
// '{' .. last '}') so the two paths share failure semantics.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// extractSystemPrompt is the verbatim Phase-1 system contract.
const extractSystemPrompt = "You are a knowledge-extraction engine for an internal encyclopedia compiled from a company's own work (tasks, decisions, chat, docs). Read ONE source document and identify the durable, encyclopedic concepts a teammate would want a lasting wiki page about — the people, companies, products, systems, decisions, methods, and recurring ideas. Ignore one-off chatter, pleasantries, and transient status. Respond with ONLY valid JSON, no prose, no code fences."

// extractUserInstructions is the verbatim JSON-shape contract appended after
// the source body in the Phase-1 user prompt.
const extractUserInstructions = `Return JSON exactly: {"concepts":[{"title":"...","slug":"kebab-case","kind":"concept|entity","summary":"one sentence","tags":["..."],"confidence":0.0-1.0}]}. Return 0 to 8 concepts. Use "entity" for a specific named thing (person/company/product/system/document); "concept" for an idea/method/decision/theme. Prefer durable knowledge over trivia. If the source has nothing encyclopedic, return {"concepts":[]}.`

// maxExtractedConcepts bounds the per-source concept count even if the LLM
// ignores the "0 to 8" instruction, so one runaway response cannot fan out
// into dozens of pages.
const maxExtractedConcepts = 8

// extractionResponse mirrors the JSON shape the extractor prompt asks for.
type extractionResponse struct {
	Concepts []ExtractedConcept `json:"concepts"`
}

// buildExtractionPrompt renders the (system, user) pair for one source. The
// untrusted source body flows into the user prompt through EscapeForPromptBody
// so a hostile source cannot smuggle instructions into the LLM context.
func buildExtractionPrompt(src SourceRecord) (system, user string) {
	var b strings.Builder
	b.WriteString("Source title: ")
	b.WriteString(src.Title)
	b.WriteString("\nSource kind: ")
	b.WriteString(string(src.Kind))
	b.WriteString("\n\nSource document:\n")
	b.WriteString(EscapeForPromptBody(src.Content))
	b.WriteString("\n\n")
	b.WriteString(extractUserInstructions)
	return extractSystemPrompt, b.String()
}

// parseExtraction robustly parses a Phase-1 LLM response into a clean slice of
// concepts. It trims whitespace, strips a ``` code fence if present, slices
// the outermost JSON object, then drops concepts with an empty title or slug,
// defaults an unknown/blank kind to "concept", normalizes slugs to kebab-case,
// and clamps confidence to [0,1]. At most maxExtractedConcepts are returned.
func parseExtraction(raw string) ([]ExtractedConcept, error) {
	jsonStr, err := sliceJSONObject(raw)
	if err != nil {
		return nil, err
	}
	var resp extractionResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal extraction response: %w (raw: %.120s)", err, jsonStr)
	}
	out := make([]ExtractedConcept, 0, len(resp.Concepts))
	for _, c := range resp.Concepts {
		title := strings.TrimSpace(c.Title)
		rawSlug := strings.TrimSpace(c.Slug)
		if title == "" || rawSlug == "" {
			continue
		}
		out = append(out, ExtractedConcept{
			Title:      title,
			Slug:       slugifySource(rawSlug),
			Kind:       conceptKind(c.Kind),
			Summary:    strings.TrimSpace(c.Summary),
			Tags:       dedupTags(c.Tags),
			Confidence: clampConfidence(c.Confidence),
		})
		if len(out) >= maxExtractedConcepts {
			break
		}
	}
	return out, nil
}

// extractConcepts runs the Phase-1 LLM call for one source and parses the
// result. A runner error or unparseable response is returned to the caller so
// the orchestrator can record it in CompileResult.Errors without aborting.
func extractConcepts(ctx context.Context, runner PamRunner, src SourceRecord) ([]ExtractedConcept, error) {
	system, user := buildExtractionPrompt(src)
	raw, err := runner.Run(ctx, system, user)
	if err != nil {
		return nil, fmt.Errorf("extract concepts from %s: %w", src.ID, err)
	}
	concepts, err := parseExtraction(raw)
	if err != nil {
		return nil, fmt.Errorf("parse concepts from %s: %w", src.ID, err)
	}
	return concepts, nil
}

// sliceJSONObject trims, strips a leading/trailing ``` fence, and returns the
// substring from the first '{' to the last '}'. Mirrors
// parseExtractionResponse in wiki_extractor.go.
func sliceJSONObject(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		end := len(lines) - 1
		for end > 0 && strings.TrimSpace(lines[end]) == "```" {
			end--
		}
		if len(lines) > 2 {
			raw = strings.Join(lines[1:end+1], "\n")
		}
	}
	start := strings.Index(raw, "{")
	last := strings.LastIndex(raw, "}")
	if start < 0 || last <= start {
		return "", fmt.Errorf("no JSON object in response (len=%d)", len(raw))
	}
	return raw[start : last+1], nil
}

// normalizeKind lowercases and trims a kind string for comparison.
func normalizeKind(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// clampConfidence bounds a confidence score to [0,1].
func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// dedupTags trims, drops blanks, and removes duplicates while preserving
// first-seen order so merged tag unions stay deterministic.
func dedupTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

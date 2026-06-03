package team

// skill_validation_gate.go implements the held-out validation gate for Stage B
// skill synthesis (issue #1004). Before accepting any NEW or ENHANCE proposal,
// the gate:
//
//  1. Loads representative task fixtures from the workspace wiki under
//     team/skills/.fixtures/<slug>.md.
//  2. Asks an LLM-as-judge to compare the candidate body against the baseline
//     (empty for new skills, existing body for enhance) on each fixture.
//  3. Accepts only when the candidate strictly improves: zero regressions and
//     at least one win across all fixtures.
//  4. Degrades gracefully when no fixture file exists for the slug — the common
//     case before teams have authored their fixture sets.
//
// The gate is disabled entirely via WUPHF_SKILL_VALIDATION_GATE_DISABLED=true.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nex-crm/wuphf/internal/config"
)

// skillValidationGate is the interface SkillSynthesizer calls before accepting
// any NEW or ENHANCE proposal. Defined where consumed per the "accept interfaces,
// return structs" convention already used by stageBLLMProvider.
//
// Validate returns (hasFixtures bool, err error):
//   - (false, nil): no fixture file found; gate skipped. Caller increments
//     ValidationGateNoFixtures.
//   - (true, nil): gate passed; candidate strictly improves on fixtures.
//   - (true, err): gate rejected; err describes why.
type skillValidationGate interface {
	Validate(ctx context.Context, slug, candidateBody, baselineBody, wikiRoot string) (bool, error)
}

// fixtureVerdict is the LLM judge's per-fixture decision.
type fixtureVerdict string

const (
	verdictBetter     fixtureVerdict = "better"
	verdictEquivalent fixtureVerdict = "equivalent"
	verdictWorse      fixtureVerdict = "worse"
)

// loadFixturesForSlug reads <wikiRoot>/team/skills/.fixtures/<slug>.md and
// returns individual fixture strings split on "\n---\n". Returns nil, nil when
// the file does not exist (graceful degradation — callers treat this as "skip
// gate"). Returns a non-nil error for unexpected read failures other than ENOENT.
//
// The slug is checked for path separators as defense-in-depth; slugs are already
// validated to ^[a-z0-9][a-z0-9-]*$ by the synth provider upstream.
func loadFixturesForSlug(wikiRoot, slug string) ([]string, error) {
	if wikiRoot == "" || slug == "" {
		return nil, nil
	}
	if strings.ContainsRune(slug, filepath.Separator) {
		return nil, nil
	}
	fixturePath := filepath.Join(wikiRoot, "team", "skills", ".fixtures", slug+".md")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill_validation_gate: read fixture for %q: %w", slug, err)
	}
	return splitFixtures(string(raw)), nil
}

// splitFixtures splits raw fixture content on "\n---\n", trims each section,
// and discards empty sections. A file containing only whitespace returns nil.
func splitFixtures(raw string) []string {
	parts := strings.Split(raw, "\n---\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validationCacheKey returns a deterministic SHA-256 cache key for a
// (fixture, candidateBody, baselineBody) triple. NUL bytes separate the
// fields so a body containing the separator string cannot forge a collision.
func validationCacheKey(fixture, candidateBody, baselineBody string) string {
	h := sha256.New()
	h.Write([]byte(fixture))
	h.Write([]byte{0})
	h.Write([]byte(candidateBody))
	h.Write([]byte{0})
	h.Write([]byte(baselineBody))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// defaultSkillValidationGate is the production implementation of
// skillValidationGate. It loads fixtures from the wiki, calls an LLM-as-judge
// (Anthropic preferred, OpenAI fallback — same credential resolution as the
// synth provider), and applies the strict-improvement rule before returning.
type defaultSkillValidationGate struct {
	httpClient *http.Client

	cacheMu sync.Mutex
	cache   map[string]fixtureVerdict // keyed by validationCacheKey(...)

	missingKeyWarned sync.Once
}

// newDefaultSkillValidationGate constructs a gate with its own HTTP client.
// The timeout mirrors the synth provider via stageBSynthTimeoutFromEnv.
func newDefaultSkillValidationGate() *defaultSkillValidationGate {
	return &defaultSkillValidationGate{
		httpClient: &http.Client{Timeout: stageBSynthTimeoutFromEnv()},
		cache:      make(map[string]fixtureVerdict),
	}
}

// Validate implements skillValidationGate.
//
//  1. Load fixtures for slug from wikiRoot. Return (false, nil) when none exist.
//  2. Judge each fixture (cache-first, then LLM). A judge failure degrades to
//     verdictEquivalent so a single failing call does not block the gate.
//  3. Count wins (better) and losses (worse). Ties are neutral.
//  4. Reject if losses > 0 or wins == 0.
func (g *defaultSkillValidationGate) Validate(
	ctx context.Context,
	slug, candidateBody, baselineBody, wikiRoot string,
) (bool, error) {
	fixtures, err := loadFixturesForSlug(wikiRoot, slug)
	if err != nil {
		// Unexpected read error: log and skip the gate rather than blocking synthesis.
		slog.Warn("skill_validation_gate: fixture load error; gate skipped",
			"slug", slug, "err", err)
		return false, nil
	}
	if len(fixtures) == 0 {
		slog.Debug("skill_validation_gate: no fixtures for slug; gate skipped",
			"slug", slug)
		return false, nil
	}

	wins := 0
	losses := 0
	for _, fixture := range fixtures {
		switch g.judgeFixture(ctx, fixture, candidateBody, baselineBody) {
		case verdictBetter:
			wins++
		case verdictWorse:
			losses++
		}
	}

	if losses > 0 {
		return true, fmt.Errorf(
			"skill_validation_gate: candidate regresses on %d/%d fixture(s) for %q (strict improvement requires zero regressions)",
			losses, len(fixtures), slug,
		)
	}
	if wins == 0 {
		return true, fmt.Errorf(
			"skill_validation_gate: candidate shows no improvement on any of %d fixture(s) for %q (at least one improvement required)",
			len(fixtures), slug,
		)
	}
	slog.Debug("skill_validation_gate: candidate passed",
		"slug", slug, "wins", wins, "total_fixtures", len(fixtures))
	return true, nil
}

// judgeFixture returns the verdict for one (fixture, candidate, baseline) triple.
// The in-memory cache is checked first; on a miss the LLM is called and the
// result stored. If the LLM call or response parsing fails, verdictEquivalent
// is returned so a single failing judge does not block the entire gate.
func (g *defaultSkillValidationGate) judgeFixture(
	ctx context.Context,
	fixture, candidateBody, baselineBody string,
) fixtureVerdict {
	key := validationCacheKey(fixture, candidateBody, baselineBody)

	g.cacheMu.Lock()
	if v, ok := g.cache[key]; ok {
		g.cacheMu.Unlock()
		return v
	}
	g.cacheMu.Unlock()

	v := g.callJudge(ctx, fixture, candidateBody, baselineBody)

	g.cacheMu.Lock()
	g.cache[key] = v
	g.cacheMu.Unlock()
	return v
}

// callJudge calls the LLM-as-judge and returns the parsed verdict. It follows
// the identical Anthropic → OpenAI fallback as callLLM in skill_synth_provider.go.
// On any error, it returns verdictEquivalent (degraded grading, not a gate failure).
func (g *defaultSkillValidationGate) callJudge(
	ctx context.Context,
	fixture, candidateBody, baselineBody string,
) fixtureVerdict {
	anthroKey := strings.TrimSpace(config.ResolveAnthropicAPIKey())
	openaiKey := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	if anthroKey == "" && openaiKey == "" {
		g.missingKeyWarned.Do(func() {
			slog.Warn("skill_validation_gate: no API key configured; judge disabled, fixtures treated as equivalent")
		})
		return verdictEquivalent
	}

	systemPrompt := validationJudgeSystemPrompt()
	userPrompt := validationJudgeUserPrompt(fixture, candidateBody, baselineBody)

	var raw string
	var callErr error
	if anthroKey != "" {
		raw, callErr = callAnthropicJudge(ctx, g.httpClient, anthroKey, systemPrompt, userPrompt)
	} else {
		raw, callErr = callOpenAIJudge(ctx, g.httpClient, openaiKey, systemPrompt, userPrompt)
	}
	if callErr != nil {
		slog.Debug("skill_validation_gate: judge call failed; treating as equivalent",
			"err", callErr)
		return verdictEquivalent
	}

	verdict, parseErr := parseJudgeResponse(raw)
	if parseErr != nil {
		slog.Debug("skill_validation_gate: judge parse failed; treating as equivalent",
			"err", parseErr,
			"raw", truncateForPrompt(raw, 120),
		)
		return verdictEquivalent
	}
	return verdict
}

// skillValidationJudgeMaxTokens is the token budget for judge responses.
// Small because the response is a single JSON object, not a full skill body.
const skillValidationJudgeMaxTokens = 256

// callAnthropicJudge posts to /v1/messages and returns the text content.
// Mirrors callAnthropic in skill_synth_provider.go but uses a smaller token
// budget appropriate for the compact JSON judge response.
func callAnthropicJudge(ctx context.Context, client *http.Client, key, systemPrompt, userPrompt string) (string, error) {
	const endpoint = "https://api.anthropic.com/v1/messages"
	model := resolveStageBSynthModel(stageBDefaultAnthropicModel)

	payload := map[string]any{
		"model":      model,
		"max_tokens": skillValidationJudgeMaxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("anthropic_judge: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic_judge: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic_judge: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("anthropic_judge: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic_judge: status %d: %s",
			resp.StatusCode, truncateForPrompt(string(respBody), 240))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("anthropic_judge: decode response: %w", err)
	}
	var out strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			out.WriteString(c.Text)
		}
	}
	return out.String(), nil
}

// callOpenAIJudge posts to /v1/chat/completions and returns the assistant content.
// Mirrors callOpenAI in skill_synth_provider.go but uses a smaller token budget
// appropriate for the compact JSON judge response.
func callOpenAIJudge(ctx context.Context, client *http.Client, key, systemPrompt, userPrompt string) (string, error) {
	const endpoint = "https://api.openai.com/v1/chat/completions"
	model := resolveStageBSynthModel(stageBDefaultOpenAIModel)

	payload := map[string]any{
		"model":      model,
		"max_tokens": skillValidationJudgeMaxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("openai_judge: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai_judge: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai_judge: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("openai_judge: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai_judge: status %d: %s",
			resp.StatusCode, truncateForPrompt(string(respBody), 240))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("openai_judge: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai_judge: empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// validationJudgeSystemPrompt returns the system prompt for the LLM judge.
func validationJudgeSystemPrompt() string {
	return `You are an impartial evaluator comparing two AI agent skill documents.
Your job: decide whether a CANDIDATE SKILL helps an agent complete a TASK better than a BASELINE SKILL.

Rules:
- "better": the candidate clearly provides more actionable, precise, or correct guidance for this task.
- "equivalent": both are equally useful (or equally unhelpful) for this task.
- "worse": the baseline is more useful than the candidate for this task.
- If the baseline is empty, compare the candidate against having no skill at all.

Respond ONLY with valid JSON: {"verdict":"better"|"equivalent"|"worse","reasoning":"<one sentence>"}.
Do not include any other text.`
}

// validationJudgeUserPrompt assembles the user turn for the LLM judge.
func validationJudgeUserPrompt(taskDescription, candidateBody, baselineBody string) string {
	var b strings.Builder
	b.WriteString("TASK:\n")
	b.WriteString(strings.TrimSpace(taskDescription))
	b.WriteString("\n\nBASELINE SKILL:\n")
	if strings.TrimSpace(baselineBody) == "" {
		b.WriteString("(none — this is a new skill with no existing baseline)\n")
	} else {
		b.WriteString(strings.TrimSpace(baselineBody))
		b.WriteString("\n")
	}
	b.WriteString("\nCANDIDATE SKILL:\n")
	b.WriteString(strings.TrimSpace(candidateBody))
	b.WriteString("\n\nRespond with JSON only.")
	return b.String()
}

// parseJudgeResponse extracts the verdict from the judge's JSON response.
// Tolerates ```json fences and leading prose via the package-level
// stripJSONNoise helper already used by parseStageBSynthResponse.
func parseJudgeResponse(raw string) (fixtureVerdict, error) {
	cleaned := stripJSONNoise(raw)
	if cleaned == "" {
		return verdictEquivalent, fmt.Errorf("skill_validation_gate: empty judge response")
	}
	var parsed struct {
		Verdict   string `json:"verdict"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return verdictEquivalent, fmt.Errorf("skill_validation_gate: decode judge json: %w", err)
	}
	switch fixtureVerdict(strings.TrimSpace(parsed.Verdict)) {
	case verdictBetter:
		return verdictBetter, nil
	case verdictWorse:
		return verdictWorse, nil
	case verdictEquivalent:
		return verdictEquivalent, nil
	default:
		return verdictEquivalent, fmt.Errorf("skill_validation_gate: unknown verdict %q", parsed.Verdict)
	}
}

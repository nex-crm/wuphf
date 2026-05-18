package team

// broker_ceo_voice_test.go — Phase 4 CEO voice regression corpus.
//
// TestCEOVoiceRegressionCorpus validates that parseCEODraftResponse correctly
// handles the LLM's raw output AND that the system prompt rules are enforced
// via corpus assertions. Each example is a hand-written canned response
// representing what a compliant model should return, exercising the spec rules:
//
//   1. Output does NOT start with "Welcome", "I'm your", "I am your", "Hello",
//      "Hi", or any greeting.
//   2. Output does NOT introduce the speaker ("I am the CEO", "As your CEO").
//   3. Total word count of all sections combined is below 200 (low word count
//      rule — CEO is terse by design).
//   4. The "goal" field is a single declarative sentence (no question marks,
//      no "I will", no preamble).
//   5. parseCEODraftResponse handles markdown code-fence wrapping that real
//      models sometimes produce.
//
// These tests run in CI with no LLM key required (pure fixture corpus).
//
// For a nightly live-provider regression against a real model, see the
// build-tag-gated companion in broker_ceo_voice_real_test.go (if present),
// which is only compiled when WUPHF_VOICE_LIVE_TEST=1 is passed via
// `go test -tags=voice_live`.
//
// Deferred: asserting that the real model's output passes these constraints
// is done in the live test file (Phase 6 nightly gate).

import (
	"fmt"
	"strings"
	"testing"
)

// ceoVoiceFixture is one example in the corpus. The LLMRaw field is what
// the model returns verbatim; the parsed fields are what parseCEODraftResponse
// should extract from it.
type ceoVoiceFixture struct {
	name       string
	userPrompt string
	// LLMRaw is the canned model output, possibly wrapped in code fences.
	LLMRaw string
	// Expected parsed fields — asserted after parseCEODraftResponse.
	wantGoal       string
	wantContext    string
	wantApproach   string
	wantAcceptance string
}

// ceoVoiceCorpus is the 5-example hand-written regression corpus. Each example
// mirrors a real user request shape from the ICP. The CEO responses deliberately
// satisfy the voice rules so we can assert compliance rather than just
// parse correctness.
var ceoVoiceCorpus = []ceoVoiceFixture{
	{
		name:       "stripe-webhooks",
		userPrompt: "Get Stripe webhooks working so payments update order status automatically",
		LLMRaw: `{
  "goal": "Implement Stripe webhook handler to automatically update order status on payment events.",
  "context": "The payment flow currently requires manual order status updates after Stripe events. Stripe sends signed webhook events to a registered endpoint. The app must verify the signature, parse the event type, and write the order status to the database atomically.",
  "approach": "- Register a POST /webhooks/stripe endpoint in the API router\n- Validate the Stripe-Signature header using the webhook signing secret\n- Handle payment_intent.succeeded and payment_intent.payment_failed events\n- Update orders table status column atomically inside a transaction\n- Return 200 quickly to prevent Stripe retries; offload DB writes to a goroutine queue if needed",
  "acceptance": "- Stripe dashboard shows successful webhook deliveries with 200 responses\n- Order status changes to 'paid' within 2 seconds of payment_intent.succeeded\n- Order status changes to 'failed' on payment_intent.payment_failed\n- Invalid or replayed webhook signatures return 400 without processing\n- Integration test covers the full signed-event → DB write path"
}`,
		wantGoal: "Implement Stripe webhook handler to automatically update order status on payment events.",
	},
	{
		name:       "ci-pipeline-setup",
		userPrompt: "Set up a CI pipeline that runs tests and blocks merges when they fail",
		LLMRaw:     "```json\n{\n  \"goal\": \"Configure GitHub Actions CI pipeline to run the full test suite on every PR and block merges on failure.\",\n  \"context\": \"Merges to main are currently unguarded. Broken commits block the team for hours. A branch protection rule plus a status check gate will enforce green-tests-only merges. The pipeline needs to install dependencies, run the test suite, and report results back to GitHub.\",\n  \"approach\": \"- Add .github/workflows/ci.yml with trigger on: [push, pull_request]\\n- Install dependencies using the repo's lockfile (bun install --frozen-lockfile)\\n- Run unit tests (bun run test) and Go tests (go test -race ./...)\\n- Fail the workflow on non-zero exit\\n- Enable branch protection on main: require status check 'ci / test'\",\n  \"acceptance\": \"- PR with a failing test shows a red check and cannot be merged\\n- PR with all passing tests shows a green check\\n- Workflow completes in under 3 minutes on a clean cache\\n- go vet reports zero issues in the workflow run\\n- Branch protection rule is active on main branch\"\n}\n```",
		wantGoal:   "Configure GitHub Actions CI pipeline to run the full test suite on every PR and block merges on failure.",
	},
	{
		name:       "auth-token-refresh",
		userPrompt: "Add automatic JWT refresh so users don't get logged out mid-session",
		LLMRaw: `{
  "goal": "Implement silent JWT access-token refresh using a short-lived access token plus a long-lived refresh token.",
  "context": "Sessions currently expire when the access token (15-minute TTL) lapses, forcing users to re-authenticate. A refresh token stored in an HttpOnly cookie can silently renew the access token. The API needs a /auth/refresh endpoint and the frontend needs an Axios interceptor to retry 401s after a successful refresh.",
  "approach": "- Add POST /auth/refresh endpoint: validate refresh token, issue new access token\n- Store refresh token in HttpOnly, Secure, SameSite=Strict cookie (not localStorage)\n- Add Axios response interceptor: on 401, call /auth/refresh, retry original request once\n- Implement token rotation: each /auth/refresh call issues a new refresh token and invalidates the old one\n- Add refresh token revocation on explicit logout",
  "acceptance": "- A user with a valid refresh token is transparently re-authenticated after access token expiry\n- A stolen access token cannot be used after its 15-minute TTL\n- Refresh token replay after rotation returns 401\n- Explicit logout invalidates the refresh token immediately\n- Browser network tab shows a single silent /auth/refresh request before retrying the failed call"
}`,
		wantGoal: "Implement silent JWT access-token refresh using a short-lived access token plus a long-lived refresh token.",
	},
	{
		name:       "onboarding-email",
		userPrompt: "Send a welcome email series to new users over the first 7 days",
		LLMRaw: `{
  "goal": "Build a 3-email drip sequence triggered on user signup, delivered over 7 days via a transactional email provider.",
  "context": "New users currently receive no post-signup outreach. A structured welcome sequence increases activation. Email 1 fires immediately on signup, Email 2 fires on day 3, Email 3 fires on day 7. Sequences must be idempotent so retries don't duplicate sends.",
  "approach": "- Create an email_sequences table: user_id, sequence_name, step, scheduled_at, sent_at\n- On signup, insert 3 rows for the welcome sequence with scheduled_at timestamps\n- Add a cron job (every 5 minutes) that queries unsent rows with scheduled_at <= now\n- Send via Resend (or configured SMTP) and mark sent_at on success\n- Idempotency: use a unique constraint on (user_id, sequence_name, step)",
  "acceptance": "- Email 1 arrives within 60 seconds of signup\n- Email 2 arrives within 5 minutes of the day-3 mark\n- Email 3 arrives within 5 minutes of the day-7 mark\n- Cron retries do not send duplicate emails (idempotency key enforced at DB level)\n- Email sequence is skipped if user deletes their account before step fires"
}`,
		wantGoal: "Build a 3-email drip sequence triggered on user signup, delivered over 7 days via a transactional email provider.",
	},
	{
		name:       "feature-flags",
		userPrompt: "Add feature flags so we can roll out new features to a percentage of users",
		LLMRaw: `{
  "goal": "Implement a lightweight feature flag system backed by a database table, supporting per-user and percentage-based rollouts.",
  "context": "The codebase currently ships features to 100% of users with no rollback mechanism. A flag table with rollout_pct and user_overrides columns gives gradual rollouts and instant kill-switches. No external service dependency needed for v1.",
  "approach": "- Create feature_flags table: name, rollout_pct (0-100), enabled, user_overrides (jsonb)\n- Expose isEnabled(flagName, userID) helper that hashes userID mod 100 against rollout_pct\n- Add admin endpoint PATCH /flags/:name for rollout_pct and enabled changes\n- Cache flag rows in-memory with a 30-second TTL to avoid per-request DB hits\n- Instrument flag evaluations with a counter metric for observability",
  "acceptance": "- isEnabled returns true for exactly rollout_pct % of distinct user IDs (verified by unit test over 10k IDs)\n- Setting enabled=false kills the feature for 100% of users within 30 seconds\n- User override in user_overrides takes priority over rollout_pct\n- Admin endpoint is auth-gated (admin role required)\n- Cache miss falls back to DB; DB failure falls back to false (safe default)"
}`,
		wantGoal: "Implement a lightweight feature flag system backed by a database table, supporting per-user and percentage-based rollouts.",
	},
}

// TestCEOVoiceRegressionCorpus is the deterministic CI gate for CEO voice rules.
// It does not make any LLM calls — it exercises parseCEODraftResponse against
// the canned corpus and then asserts the voice invariants on the parsed output.
func TestCEOVoiceRegressionCorpus(t *testing.T) {
	t.Parallel()
	for _, fix := range ceoVoiceCorpus {
		fix := fix
		t.Run(fix.name, func(t *testing.T) {
			t.Parallel()
			resp, err := parseCEODraftResponse(fix.LLMRaw)
			if err != nil {
				t.Fatalf("parseCEODraftResponse(%q): %v", fix.name, err)
			}

			// Parsed goal must match expected (spot-check correctness of parser).
			if fix.wantGoal != "" && resp.Goal != fix.wantGoal {
				t.Errorf("goal mismatch:\n got:  %q\n want: %q", resp.Goal, fix.wantGoal)
			}

			// Assert voice rules on all four fields.
			for _, section := range []struct {
				label string
				text  string
			}{
				{"goal", resp.Goal},
				{"context", resp.Context},
				{"approach", resp.Approach},
				{"acceptance", resp.Acceptance},
			} {
				assertCEOVoiceRules(t, fix.name, section.label, section.text)
			}

			// Low word count: all sections combined must be under 200 words.
			combined := strings.Join([]string{resp.Goal, resp.Context, resp.Approach, resp.Acceptance}, " ")
			wordCount := len(strings.Fields(combined))
			if wordCount > 200 {
				t.Errorf("combined word count %d > 200 (CEO voice: low word count)", wordCount)
			}

			// Goal section specifically: single-sentence, declarative — must not
			// end with a question mark.
			if strings.TrimSpace(resp.Goal) == "" {
				t.Error("goal is empty")
			}
			if strings.HasSuffix(strings.TrimSpace(resp.Goal), "?") {
				t.Errorf("goal ends with '?' — must be a declarative statement, not a question")
			}
		})
	}
}

// assertCEOVoiceRules checks a single section text against the CEO voice rules.
// These are the load-bearing constraints from spec §"CEO voice canon":
//
//  1. Does not start with a greeting or self-introduction.
//  2. Does not contain "I'm your" / "I am your".
//  3. Does not contain "Welcome".
//  4. Does not start with "Hello" or "Hi".
//  5. Does not say "I am the CEO" / "As your CEO" (no role self-introduction).
func assertCEOVoiceRules(t *testing.T, fixtureName, sectionLabel, text string) {
	t.Helper()
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	lower := strings.ToLower(trimmed)

	// Rule 1a: no greeting openers.
	forbiddenPrefixes := []string{
		"welcome",
		"hello",
		"hi ",
		"hi,",
		"hey ",
		"hey,",
		"greetings",
	}
	for _, prefix := range forbiddenPrefixes {
		if strings.HasPrefix(lower, prefix) {
			t.Errorf("[%s/%s] starts with forbidden greeting prefix %q: %q", fixtureName, sectionLabel, prefix, trimmed[:clampInt(80, len(trimmed))])
		}
	}

	// Rule 1b: no self-introduction phrases anywhere in the text.
	forbiddenPhrases := []string{
		"i'm your",
		"i am your",
		"i'm the ceo",
		"i am the ceo",
		"as your ceo",
		"as the ceo",
		"welcome to",
		"welcome!",
		"welcome,",
	}
	for _, phrase := range forbiddenPhrases {
		if strings.Contains(lower, phrase) {
			t.Errorf("[%s/%s] contains forbidden phrase %q", fixtureName, sectionLabel, phrase)
		}
	}
}

// TestParseCEODraftResponseFenceStripping verifies that the code-fence stripper
// handles the common ```json ... ``` wrapper that real models produce.
func TestParseCEODraftResponseFenceStripping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "plain-json",
			raw:  `{"goal":"g","context":"c","approach":"a","acceptance":"ac"}`,
		},
		{
			name: "json-fence",
			raw:  "```json\n{\"goal\":\"g\",\"context\":\"c\",\"approach\":\"a\",\"acceptance\":\"ac\"}\n```",
		},
		{
			name: "bare-fence",
			raw:  "```\n{\"goal\":\"g\",\"context\":\"c\",\"approach\":\"a\",\"acceptance\":\"ac\"}\n```",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := parseCEODraftResponse(tc.raw)
			if err != nil {
				t.Fatalf("parseCEODraftResponse(%q): %v", tc.name, err)
			}
			if resp.Goal != "g" {
				t.Errorf("goal = %q, want %q", resp.Goal, "g")
			}
			if resp.Context != "c" {
				t.Errorf("context = %q, want %q", resp.Context, "c")
			}
		})
	}
}

// TestParseCEODraftResponseRejectsInvalidJSON verifies that malformed LLM
// output returns a descriptive error rather than silently producing zero values.
func TestParseCEODraftResponseRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "plain-text", raw: "Here is your issue draft: goal is to build a webhook."},
		{name: "truncated", raw: `{"goal":"half`},
		{name: "array-not-object", raw: `["goal","context","approach","acceptance"]`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseCEODraftResponse(tc.raw)
			if err == nil {
				t.Errorf("parseCEODraftResponse(%q): expected error for invalid JSON, got nil", tc.name)
			}
		})
	}
}

// TestCEODraftIdempotency verifies that draftIssueLocked is idempotent:
// if IssueDraftSpec.DraftedAt is already set, the function returns nil and
// does NOT call the LLM (the task spec is unchanged).
func TestCEODraftIdempotency(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tasks = append(b.tasks, teamTask{
		ID:      "task-idempotency",
		Channel: "general",
		Title:   "Idempotency test task",
		Owner:   "eng",
	})
	task := &b.tasks[len(b.tasks)-1]
	if err := b.applyLifecycleStateLocked(task, LifecycleStateDrafting); err != nil {
		t.Fatalf("applyLifecycleStateLocked: %v", err)
	}

	// Pre-set DraftedAt to simulate a previous draft.
	task.IssueDraftSpec = &IssueDraftSpec{
		Goal:      "Pre-existing goal",
		DraftedAt: "2026-01-01T00:00:00Z",
	}
	originalSpec := *task.IssueDraftSpec

	// Call draftIssueLocked — should return nil immediately (idempotent).
	// The real LLM call is skipped because DraftedAt is set, so no API key needed.
	err := b.draftIssueLocked(t.Context(), "task-idempotency", "rebuild everything", nil)
	if err != nil {
		t.Fatalf("draftIssueLocked (idempotent path): %v", err)
	}

	// Spec must be unchanged.
	if task.IssueDraftSpec.Goal != originalSpec.Goal {
		t.Errorf("goal changed: got %q, want %q", task.IssueDraftSpec.Goal, originalSpec.Goal)
	}
	if task.IssueDraftSpec.DraftedAt != originalSpec.DraftedAt {
		t.Errorf("drafted_at changed: got %q, want %q", task.IssueDraftSpec.DraftedAt, originalSpec.DraftedAt)
	}
}

// TestCEODraftRejectsNonDraftingState verifies that draftIssueLocked refuses
// to call the LLM for tasks that are not in Drafting state.
func TestCEODraftRejectsNonDraftingState(t *testing.T) {
	t.Parallel()
	nonDraftingStates := []LifecycleState{
		LifecycleStateRunning,
		LifecycleStateApproved,
		LifecycleStateReview,
		LifecycleStateIntake,
		LifecycleStateChangesRequested,
	}
	for _, state := range nonDraftingStates {
		state := state
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			b.mu.Lock()
			defer b.mu.Unlock()

			b.tasks = append(b.tasks, teamTask{
				ID:      fmt.Sprintf("task-nondraft-%s", state),
				Channel: "general",
				Title:   "Non-draft state test task",
				Owner:   "eng",
			})
			task := &b.tasks[len(b.tasks)-1]
			if err := b.applyLifecycleStateLocked(task, state); err != nil {
				t.Fatalf("applyLifecycleStateLocked(%s): %v", state, err)
			}

			err := b.draftIssueLocked(t.Context(), task.ID, "do something", nil)
			if err == nil {
				t.Errorf("state %q: expected error from draftIssueLocked, got nil", state)
			}
		})
	}
}

// clampInt returns the smaller of a and b for safe string truncation in
// assertCEOVoiceRules. Named distinctly to avoid redeclaring the min helper
// already defined in wiki_query_test.go.
func clampInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

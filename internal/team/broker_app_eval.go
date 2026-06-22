package team

// broker_app_eval.go — post-build app ACCEPTANCE gate.
//
// "The build passed" is not "the app does what was asked". The deterministic
// publish gate (tsc + vite + the stack/theme/card-pile guards) proves the app
// compiles and conforms; it cannot tell whether the app actually satisfies the
// human's brief. This gate closes that gap: when an App Builder build task
// reaches done, the BROKER (not an agent) runs two checks against the original
// brief — deterministic structure checks plus a bounded one-shot LLM acceptance
// judge — and, if the app falls short, REOPENS the task with the specific gaps so
// the App Builder fixes + republishes. Bounded retries stop an endless loop; once
// exhausted the gate flags the task for a human instead of silently declaring it
// done.
//
// Mirrors broker_workflow_detect.go: broker-actuated, gated on
// workflowDetectionEnabled (off in the unit suite), one bounded judge call that
// only returns structured JSON and never calls a tool.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	appAcceptanceTimeout        = 60 * time.Second
	appAcceptanceMaxRetries     = 2 // auto-fix attempts before flagging for a human
	appAcceptanceMaxBriefBytes  = 8 << 10
	appAcceptanceMaxCapsBytes   = 8 << 10
	appAcceptanceMinBundleBytes = 2000
	appAcceptanceMaxGaps        = 12
	appAcceptanceFailKind       = "app_acceptance_fail"
	appAcceptancePassKind       = "app_acceptance_pass"
	appAcceptanceHaltKind       = "app_acceptance_halt"
)

// queueAppAcceptanceEval fires the post-done acceptance gate for an App Builder
// build task. No-op unless detection is enabled (the production web path turns it
// on), so the unit suite — which completes many tasks — never makes a live judge
// call. The owner filter inside the worker keeps it to App Builder build tasks.
func (b *Broker) queueAppAcceptanceEval(taskID string) {
	if b == nil || !b.workflowDetectionEnabled {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	go func() {
		defer recoverPanicTo("appAcceptanceEval", "task="+taskID)
		b.evaluateAppAcceptanceForTask(taskID)
	}()
}

// appAcceptanceDecision is the strict shape the judge LLM must return.
type appAcceptanceDecision struct {
	Meets   bool     `json:"meets"`
	Summary string   `json:"summary"`
	Gaps    []string `json:"gaps"`
}

// appAcceptanceAction is what the broker does with a judge verdict.
type appAcceptanceAction int

const (
	appAcceptanceActionPass appAcceptanceAction = iota
	appAcceptanceActionReopen
	appAcceptanceActionHalt
)

func (b *Broker) evaluateAppAcceptanceForTask(taskID string) {
	b.mu.Lock()
	task := b.taskByIDLocked(taskID)
	if task == nil || task.System ||
		!strings.EqualFold(strings.TrimSpace(task.Owner), appBuilderSlug) {
		b.mu.Unlock()
		return
	}
	t := *task
	channel := normalizeChannelSlug(t.Channel)
	priorFails := b.countAppAcceptanceFailsLocked(channel)
	b.mu.Unlock()

	brief := strings.TrimSpace(t.Details)
	if channel == "" || brief == "" {
		return // nothing to grade against
	}

	app, ok := b.appForEditChannel(channel)
	if !ok {
		return // no published app bound to this task → nothing to grade
	}

	// Deterministic checks first — cheap and never wrong.
	detGaps := b.deterministicAppGaps(app)

	caps := ""
	if files, err := b.appStore().Source(app.ID); err == nil {
		caps = appEvalCapBytes(
			renderAppCapabilities(introspectAppSource(files)),
			appAcceptanceMaxCapsBytes,
		)
	}

	system, prompt := buildAppAcceptancePrompt(app, caps, brief, detGaps)
	ctx, cancel := context.WithTimeout(context.Background(), appAcceptanceTimeout)
	defer cancel()
	out, err := currentAppsLLMCompleter()(ctx, system, prompt)
	if err != nil {
		log.Printf("app acceptance %s: judge unavailable: %v", taskID, err)
		return // an unavailable provider must not block a delivered task
	}
	decision, ok := parseAppAcceptanceDecision(out)
	if !ok {
		log.Printf("app acceptance %s: could not parse judge output", taskID)
		return
	}

	action, gaps := decideAppAcceptance(decision, detGaps, priorFails)
	switch action {
	case appAcceptanceActionPass:
		b.postAppAcceptanceResult(channel, appAcceptancePassKind,
			fmt.Sprintf("✅ Acceptance check passed — %q meets the brief. %s",
				app.Name, strings.TrimSpace(decision.Summary)))
		log.Printf("app acceptance %s: PASS", taskID)
	case appAcceptanceActionHalt:
		b.postAppAcceptanceResult(channel, appAcceptanceHaltKind,
			fmt.Sprintf("⚠️ Acceptance check still failing after %d auto-fix attempts — leaving %q for a human to review:\n%s",
				appAcceptanceMaxRetries, app.Name, renderGaps(gaps)))
		log.Printf("app acceptance %s: HALT after %d retries", taskID, priorFails)
	case appAcceptanceActionReopen:
		b.reopenAppForAcceptanceFix(taskID, channel, app, gaps)
		log.Printf("app acceptance %s: FAIL → reopened (attempt %d)", taskID, priorFails+1)
	}
}

// decideAppAcceptance is the pure policy: PASS only when the judge says the app
// meets the brief AND no deterministic check failed; otherwise REOPEN for an
// auto-fix while retries remain, then HALT (flag for a human) once exhausted. A
// deterministic gap fails the app even if the judge said meets=true — a build
// that didn't publish never "meets the brief", whatever the prose claims.
func decideAppAcceptance(
	decision appAcceptanceDecision,
	detGaps []string,
	priorFails int,
) (appAcceptanceAction, []string) {
	gaps := make([]string, 0, len(detGaps)+len(decision.Gaps))
	gaps = append(gaps, detGaps...)
	gaps = append(gaps, sanitizeGaps(decision.Gaps)...)
	if decision.Meets && len(detGaps) == 0 {
		return appAcceptanceActionPass, nil
	}
	if priorFails >= appAcceptanceMaxRetries {
		return appAcceptanceActionHalt, gaps
	}
	return appAcceptanceActionReopen, gaps
}

// appForEditChannel resolves the app this task produced: the app whose edit
// thread is bound to the task's channel. Reads the app store (its own lock);
// never call while holding b.mu.
func (b *Broker) appForEditChannel(channel string) (CustomApp, bool) {
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		return CustomApp{}, false
	}
	apps, err := b.appStore().List()
	if err != nil {
		return CustomApp{}, false
	}
	for _, a := range apps {
		if normalizeChannelSlug(a.EditChannel) == channel {
			return a, true
		}
	}
	return CustomApp{}, false
}

// deterministicAppGaps are the cheap, never-wrong checks the judge shouldn't have
// to reason about: the app actually published a non-trivial, versioned bundle.
func (b *Broker) deterministicAppGaps(app CustomApp) []string {
	var gaps []string
	if !strings.EqualFold(strings.TrimSpace(app.Status), "ready") || app.Version < 1 {
		gaps = append(gaps, "The app did not publish a ready, versioned build.")
	}
	if _, html, err := b.appStore().Get(app.ID); err != nil || len(html) < appAcceptanceMinBundleBytes {
		gaps = append(gaps, "The published bundle is missing or trivially small.")
	}
	return gaps
}

// countAppAcceptanceFailsLocked counts prior acceptance-fail notices already in
// the task channel — the retry budget. Stateless + persisted, so it survives a
// restart and a reopened task can't loop forever. Caller holds b.mu.
func (b *Broker) countAppAcceptanceFailsLocked(channel string) int {
	channel = normalizeChannelSlug(channel)
	n := 0
	for i := range b.messages {
		if normalizeChannelSlug(b.messages[i].Channel) == channel &&
			b.messages[i].Kind == appAcceptanceFailKind {
			n++
		}
	}
	return n
}

// postAppAcceptanceResult records a non-reopening acceptance outcome (pass or
// human-halt) in the task channel.
func (b *Broker) postAppAcceptanceResult(channel, kind, content string) {
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      kind,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err := b.saveLocked(); err != nil {
		log.Printf("app acceptance: persist result: %v", err)
	}
}

// reopenAppForAcceptanceFix gates "done": it returns the task to in_progress,
// posts the concrete gaps addressed to the App Builder, and wakes it through the
// same task_followup path a human edit uses. The App Builder fixes + republishes;
// the next done re-runs this gate (bounded by the fail-notice count).
func (b *Broker) reopenAppForAcceptanceFix(taskID, channel string, app CustomApp, gaps []string) {
	channel = normalizeChannelSlug(channel)
	b.mu.Lock()
	defer b.mu.Unlock()
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return
	}
	task.status = "in_progress"
	content := fmt.Sprintf(
		"@%s Acceptance check: %q does not yet meet the brief. Fix these, then republish with register_app:\n%s",
		appBuilderSlug, app.Name, renderGaps(gaps),
	)
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      appAcceptanceFailKind,
		Content:   content,
		Tagged:    []string{appBuilderSlug},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	// Wake the App Builder through the same task_followup path human edits use.
	b.appendActionLocked(taskFollowUpActionKind, "office", channel, "system",
		truncateSummary("acceptance gaps for "+app.Name, 140), taskID)
	if err := b.saveLocked(); err != nil {
		log.Printf("app acceptance: reopen persist: %v", err)
	}
}

// buildAppAcceptancePrompt composes the judge's system + user prompt. The judge
// only verdicts against the brief; it never calls a tool. The strict JSON
// contract is what the broker actuates.
func buildAppAcceptancePrompt(app CustomApp, caps, brief string, detGaps []string) (system, user string) {
	system = "You are an acceptance reviewer for a small internal React tool (an \"App\") that a builder agent just produced for a human. " +
		"Decide whether the FINISHED app actually satisfies the human's brief — NOT whether it compiles (that is already checked separately). " +
		"Judge ONLY against the brief's explicit requirements: for each requirement (a specific input, a named output, a workflow step, a control, a stated behavior), is it implemented by the app as described by its capabilities/source? " +
		"Be strict but fair. A requirement the brief states that the app does not implement is a GAP. Do NOT invent requirements the brief never stated, and do NOT fail an app for lacking a capability the workspace cannot provide. " +
		"Respond with EXACTLY ONE JSON object and nothing else: {\"meets\": boolean, \"summary\": string, \"gaps\": string[]}. " +
		"meets: true ONLY if every stated requirement is satisfied. summary: one line on the verdict. gaps: a SHORT list of concrete, actionable shortfalls, each naming the unmet requirement (empty when meets=true). " +
		"When you cannot confirm a stated requirement is met from the capabilities/source, list it as a gap to fix rather than passing silently."

	var u strings.Builder
	fmt.Fprintf(&u, "THE BRIEF (what the human asked for)\n%s\n\n", appEvalCapBytes(brief, appAcceptanceMaxBriefBytes))
	fmt.Fprintf(&u, "THE BUILT APP\nName: %s\nSummary: %s\n", strings.TrimSpace(app.Name), strings.TrimSpace(app.Summary))
	if caps != "" {
		u.WriteString("\nWHAT THE APP ACTUALLY DOES (introspected from its source)\n")
		u.WriteString(caps)
		u.WriteString("\n")
	}
	if len(detGaps) > 0 {
		u.WriteString("\nDETERMINISTIC CHECKS ALREADY FAILED (treat as confirmed gaps)\n")
		u.WriteString(renderGaps(detGaps))
		u.WriteString("\n")
	}
	return system, u.String()
}

// parseAppAcceptanceDecision extracts the judge's JSON verdict, tolerating any
// prose or fences the model adds.
func parseAppAcceptanceDecision(out string) (appAcceptanceDecision, bool) {
	raw, ok := extractFirstJSON(out)
	if !ok {
		return appAcceptanceDecision{}, false
	}
	var d appAcceptanceDecision
	if err := json.Unmarshal(raw, &d); err != nil {
		return appAcceptanceDecision{}, false
	}
	return d, true
}

// sanitizeGaps trims, caps each entry, and bounds the count so a runaway judge
// can't flood the channel.
func sanitizeGaps(gaps []string) []string {
	out := make([]string, 0, len(gaps))
	for _, g := range gaps {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		out = append(out, truncateSummary(g, 240))
		if len(out) >= appAcceptanceMaxGaps {
			break
		}
	}
	return out
}

func renderGaps(gaps []string) string {
	var b strings.Builder
	for _, g := range gaps {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", g)
	}
	return strings.TrimRight(b.String(), "\n")
}

func appEvalCapBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

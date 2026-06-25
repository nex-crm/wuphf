package team

// broker_apps_proposal.go — the implicit-intent permission gate for Apps.
//
// When an agent notices a repeatable workflow it does NOT build silently. It
// raises a non-blocking approval request (propose_app -> POST /requests with an
// app_proposal payload). The human can approve, reject, or "add context and
// approve" (approve_with_note). Only on an approve does the broker spawn a task
// owned by the App Builder agent to actually build (or improve) the app.
//
// Explicit paths (the /create-app, /update-app slash commands and the Edit
// button on an app screen) skip this gate — the human initiated the build — and
// post the App Builder task directly via the normal task surface.

import (
	"fmt"
	"strings"
	"time"
)

// appBuilderSlug is the team-package mirror of company.AppBuilderSlug — the
// slug of the built-in App Builder agent. Kept as a local const so the hot
// broker paths don't import the company package just for one identifier; the
// two MUST stay in sync.
const appBuilderSlug = "app-builder"

// appProposalSpec is the structured app request carried on an approval request
// (humanInterview.AppProposal) and unpacked into the App Builder task when the
// human approves.
type appProposalSpec struct {
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description"`
	// AppID is set when the proposal improves an EXISTING app rather than
	// creating a new one — the agent is expected to have checked list_apps
	// first and found a related app to extend instead of duplicating.
	AppID string `json:"app_id,omitempty"`
	// ObservedSteps is the deterministic tool-shape the detector mined for this
	// proposal (e.g. ["crm_fetch_leads", "score_leads"]). Empty for agent-raised
	// proposals. Passed through to the App Builder's brief as concrete, proven
	// build targets so the generator works from the real steps, not just prose.
	ObservedSteps []string `json:"observed_steps,omitempty"`
}

// sanitizeAppProposalSpec trims the proposal and drops it entirely when it has
// no usable name — a proposal with nothing to build should not mark the request.
func sanitizeAppProposalSpec(in *appProposalSpec) *appProposalSpec {
	if in == nil {
		return nil
	}
	out := appProposalSpec{
		Name:        strings.TrimSpace(in.Name),
		Icon:        strings.TrimSpace(in.Icon),
		Summary:     strings.TrimSpace(in.Summary),
		Description: strings.TrimSpace(in.Description),
		AppID:       strings.TrimSpace(in.AppID),
	}
	for _, step := range in.ObservedSteps {
		if step = strings.TrimSpace(step); step != "" {
			out.ObservedSteps = append(out.ObservedSteps, step)
		}
	}
	if out.Name == "" {
		return nil
	}
	return &out
}

// appProposalApproved reports whether an answer choice green-lights the build.
// "approve" and "approve_with_note" (= add context and approve) both proceed;
// reject / reject_with_steer / needs_more_info do not.
func appProposalApproved(choiceID string) bool {
	switch strings.ToLower(strings.TrimSpace(choiceID)) {
	case "approve", "approve_with_note":
		return true
	default:
		return false
	}
}

// maybeSpawnAppBuilderTaskFromProposal inspects a just-answered request and, if
// it was an approved App Builder proposal, creates the App Builder task. Called
// from handlePostRequestAnswer OUTSIDE b.mu because MutateTask locks internally.
func (b *Broker) maybeSpawnAppBuilderTaskFromProposal(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	b.mu.Lock()
	var snapshot humanInterview
	found := false
	for i := range b.requests {
		if b.requests[i].ID == requestID {
			snapshot = cloneHumanInterview(b.requests[i])
			found = true
			break
		}
	}
	b.mu.Unlock()
	if !found || snapshot.AppProposal == nil || snapshot.Answered == nil {
		return
	}
	if !appProposalApproved(snapshot.Answered.GetChoiceID()) {
		return
	}

	note := snapshot.Answered.GetCustomText()
	title, details := appBuilderTaskBrief(*snapshot.AppProposal, note)
	channel := normalizeChannelSlug(snapshot.Channel)
	if channel == "" {
		channel = "general"
	}
	if _, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   channel,
		Title:     title,
		Details:   details,
		Owner:     appBuilderSlug,
		CreatedBy: "system",
		TaskType:  "issue",
	}); err != nil {
		b.mu.Lock()
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   channel,
			Content:   fmt.Sprintf("Could not start the App Builder task for \"%s\": %s", snapshot.AppProposal.Name, strings.TrimSpace(err.Error())),
			Tagged:    uniqueSlugs([]string{appBuilderSlug}),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		_ = b.saveLocked()
		b.mu.Unlock()
	}
}

// appBuilderTaskBrief composes the task title + details handed to the App
// Builder. The brief tells it exactly what to ship and how to register it.
func appBuilderTaskBrief(spec appProposalSpec, humanNote string) (string, string) {
	verb := "Build"
	if spec.AppID != "" {
		verb = "Improve"
	}
	title := fmt.Sprintf("%s app: %s", verb, spec.Name)

	var d strings.Builder
	if spec.AppID != "" {
		fmt.Fprintf(&d, "Improve the existing app `%s` (\"%s\").\n\n", spec.AppID, spec.Name)
		d.WriteString("First call get_app to read its current source and manifest, then apply the change below.\n\n")
	} else {
		fmt.Fprintf(&d, "Build a new internal tool named \"%s\".\n\n", spec.Name)
	}
	if spec.Summary != "" {
		fmt.Fprintf(&d, "Summary: %s\n\n", spec.Summary)
	}
	d.WriteString("What it should do:\n")
	d.WriteString(spec.Description)
	d.WriteString("\n")
	if len(spec.ObservedSteps) > 0 {
		fmt.Fprintf(&d, "\nObserved workflow steps (mined from the repeated work — implement these as the tool's backbone):\n%s\n",
			strings.Join(spec.ObservedSteps, " -> "))
	}
	if note := strings.TrimSpace(humanNote); note != "" {
		fmt.Fprintf(&d, "\nHuman constraints / added context:\n%s\n", note)
	}
	d.WriteString("\nWhen the build passes, register it with register_app")
	if spec.AppID != "" {
		fmt.Fprintf(&d, " (app_id=%s)", spec.AppID)
	}
	d.WriteString(" so it appears under Apps.")
	return title, d.String()
}

// appEditSessionBrief composes the task the broker creates when a human opens
// the "chat to edit" panel on an app that has no edit thread yet (an app minted
// before edit-channel stamping). Creating the task mints a fresh task-<id>
// channel that the create hook binds to this app, turning it into the app's
// persistent edit thread. The brief tells the App Builder to greet and WAIT —
// no change is requested yet, so it must NOT rebuild until the human asks. The
// register_app(app_id=…) line is load-bearing: stampAppEditChannelForTaskLocked
// parses the id from it to bind the channel.
func appEditSessionBrief(app CustomApp, capsSummary string) (string, string) {
	title := fmt.Sprintf("Edit app: %s", app.Name)
	var d strings.Builder
	fmt.Fprintf(&d, "Open the persistent edit thread for the existing app `%s` (\"%s\").\n\n", app.ID, app.Name)
	if s := strings.TrimSpace(capsSummary); s != "" {
		d.WriteString(s)
		d.WriteString("\n")
	}
	d.WriteString("No change is requested yet — the human just opened the \"chat to edit\" panel beside this app. ")
	d.WriteString("Call get_app to read its current source and manifest (it returns the same capabilities summary above), then post ONE short greeting grounded in what the app ACTUALLY does today — do not claim capabilities not listed above — and ask what they would like to change. Do NOT modify or republish the app until they ask.\n\n")
	fmt.Fprintf(&d, "When they request a change here, apply it to the source and republish with register_app(app_id=%s) so it updates in place.", app.ID)
	return title, d.String()
}

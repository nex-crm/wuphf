package team

import (
	"fmt"
	"strings"
)

type officeSignal struct {
	ID            string
	Source        string
	Kind          string
	Title         string
	Content       string
	Confidence    string
	Urgency       string
	Channel       string
	Owner         string
	RequiresHuman bool
	Blocking      bool
}

type officeActionPlan struct {
	Summary        string
	Tagged         []string
	Tasks          []insightTaskPlan
	Requests       []humanInterview
	DecisionKind   string
	DecisionReason string
}

func buildInsightSignals(insights []nexInsight) []officeSignal {
	important := selectImportantInsights(insights)
	signals := make([]officeSignal, 0, len(important))
	for _, insight := range important {
		content := strings.TrimSpace(insight.Content)
		if content == "" {
			continue
		}
		if hint := strings.TrimSpace(insight.Target.Hint); hint != "" {
			content += " (" + hint + ")"
		}
		requiresHuman, blocking := signalNeedsHuman(content, insight.Type)
		signals = append(signals, officeSignal{
			ID:            insight.ID,
			Source:        "nex_insights",
			Kind:          strings.TrimSpace(insight.Type),
			Title:         "Nex insight",
			Content:       content,
			Confidence:    strings.TrimSpace(insight.ConfidenceLevel),
			Urgency:       inferUrgency(content, insight.Type),
			Channel:       "general",
			Owner:         inferInsightOwner(insight),
			RequiresHuman: requiresHuman,
			Blocking:      blocking,
		})
	}
	return signals
}

func buildNotificationSignals(items []nexFeedItem) []officeSignal {
	signals := make([]officeSignal, 0, len(items))
	for _, item := range items {
		title, content := formatNexFeedItem(item)
		if strings.TrimSpace(content) == "" {
			continue
		}
		requiresHuman, blocking := signalNeedsHuman(content, item.Type)
		signals = append(signals, officeSignal{
			ID:            item.ID,
			Source:        "nex_notifications",
			Kind:          strings.TrimSpace(item.Type),
			Title:         title,
			Content:       content,
			Urgency:       inferUrgency(content, item.Type),
			Channel:       "general",
			Owner:         inferOwnerFromText(title + " " + content),
			RequiresHuman: requiresHuman,
			Blocking:      blocking,
		})
	}
	return signals
}

func isHumanDirectiveMessage(msg channelMessage) bool {
	from := strings.TrimSpace(strings.ToLower(msg.From))
	if from != "you" && from != "human" {
		return false
	}
	kind := strings.TrimSpace(strings.ToLower(msg.Kind))
	return kind == ""
}

func buildHumanDirectiveSignal(msg channelMessage) (officeSignal, bool) {
	if !isHumanDirectiveMessage(msg) {
		return officeSignal{}, false
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return officeSignal{}, false
	}
	owner := "ceo"
	for _, tagged := range msg.Tagged {
		if tagged == "ceo" {
			owner = "ceo"
			break
		}
		if strings.TrimSpace(tagged) != "" {
			owner = tagged
			break
		}
	}
	if owner == "ceo" {
		if inferred := inferOwnerFromText(content); inferred != "" {
			owner = inferred
		}
	}
	return officeSignal{
		ID:         strings.TrimSpace(msg.ID),
		Source:     "human_directive",
		Kind:       "directive",
		Title:      "Human directive",
		Content:    content,
		Confidence: "explicit",
		Urgency:    "high",
		Channel:    normalizeChannelSlug(msg.Channel),
		Owner:      owner,
	}, true
}

func planHumanDirective(msg channelMessage) officeActionPlan {
	plan := officeActionPlan{
		DecisionKind:   "wake_specialist",
		DecisionReason: "Direct human instructions should interrupt background work and be triaged ahead of autonomous follow-up.",
	}
	targets := make([]string, 0, len(msg.Tagged))
	for _, tagged := range uniqueSlugs(msg.Tagged) {
		if tagged == "ceo" || strings.TrimSpace(tagged) == "" {
			continue
		}
		targets = append(targets, tagged)
	}
	if len(targets) == 0 {
		if inferred := inferOwnerFromText(msg.Content); inferred != "" && inferred != "ceo" {
			targets = append(targets, inferred)
		}
	}
	if len(targets) == 0 {
		plan.DecisionKind = "triage_human_directive"
		plan.DecisionReason = "The CEO should triage this direct human request before waking anyone else."
	}
	plan.Tagged = targets

	lines := []string{
		"Human directed the office:",
		"- " + strings.TrimSpace(msg.Content),
	}
	switch {
	case len(targets) > 1:
		lines = append(lines, "", "CEO should triage first, then wake @"+strings.Join(targets, ", @")+".")
	case len(targets) == 1:
		lines = append(lines, "", "CEO should triage first, then wake @"+targets[0]+".")
	default:
		lines = append(lines, "", "CEO should triage this directly before any background work takes priority.")
	}
	plan.Summary = strings.Join(lines, "\n")
	return plan
}

func planOfficeActions(signals []officeSignal) officeActionPlan {
	plan := officeActionPlan{}
	if len(signals) == 0 {
		return plan
	}

	lines := []string{"Nex surfaced a few things worth acting on:"}
	seenOwners := map[string]struct{}{}
	seenTasks := map[string]struct{}{}
	for _, signal := range signals {
		line := "- " + signal.Content
		if signal.Urgency != "" {
			line += " [" + signal.Urgency + "]"
		}
		lines = append(lines, line)
		if signal.Owner != "" {
			if _, ok := seenOwners[signal.Owner]; !ok {
				plan.Tagged = append(plan.Tagged, signal.Owner)
				seenOwners[signal.Owner] = struct{}{}
			}
			key := strings.ToLower(strings.TrimSpace(signal.Owner + "::" + truncate(signal.Content, 80)))
			if _, exists := seenTasks[key]; !exists {
				seenTasks[key] = struct{}{}
				plan.Tasks = append(plan.Tasks, insightTaskPlan{
					Owner:   signal.Owner,
					Title:   fmt.Sprintf("Follow up on Nex signal: %s", truncate(strings.TrimSpace(signal.Content), 72)),
					Details: buildSignalTaskDetails(signal),
				})
			}
		}
		if signal.RequiresHuman {
			kind := requestKindForSignal(signal)
			plan.Requests = append(plan.Requests, humanInterview{
				Kind:          kind,
				Status:        "pending",
				From:          "ceo",
				Channel:       signal.Channel,
				Title:         signalRequestTitle(signal),
				Question:      signalQuestion(signal),
				Context:       signal.Content,
				Blocking:      signal.Blocking || kind == "approval" || kind == "choice" || kind == "confirm",
				Required:      true,
				Options:       signalRequestOptions(signal),
				RecommendedID: recommendedIDForKind(kind),
				CreatedAt:     "",
			})
		}
	}

	// When tasks were created but no explicit human requests exist, inject a
	// confirmation request so the human can approve, adjust, or redirect
	// the planned work before agents act on it.
	if len(plan.Tasks) > 0 && len(plan.Requests) == 0 {
		taskOwners := make([]string, 0, len(plan.Tasks))
		for _, t := range plan.Tasks {
			taskOwners = append(taskOwners, "@"+t.Owner)
		}
		confirmSig := officeSignal{Kind: "confirm"}
		plan.Requests = append(plan.Requests, humanInterview{
			Kind:          "confirm",
			Status:        "pending",
			From:          "ceo",
			Channel:       "general",
			Title:         "Confirmation needed",
			Question:      fmt.Sprintf("The office is about to open tasks for %s. Does this look right?", strings.Join(uniqueSlugs(taskOwners), ", ")),
			Context:       plan.Tasks[0].Details,
			Blocking:      true,
			Required:      true,
			Options:       signalRequestOptions(confirmSig),
			RecommendedID: "confirm_proceed",
			CreatedAt:     "",
		})
	}

	if len(plan.Tasks) > 0 {
		lines = append(lines, "", "I opened tasks for the right owners so we do not dogpile this.")
	}
	if len(plan.Requests) > 0 {
		lines = append(lines, "Some of this needs a human call, so I also opened a request instead of guessing.")
	}
	plan.Summary = strings.Join(lines, "\n")
	switch {
	case len(plan.Requests) > 0 && len(plan.Tasks) > 0:
		plan.DecisionKind = "ask_human_and_create_task"
		plan.DecisionReason = "High-signal context change produced owned work and at least one human decision point."
	case len(plan.Requests) > 0:
		plan.DecisionKind = "ask_human"
		plan.DecisionReason = "The signal requires a human call before the office should proceed."
	default:
		plan.DecisionKind = "summarize"
		plan.DecisionReason = "The signal is worth surfacing, but not strong enough to create new owned work."
	}
	return plan
}

func buildSignalTaskDetails(signal officeSignal) string {
	parts := []string{strings.TrimSpace(signal.Content)}
	if signal.Source != "" {
		parts = append(parts, "Source: "+signal.Source)
	}
	if signal.Kind != "" {
		parts = append(parts, "Kind: "+signal.Kind)
	}
	if signal.Confidence != "" {
		parts = append(parts, "Confidence: "+signal.Confidence)
	}
	if signal.Urgency != "" {
		parts = append(parts, "Urgency: "+signal.Urgency)
	}
	return strings.Join(parts, "\n")
}

func signalRequestTitle(signal officeSignal) string {
	switch requestKindForSignal(signal) {
	case "approval":
		return "Approval needed"
	case "choice":
		return "Decision needed"
	case "confirm":
		return "Confirmation needed"
	default:
		return "Human input needed"
	}
}

func signalQuestion(signal officeSignal) string {
	switch requestKindForSignal(signal) {
	case "approval":
		return "Should the office act on this now, or does this need a different call?"
	case "choice":
		return "What direction should the office take on this?"
	case "confirm":
		return "The office wants to act on this. Does the plan look right?"
	default:
		return "How should the office handle this?"
	}
}

func recommendedIDForKind(kind string) string {
	switch kind {
	case "approval":
		return "approve"
	case "choice":
		return "balanced"
	case "confirm":
		return "confirm_proceed"
	default:
		return "proceed"
	}
}

func signalRequestOptions(signal officeSignal) []interviewOption {
	switch requestKindForSignal(signal) {
	case "approval":
		return []interviewOption{
			{ID: "approve", Label: "Approve", Description: "Green-light this and let the team execute immediately."},
			{ID: "approve_with_conditions", Label: "Approve with conditions", Description: "Proceed, but I will add constraints the team must follow."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Not enough context to decide — gather more details first."},
			{ID: "delegate", Label: "Delegate", Description: "Route to a specific person for a closer look before deciding."},
			{ID: "reject", Label: "Reject", Description: "Do not proceed with this."},
		}
	case "choice":
		return []interviewOption{
			{ID: "move_fast", Label: "Move fast", Description: "Bias toward speed — ship now, iterate later."},
			{ID: "balanced", Label: "Balanced", Description: "Weigh trade-offs and aim for a reasonable middle ground."},
			{ID: "be_careful", Label: "Be careful", Description: "Bias toward caution — tighter review loop before acting."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Not enough context to choose — investigate first."},
			{ID: "delegate", Label: "Delegate", Description: "Let the owner or a specialist make this call."},
		}
	case "confirm":
		return []interviewOption{
			{ID: "confirm_proceed", Label: "Confirm", Description: "Looks good — proceed as planned."},
			{ID: "adjust", Label: "Adjust", Description: "Proceed with modifications — I will add details."},
			{ID: "reassign", Label: "Reassign", Description: "Wrong owner or scope — reroute this to someone else."},
			{ID: "hold", Label: "Hold", Description: "Do not act yet — I need to review this further."},
		}
	default:
		return []interviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "I will provide specific guidance for how to handle this."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person to own."},
			{ID: "hold", Label: "Hold", Description: "Pause until I review this further."},
		}
	}
}

func requestKindForSignal(signal officeSignal) string {
	text := strings.ToLower(strings.TrimSpace(signal.Content + " " + signal.Kind))
	switch {
	case signal.Blocking:
		return "approval"
	case strings.Contains(text, "choose"), strings.Contains(text, "decision"),
		strings.Contains(text, "which"), strings.Contains(text, "priorit"):
		return "choice"
	case strings.Contains(text, "confirm"), strings.Contains(text, "verify"),
		strings.Contains(text, "check"), strings.Contains(text, "review"):
		return "confirm"
	default:
		return "approval"
	}
}

func signalNeedsHuman(content, kind string) (requiresHuman bool, blocking bool) {
	text := strings.ToLower(strings.TrimSpace(content + " " + kind))

	// Blocking: explicit human gate-keeping required by policy.
	switch {
	case strings.Contains(text, "approval"), strings.Contains(text, "approve"),
		strings.Contains(text, "legal"), strings.Contains(text, "security review"),
		strings.Contains(text, "permission"), strings.Contains(text, "contract"):
		return true, true
	}

	// Non-blocking but still requires human decision.
	switch {
	case strings.Contains(text, "should we"), strings.Contains(text, "choose"),
		strings.Contains(text, "decision"), strings.Contains(text, "confirm"):
		return true, false
	}

	// Autonomous-safe: purely informational signals that need no human action.
	switch {
	case strings.Contains(text, "fyi"), strings.Contains(text, "status update"),
		strings.Contains(text, "summary"), strings.Contains(text, "no action needed"),
		strings.Contains(text, "resolved"), strings.Contains(text, "completed"):
		return false, false
	}

	// Default: route to human. Unless policy says otherwise, humans decide.
	return true, false
}

func inferUrgency(content, kind string) string {
	text := strings.ToLower(strings.TrimSpace(content + " " + kind))
	switch {
	case strings.Contains(text, "urgent"), strings.Contains(text, "critical"), strings.Contains(text, "blocked"), strings.Contains(text, "freeze"), strings.Contains(text, "outage"):
		return "high"
	case strings.Contains(text, "risk"), strings.Contains(text, "budget"), strings.Contains(text, "delay"), strings.Contains(text, "follow up"), strings.Contains(text, "action"):
		return "medium"
	default:
		return "normal"
	}
}

func inferOwnerFromText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(text, "sales"), strings.Contains(text, "pipeline"), strings.Contains(text, "pricing"), strings.Contains(text, "revenue"), strings.Contains(text, "budget"):
		return "cro"
	case strings.Contains(text, "campaign"), strings.Contains(text, "positioning"), strings.Contains(text, "brand"), strings.Contains(text, "marketing"), strings.Contains(text, "launch"):
		return "cmo"
	case strings.Contains(text, "design"), strings.Contains(text, "hero"), strings.Contains(text, "landing"), strings.Contains(text, "ui"):
		return "designer"
	case strings.Contains(text, "frontend"), strings.Contains(text, "signup"), strings.Contains(text, "web"):
		return "fe"
	case strings.Contains(text, "backend"), strings.Contains(text, "database"), strings.Contains(text, "api"), strings.Contains(text, "integration"):
		return "be"
	case strings.Contains(text, "ai"), strings.Contains(text, "llm"), strings.Contains(text, "model"), strings.Contains(text, "transcript"):
		return "ai"
	default:
		return "pm"
	}
}

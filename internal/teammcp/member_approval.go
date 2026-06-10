package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// requireTeamMemberApproval gates agent-initiated office-member creation behind
// a blocking human approval. Spinning up a new specialist is a durable,
// cost-incurring, persistent act, so an agent (the CEO included) may PROPOSE a
// new member, but a human must approve before it is created. This mirrors the
// external-action approval gate (requireTeamActionApproval in actions.go):
// create a blocking "approval" request in the Requests system, then poll until
// the human decides.
//
// Returns nil when approved (the caller then creates the member). Returns a
// descriptive error on reject / hold / cancel / timeout so the agent routes to
// "reuse an existing specialist instead" rather than retrying the create.
//
// WUPHF_UNSAFE=1 bypasses the gate, matching the action gate and the --unsafe
// launch flag: an operator who set it has explicitly opted out of every
// approval gate. Human-initiated member creation does not reach this path — the
// UI posts to /office-members directly; only the agent-facing team_member tool
// calls this.
func requireTeamMemberApproval(ctx context.Context, actor string, args TeamMemberArgs) error {
	if os.Getenv("WUPHF_UNSAFE") == "1" {
		return nil
	}

	slug := normalizeSlug(args.Slug)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "ceo"
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = slug
	}
	role := strings.TrimSpace(args.Role)

	ctxLines := []string{
		fmt.Sprintf("@%s wants to add a NEW office member (a new agent, not a task).", actor),
		fmt.Sprintf("- Slug: %s", slug),
		fmt.Sprintf("- Name: %s", name),
	}
	if role != "" {
		ctxLines = append(ctxLines, fmt.Sprintf("- Role: %s", role))
	}
	if len(args.Expertise) > 0 {
		ctxLines = append(ctxLines, fmt.Sprintf("- Expertise: %s", strings.Join(args.Expertise, ", ")))
	}
	if p := strings.TrimSpace(args.Personality); p != "" {
		ctxLines = append(ctxLines, fmt.Sprintf("- Personality: %s", p))
	}
	if pk := strings.TrimSpace(args.Provider); pk != "" {
		runtime := pk
		if m := strings.TrimSpace(args.Model); m != "" {
			runtime += " / " + m
		}
		ctxLines = append(ctxLines, fmt.Sprintf("- Runtime: %s", runtime))
	}
	ctxLines = append(ctxLines, "Approve only if no existing teammate can cover this work. Rejecting keeps the roster unchanged; the requester should reuse an existing specialist.")

	title := fmt.Sprintf("Approve new specialist @%s?", slug)
	question := fmt.Sprintf("Add %s (@%s) to the team?", name, slug)
	if role != "" {
		question = fmt.Sprintf("Add %s (@%s, %s) to the team?", name, slug, role)
	}

	// Advertise ONLY a binary approve/reject pair. The create path has a
	// binary outcome — nil (proceed) or error (don't) — and reads no typed
	// guidance, so the stock "approval" set (which adds approve_with_note,
	// reject_with_steer, and hold) would offer steering whose semantics this
	// handler silently discards. Passing an explicit minimal set keeps
	// normalizeHumanRequestOptions from auto-injecting those options; it still
	// enriches the labels/descriptions from the "approval" defaults by ID.
	options, recommendedID := normalizeHumanRequestOptions("approval", "approve", []HumanInterviewOption{
		{ID: "approve"},
		{ID: "reject"},
	})

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           "approval",
		"channel":        "general",
		"from":           actor,
		"title":          title,
		"question":       question,
		"context":        strings.Join(ctxLines, "\n"),
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       true,
		"required":       true,
		// Collapse repeated create attempts for the same slug onto one card so
		// an agent retry loop can't stack duplicate approval requests.
		"dedupe_key": "member-create:" + slug,
	}, &created); err != nil {
		return fmt.Errorf("create member-approval request: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return fmt.Errorf("member-approval request did not return an ID")
	}

	timeout := time.NewTimer(actionApprovalTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(actionApprovalPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for human approval to create @%s after %s; do NOT retry — assign this work to an existing specialist instead", slug, actionApprovalTimeout)
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				return fmt.Errorf("poll member approval: %w", err)
			}
			switch strings.ToLower(strings.TrimSpace(result.Status)) {
			case "canceled", "cancelled":
				return fmt.Errorf("human canceled the request to create @%s; assign this work to an existing specialist instead", slug)
			case "not_found":
				return fmt.Errorf("member-approval request not found for @%s", slug)
			}
			if result.Answered == nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(result.Answered.ChoiceID)) {
			case "approve", "approve_with_note", "confirm_proceed":
				return nil
			}
			reason := strings.TrimSpace(result.Answered.CustomText)
			if reason == "" {
				reason = strings.TrimSpace(result.Answered.ChoiceText)
			}
			if reason == "" {
				reason = strings.TrimSpace(result.Answered.ChoiceID)
			}
			return fmt.Errorf("human declined to create @%s (%s); do NOT retry team_member create — assign this work to an existing specialist instead", slug, reason)
		}
	}
}

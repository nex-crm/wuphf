package teammcp

// routine_tools.go — team_routine: the agent-reachable path for standing
// automations (ten-out-of-ten Wave D / D1).
//
// ICP-eval v3 [18:30–18:36]: asked for one weekly summary, agents
// registered TWO provider-session cron jobs ("live only for 7 days in the
// current session") that the Scheduled Tasks app could not see. The office
// scheduler was persistent the whole time; agents just had no tool that
// reached it for general recurring work (team_action_workflow_schedule
// only schedules saved EXTERNAL workflows and is gated on an action
// provider).
//
// team_routine routes into POST /scheduler/routines on the broker:
// persistent across restarts and session ends, visible + editable in the
// Scheduled Tasks app, and deduped server-side by normalized
// purpose+schedule, so a second agent registering the same automation
// updates the existing job instead of minting a duplicate.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/calendar"
)

// TeamRoutineArgs is the input shape for team_routine.
type TeamRoutineArgs struct {
	Purpose  string `json:"purpose" jsonschema:"One line describing what the standing automation does, e.g. 'Weekly Monday 9am renewal risk summary to #general'. Re-registering the same purpose and schedule updates the existing routine instead of creating a duplicate."`
	Schedule string `json:"schedule" jsonschema:"Cron expression or shorthand like daily, hourly, 4h, or 0 9 * * 1"`
	Channel  string `json:"channel,omitempty" jsonschema:"Office channel the routine posts into on each run. Defaults to the current conversation channel."`
	Owner    string `json:"owner,omitempty" jsonschema:"Agent slug that handles each scheduled run. Defaults to you."`
	Prompt   string `json:"prompt,omitempty" jsonschema:"Instructions posted to the owner on every scheduled run. Be specific: this is the whole work order the owner receives."`
	MySlug   string `json:"my_slug,omitempty" jsonschema:"Agent slug registering the routine. Defaults to WUPHF_AGENT_SLUG."`
	Summary  string `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

const teamRoutineDescription = "Register a standing automation (recurring routine) in the office's persistent scheduler. " +
	"This is the ONLY correct way to set up recurring work: the job is stored by the broker, survives restarts and session ends, " +
	"runs on schedule through the office scheduler, and is visible and editable by the human in the Scheduled Tasks app. " +
	"Registering the same purpose + schedule again updates the existing routine instead of creating a duplicate, so do not " +
	"register a second job for an automation a teammate already set up. NEVER use provider-level or session-scoped cron " +
	"features for office automations — those die with the session, need re-registration, and are invisible to the human."

func registerRoutineTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_routine",
		teamRoutineDescription,
	), handleTeamRoutine)
}

// teamRoutineJobResponse mirrors the broker's POST /scheduler/routines
// response shape (job + updated flag).
type teamRoutineJobResponse struct {
	Job struct {
		Slug         string `json:"slug"`
		Label        string `json:"label"`
		ScheduleExpr string `json:"schedule_expr"`
		Channel      string `json:"channel"`
		TargetID     string `json:"target_id"`
		NextRun      string `json:"next_run"`
		Enabled      bool   `json:"enabled"`
	} `json:"job"`
	Updated bool `json:"updated"`
}

func handleTeamRoutine(ctx context.Context, _ *mcp.CallToolRequest, args TeamRoutineArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	purpose := strings.TrimSpace(args.Purpose)
	if purpose == "" {
		return toolError(fmt.Errorf("purpose is required")), nil, nil
	}
	scheduleExpr := strings.TrimSpace(args.Schedule)
	if scheduleExpr == "" {
		return toolError(fmt.Errorf("schedule is required")), nil, nil
	}
	// Validate locally for a crisp error before crossing the wire.
	sched, err := calendar.ParseCron(scheduleExpr)
	if err != nil {
		return toolError(fmt.Errorf("invalid schedule %q: %w", scheduleExpr, err)), nil, nil
	}
	if sched.Next(time.Now().UTC()).IsZero() {
		return toolError(fmt.Errorf("could not compute next run for %q", scheduleExpr)), nil, nil
	}
	owner := strings.TrimSpace(args.Owner)
	if owner == "" {
		owner = slug
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)

	var resp teamRoutineJobResponse
	if err := brokerPostJSON(ctx, "/scheduler/routines", map[string]any{
		"purpose":    purpose,
		"schedule":   scheduleExpr,
		"channel":    channel,
		"owner":      owner,
		"prompt":     strings.TrimSpace(args.Prompt),
		"created_by": slug,
	}, &resp); err != nil {
		return toolError(fmt.Errorf("register routine: %w", err)), nil, nil
	}

	verb := "registered"
	if resp.Updated {
		verb = "updated (an automation with this purpose + schedule already existed)"
	}
	_ = brokerRecordAction(ctx, "routine_registered", "scheduler", channel, slug,
		fallbackSummary(args.Summary, fmt.Sprintf("Standing automation %q %s (%s)", purpose, verb, scheduleExpr)),
		resp.Job.Slug)

	// The confirmation the agent relays to the human MUST be truthful:
	// the job is persistent and human-visible. No session-death caveat
	// applies — if one ever does again, the registration path regressed.
	result := map[string]any{
		"ok":          true,
		"slug":        resp.Job.Slug,
		"purpose":     resp.Job.Label,
		"schedule":    resp.Job.ScheduleExpr,
		"owner":       resp.Job.TargetID,
		"channel":     resp.Job.Channel,
		"next_run":    resp.Job.NextRun,
		"updated":     resp.Updated,
		"persistence": "Stored in the office scheduler (broker state). Survives restarts and session ends; no re-registration is ever needed.",
		"visible_in":  "Scheduled Tasks app — the human can pause, edit, or re-cadence it there.",
		"tell_the_human": fmt.Sprintf(
			"Standing automation %q is %s — it runs %s, persists across restarts, and is visible in the Scheduled Tasks app.",
			purpose, verb, scheduleExpr),
	}
	return textResult(prettyObject(result)), nil, nil
}

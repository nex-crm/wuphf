package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// cmdManagedAgent dispatches /managed-agent subcommands for HITL interactions
// with the Nex managed agents backend.
func cmdManagedAgent(ctx *SlashContext, args string) error {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		ctx.SendResult("Usage: /managed-agent <approve|respond|deny|stop|events> <run_id> [args...]", nil)
		return nil
	}
	sub := parts[0]
	switch sub {
	case "approve":
		return cmdManagedAgentApprove(ctx, parts[1:])
	case "respond":
		return cmdManagedAgentRespond(ctx, parts[1:])
	case "deny":
		return cmdManagedAgentDeny(ctx, parts[1:])
	case "stop":
		return cmdManagedAgentStop(ctx, parts[1:])
	case "events":
		return cmdManagedAgentEvents(ctx, parts[1:])
	default:
		ctx.SendResult(fmt.Sprintf("Unknown subcommand: %q. Use approve|respond|deny|stop|events", sub), nil)
		return nil
	}
}

// cmdManagedAgentApprove handles /managed-agent approve <run_id>
func cmdManagedAgentApprove(ctx *SlashContext, args []string) error {
	if len(args) == 0 {
		ctx.SendResult("Usage: /managed-agent approve <run_id>", nil)
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	runID := args[0]
	ctx.SetLoading(true)
	resp, err := ctx.APIClient.ApproveAgentRun(runID)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}

	if ctx.Format == "json" {
		b, _ := json.Marshal(resp)
		ctx.SendResult(string(b), nil)
	} else {
		ctx.SendResult(fmt.Sprintf("Run %s approved. Status: %s", runID, resp.Status), nil)
	}
	return nil
}

// cmdManagedAgentRespond handles /managed-agent respond <run_id> <message...>
func cmdManagedAgentRespond(ctx *SlashContext, args []string) error {
	if len(args) < 2 {
		ctx.SendResult("Usage: /managed-agent respond <run_id> <message>", nil)
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	runID := args[0]
	message := strings.Join(args[1:], " ")

	ctx.SetLoading(true)
	resp, err := ctx.APIClient.RespondToAgentRun(runID, message)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}

	if ctx.Format == "json" {
		b, _ := json.Marshal(resp)
		ctx.SendResult(string(b), nil)
	} else {
		ctx.SendResult(fmt.Sprintf("Response sent to run %s. Status: %s", runID, resp.Status), nil)
	}
	return nil
}

// cmdManagedAgentDeny handles /managed-agent deny <run_id> [--reason <text>]
func cmdManagedAgentDeny(ctx *SlashContext, args []string) error {
	if len(args) == 0 {
		ctx.SendResult("Usage: /managed-agent deny <run_id> [--reason <text>]", nil)
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	runID := args[0]
	reason := ""

	// Parse optional --reason flag from remaining slice.
	// args[1:] may contain ["--reason", "some", "reason", "text"] so collect
	// everything after --reason as the reason string.
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--reason" && i+1 < len(rest) {
			reason = strings.Join(rest[i+1:], " ")
			break
		}
	}

	ctx.SetLoading(true)
	resp, err := ctx.APIClient.DenyAgentRun(runID, reason)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}

	if ctx.Format == "json" {
		b, _ := json.Marshal(resp)
		ctx.SendResult(string(b), nil)
	} else {
		ctx.SendResult(fmt.Sprintf("Run %s denied. Status: %s", runID, resp.Status), nil)
	}
	return nil
}

// cmdManagedAgentStop handles /managed-agent stop <run_id>
func cmdManagedAgentStop(ctx *SlashContext, args []string) error {
	if len(args) == 0 {
		ctx.SendResult("Usage: /managed-agent stop <run_id>", nil)
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	runID := args[0]
	ctx.SetLoading(true)
	resp, err := ctx.APIClient.StopAgentRun(runID)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}

	if ctx.Format == "json" {
		b, _ := json.Marshal(resp)
		ctx.SendResult(string(b), nil)
	} else {
		ctx.SendResult(fmt.Sprintf("Run %s stopped. Status: %s", runID, resp.Status), nil)
	}
	return nil
}

// sseEvent is used to parse typed SSE event JSON payloads.
type sseEvent struct {
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
}

// cmdManagedAgentEvents handles /managed-agent events <run_id>.
// Streams SSE from the backend, printing each event as a JSON line.
// Exits with code 2 if an approval_needed event is received.
// Exits cleanly when session.status_idle is received.
func cmdManagedAgentEvents(ctx *SlashContext, args []string) error {
	if len(args) == 0 {
		ctx.SendResult("Usage: /managed-agent events <run_id>", nil)
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	runID := args[0]
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := ctx.APIClient.StreamAgentRunEvents(streamCtx, runID)
	if err != nil {
		return err
	}

	approvalNeeded := false
	for line := range ch {
		ctx.SendResult(line, nil)

		// Parse the event type to check for terminal states.
		var ev sseEvent
		if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr == nil {
			switch ev.Type {
			case "approval_needed":
				approvalNeeded = true
				cancel() // stop stream
			case "session.status_idle":
				cancel() // stop stream cleanly
			}
		}
	}

	if approvalNeeded {
		return &exitCodeError{code: 2, msg: "approval required for run " + runID}
	}
	return nil
}

// exitCodeError carries a non-zero exit code back to the dispatcher.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

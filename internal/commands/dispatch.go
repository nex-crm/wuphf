package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// CommandResult holds the output from a non-interactive command dispatch.
type CommandResult struct {
	Output   string
	Data     any
	ExitCode int
	Error    string
}

// DispatchWithService is like Dispatch but accepts an AgentService for commands
// that need access to running agents (e.g. /agents, /agent).
// DispatchWithService is like Dispatch but accepts an AgentService for commands
// that need access to running agents (e.g. /agents, /agent).
func DispatchWithService(input string, apiKey string, format string, timeout int, agentSvc *agent.AgentService) CommandResult {
	return dispatchInternal(input, apiKey, format, timeout, agentSvc)
}

// Dispatch parses input and runs the matching command non-interactively.
// format is "text" or "json"; timeout is in milliseconds (0 = default).
func Dispatch(input string, apiKey string, format string, timeout int) CommandResult {
	return dispatchInternal(input, apiKey, format, timeout, nil)
}

func dispatchInternal(input string, apiKey string, format string, timeout int, agentSvc *agent.AgentService) CommandResult {
	name, args, isSlash := ParseSlashInput(input)
	if !isSlash {
		// Treat plain text as /ask
		name = "ask"
		args = input
	}

	r := NewRegistry()
	RegisterAllCommands(r)

	cmd, ok := r.Get(name)
	if !ok {
		return CommandResult{
			Output:   fmt.Sprintf("Unknown command: /%s", name),
			ExitCode: 1,
			Error:    fmt.Sprintf("unknown command: %s", name),
		}
	}

	if cmd.Execute == nil {
		return CommandResult{
			Output: fmt.Sprintf("/%s — %s (not available in non-interactive mode)", cmd.Name, cmd.Description),
		}
	}

	cfg, _ := config.Load()
	client := api.NewClient(apiKey)
	if timeout > 0 {
		client.Timeout = time.Duration(timeout) * time.Millisecond
	}

	var output strings.Builder
	var execErr error

	ctx := &SlashContext{
		AgentService: agentSvc,
		APIClient:    client,
		Config:       &cfg,
		AddMessage: func(role, content string) {
			output.WriteString(content)
			output.WriteString("\n")
		},
		SetLoading:  func(bool) {},
		ShowPicker:  nil,
		ShowConfirm: nil,
		SendResult: func(out string, err error) {
			if out != "" {
				output.WriteString(out)
				output.WriteString("\n")
			}
			if err != nil {
				execErr = err
			}
		},
	}

	err := cmd.Execute(ctx, args)
	if err != nil {
		if err == ErrQuit {
			return CommandResult{Output: "", ExitCode: 0}
		}
		return CommandResult{
			Output:   output.String(),
			ExitCode: 1,
			Error:    err.Error(),
		}
	}
	if execErr != nil {
		return CommandResult{
			Output:   output.String(),
			ExitCode: 1,
			Error:    execErr.Error(),
		}
	}

	outStr := strings.TrimRight(output.String(), "\n")
	if format == "json" {
		payload := map[string]any{"output": outStr}
		b, _ := json.Marshal(payload)
		return CommandResult{Output: string(b), Data: payload}
	}
	return CommandResult{Output: outStr}
}

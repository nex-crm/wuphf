package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ClaudeStreamEvent is a normalized event emitted while parsing Claude stream-json output.
type ClaudeStreamEvent struct {
	Type      string
	Text      string
	ToolName  string
	ToolInput string
	ToolUseID string
	Detail    string
}

// ClaudeStreamResult captures the final outcome of a streamed Claude turn.
type ClaudeStreamResult struct {
	FinalMessage string
	LastError    string
}

// ReadClaudeJSONStream consumes Claude CLI stream-json output and normalizes it
// into text, thinking, tool, and error events.
func ReadClaudeJSONStream(r io.Reader, onEvent func(ClaudeStreamEvent)) (ClaudeStreamResult, error) {
	var result ClaudeStreamResult
	var text strings.Builder

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var msg claudeStreamMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "assistant":
			if msg.Message == nil {
				continue
			}
			for _, block := range msg.Message.Content {
				switch block.Type {
				case "thinking":
					if strings.TrimSpace(block.Thinking) == "" {
						continue
					}
					if onEvent != nil {
						onEvent(ClaudeStreamEvent{
							Type:   "thinking",
							Text:   block.Thinking,
							Detail: block.Thinking,
						})
					}
				case "text":
					if strings.TrimSpace(block.Text) == "" {
						continue
					}
					text.WriteString(block.Text)
					if onEvent != nil {
						onEvent(ClaudeStreamEvent{
							Type:   "text",
							Text:   block.Text,
							Detail: block.Text,
						})
					}
				case "tool_use":
					inputJSON, _ := json.Marshal(block.Input)
					if onEvent != nil {
						onEvent(ClaudeStreamEvent{
							Type:      "tool_use",
							ToolName:  block.Name,
							ToolUseID: block.ID,
							ToolInput: string(inputJSON),
							Detail:    strings.TrimSpace(block.Name),
						})
					}
				}
			}
		case "user":
			if msg.Message != nil {
				for _, block := range msg.Message.Content {
					if block.Type != "tool_result" {
						continue
					}
					resultStr := formatClaudeToolResult(block.Content)
					if onEvent != nil {
						onEvent(ClaudeStreamEvent{
							Type:      "tool_result",
							ToolUseID: block.ID,
							Text:      resultStr,
							Detail:    resultStr,
						})
					}
				}
			}
			if msg.ToolUseResult != nil && strings.TrimSpace(msg.ToolUseResult.Stdout) != "" && onEvent != nil {
				resultStr := truncateClaudeOutput(msg.ToolUseResult.Stdout)
				onEvent(ClaudeStreamEvent{
					Type:   "tool_result",
					Text:   resultStr,
					Detail: resultStr,
				})
			}
		case "result":
			if textOut := strings.TrimSpace(msg.Result); textOut != "" {
				result.FinalMessage = textOut
			}
			if errors := parseClaudeErrors(msg.Errors); len(errors) > 0 {
				result.LastError = strings.Join(errors, "; ")
				if onEvent != nil {
					onEvent(ClaudeStreamEvent{
						Type:   "error",
						Text:   result.LastError,
						Detail: result.LastError,
					})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read claude json stream: %w", err)
	}

	if strings.TrimSpace(result.FinalMessage) == "" {
		result.FinalMessage = strings.TrimSpace(text.String())
	}
	return result, nil
}

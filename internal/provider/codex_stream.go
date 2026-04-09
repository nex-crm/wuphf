package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// CodexStreamEvent is a normalized event emitted while parsing Codex JSONL output.
type CodexStreamEvent struct {
	Type      string
	RawType   string
	Text      string
	ToolName  string
	ToolInput string
	ToolUseID string
	Detail    string
}

// CodexStreamResult captures the final outcome of a streamed Codex turn.
type CodexStreamResult struct {
	FinalMessage  string
	LastPlainLine string
	LastError     string
}

type codexJSONEvent struct {
	Type        string                `json:"type"`
	Message     string                `json:"message,omitempty"`
	Delta       string                `json:"delta,omitempty"`
	Text        string                `json:"text,omitempty"`
	Name        string                `json:"name,omitempty"`
	Arguments   string                `json:"arguments,omitempty"`
	ItemID      string                `json:"item_id,omitempty"`
	ContentPart *codexJSONContentPart `json:"content_part,omitempty"`
	Error       *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	Item *codexJSONItem `json:"item,omitempty"`
}

type codexJSONItem struct {
	ID        string                 `json:"id,omitempty"`
	Type      string                 `json:"type,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	Content   []codexJSONContentPart `json:"content,omitempty"`
}

type codexJSONContentPart struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

type codexStreamState struct {
	deltaText           strings.Builder
	pendingTextBreak    bool
	completedMessages   []string
	completedMessageSet map[string]struct{}
	toolNames           map[string]string
	toolArgs            map[string]string
	toolStarted         map[string]struct{}
	toolFinished        map[string]struct{}
}

// ReadCodexJSONStream consumes Codex CLI JSONL output, normalizes streaming events, and
// reconstructs the best available final assistant message.
func ReadCodexJSONStream(r io.Reader, onEvent func(CodexStreamEvent)) (CodexStreamResult, error) {
	var result CodexStreamResult
	state := codexStreamState{
		completedMessageSet: make(map[string]struct{}),
		toolNames:           make(map[string]string),
		toolArgs:            make(map[string]string),
		toolStarted:         make(map[string]struct{}),
		toolFinished:        make(map[string]struct{}),
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event codexJSONEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			result.LastPlainLine = line
			continue
		}

		if detail := strings.TrimSpace(extractCodexError(event)); detail != "" {
			result.LastError = detail
			if onEvent != nil {
				onEvent(CodexStreamEvent{
					Type:    "error",
					RawType: event.Type,
					Text:    detail,
					Detail:  detail,
				})
			}
		}

		state.consumeToolEvent(event, onEvent)
		state.consumeTextDelta(event, onEvent)

		if text := strings.TrimSpace(extractCodexCompletedMessage(event)); text != "" {
			if _, seen := state.completedMessageSet[text]; !seen {
				state.completedMessageSet[text] = struct{}{}
				state.completedMessages = append(state.completedMessages, text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read codex json stream: %w", err)
	}

	result.FinalMessage = strings.TrimSpace(firstNonEmpty(
		strings.Join(state.completedMessages, "\n\n"),
		state.deltaText.String(),
		result.LastPlainLine,
	))
	return result, nil
}

func (s *codexStreamState) consumeTextDelta(event codexJSONEvent, onEvent func(CodexStreamEvent)) {
	text := strings.TrimSpace(extractCodexTextDelta(event))
	if text == "" {
		return
	}
	if s.pendingTextBreak && s.deltaText.Len() > 0 {
		s.deltaText.WriteString("\n\n")
		if onEvent != nil {
			onEvent(CodexStreamEvent{Type: "text", RawType: event.Type, Text: "\n\n"})
		}
		s.pendingTextBreak = false
	}
	s.deltaText.WriteString(text)
	if onEvent != nil {
		onEvent(CodexStreamEvent{Type: "text", RawType: event.Type, Text: text})
	}
}

func (s *codexStreamState) consumeToolEvent(event codexJSONEvent, onEvent func(CodexStreamEvent)) {
	itemID := strings.TrimSpace(firstNonEmpty(event.ItemID, itemIDFromEvent(event)))
	if strings.HasPrefix(event.Type, "response.function_call_arguments.") {
		if itemID == "" {
			itemID = "tool"
		}
		if delta := strings.TrimSpace(firstNonEmpty(event.Arguments, event.Delta)); delta != "" {
			s.toolArgs[itemID] += delta
		}
		name := strings.TrimSpace(firstNonEmpty(event.Name, s.toolNames[itemID], "function_call"))
		s.toolNames[itemID] = name
		if onEvent != nil {
			s.emitToolStarted(itemID, name, s.toolArgs[itemID], event.Type, onEvent)
		}
		if strings.HasSuffix(event.Type, ".done") && onEvent != nil {
			s.emitToolFinished(itemID, name, s.toolArgs[itemID], event.Type, onEvent)
		}
		return
	}

	if !eventHasToolItem(event) {
		return
	}

	name := strings.TrimSpace(firstNonEmpty(toolNameFromEvent(event), s.toolNames[itemID], "tool"))
	args := strings.TrimSpace(firstNonEmpty(toolArgumentsFromEvent(event), s.toolArgs[itemID]))
	if itemID == "" {
		itemID = name
	}
	s.toolNames[itemID] = name
	if args != "" {
		s.toolArgs[itemID] = args
	}

	if onEvent != nil {
		s.emitToolStarted(itemID, name, s.toolArgs[itemID], event.Type, onEvent)
		if strings.HasSuffix(event.Type, ".done") || event.Type == "item.completed" {
			s.emitToolFinished(itemID, name, s.toolArgs[itemID], event.Type, onEvent)
		}
	}
}

func (s *codexStreamState) emitToolStarted(itemID, name, args, rawType string, onEvent func(CodexStreamEvent)) {
	if _, seen := s.toolStarted[itemID]; seen {
		return
	}
	s.toolStarted[itemID] = struct{}{}
	if s.deltaText.Len() > 0 {
		s.pendingTextBreak = true
	}
	onEvent(CodexStreamEvent{
		Type:      "tool_use",
		RawType:   rawType,
		ToolName:  name,
		ToolInput: strings.TrimSpace(args),
		ToolUseID: itemID,
	})
}

func (s *codexStreamState) emitToolFinished(itemID, name, args, rawType string, onEvent func(CodexStreamEvent)) {
	if _, seen := s.toolFinished[itemID]; seen {
		return
	}
	s.toolFinished[itemID] = struct{}{}
	s.pendingTextBreak = true

	summary := strings.TrimSpace(name + " completed")
	if trimmedArgs := strings.TrimSpace(args); trimmedArgs != "" {
		summary += ": " + truncateCodexEventText(trimmedArgs, 160)
	}
	onEvent(CodexStreamEvent{
		Type:      "tool_result",
		RawType:   rawType,
		Text:      summary,
		ToolName:  name,
		ToolInput: strings.TrimSpace(args),
		ToolUseID: itemID,
	})
}

func extractCodexCompletedMessage(event codexJSONEvent) string {
	switch event.Type {
	case "response.output_text.done":
		return strings.TrimSpace(firstNonEmpty(event.Text, event.Delta, textFromContentPart(event.ContentPart)))
	case "response.output_item.done", "item.completed":
		if event.Item == nil || !isCodexTextItemType(event.Item.Type) {
			return ""
		}
		return extractCodexTextFromItem(*event.Item)
	default:
		return ""
	}
}

func extractCodexTextDelta(event codexJSONEvent) string {
	switch event.Type {
	case "response.output_text.delta":
		return strings.TrimSpace(firstNonEmpty(event.Delta, event.Text, textFromContentPart(event.ContentPart)))
	default:
		return ""
	}
}

func extractCodexTextFromItem(item codexJSONItem) string {
	if text := strings.TrimSpace(item.Text); text != "" {
		return text
	}
	parts := make([]string, 0, len(item.Content))
	for _, part := range item.Content {
		if part.Type == "output_text" || part.Type == "text" {
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractCodexError(event codexJSONEvent) string {
	switch event.Type {
	case "error", "turn.failed", "response.failed":
		if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
			return event.Error.Message
		}
		if strings.TrimSpace(event.Message) != "" {
			return event.Message
		}
	}
	return ""
}

func eventHasToolItem(event codexJSONEvent) bool {
	return event.Item != nil && isCodexToolItemType(event.Item.Type)
}

func itemIDFromEvent(event codexJSONEvent) string {
	if event.Item == nil {
		return ""
	}
	return strings.TrimSpace(event.Item.ID)
}

func toolNameFromEvent(event codexJSONEvent) string {
	if event.Item == nil {
		return strings.TrimSpace(event.Name)
	}
	return strings.TrimSpace(firstNonEmpty(event.Item.Name, event.Name, event.Item.Type))
}

func toolArgumentsFromEvent(event codexJSONEvent) string {
	if event.Item == nil {
		return strings.TrimSpace(firstNonEmpty(event.Arguments, event.Delta))
	}
	return strings.TrimSpace(firstNonEmpty(event.Item.Arguments, event.Arguments, event.Delta))
}

func textFromContentPart(part *codexJSONContentPart) string {
	if part == nil {
		return ""
	}
	return strings.TrimSpace(part.Text)
}

func isCodexTextItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "agent_message", "message", "assistant", "output_text":
		return true
	default:
		return false
	}
}

func isCodexToolItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call", "tool_call", "computer_call", "custom_tool_call":
		return true
	default:
		return false
	}
}

func truncateCodexEventText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

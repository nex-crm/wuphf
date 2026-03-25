package commands

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseFlags splits "pos1 pos2 --key value --bool" into positional args and named flags.
// Handles quoted values: --data '{"key":"value"}'
func parseFlags(args string) (positional []string, flags map[string]string) {
	flags = make(map[string]string)
	tokens := tokenize(args)
	for i := 0; i < len(tokens); i++ {
		if strings.HasPrefix(tokens[i], "--") {
			key := strings.TrimPrefix(tokens[i], "--")
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") {
				flags[key] = tokens[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		} else {
			positional = append(positional, tokens[i])
		}
	}
	return
}

// tokenize splits on whitespace, respecting single-quoted strings.
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '\'' && !inQuote:
			inQuote = true
		case r == '\'' && inQuote:
			inQuote = false
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// parseData extracts --data flag and parses as JSON map.
func parseData(flags map[string]string) (map[string]any, error) {
	raw, ok := flags["data"]
	if !ok {
		return nil, fmt.Errorf("--data flag required")
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("invalid JSON in --data: %w", err)
	}
	return data, nil
}

// formatTable renders headers + rows as aligned text.
func formatTable(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return "(no results)"
	}
	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	// Render
	var sb strings.Builder
	for i, h := range headers {
		sb.WriteString(fmt.Sprintf("%-*s  ", widths[i], h))
	}
	sb.WriteString("\n")
	for i := range headers {
		sb.WriteString(strings.Repeat("─", widths[i]))
		sb.WriteString("  ")
	}
	sb.WriteString("\n")
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				sb.WriteString(fmt.Sprintf("%-*s  ", widths[i], cell))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatJSON pretty-prints any value.
func formatJSON(data any) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(b)
}

// getFlag returns a flag value or empty string.
func getFlag(flags map[string]string, key string) string {
	return flags[key]
}

// getFlagOr returns a flag value or the default.
func getFlagOr(flags map[string]string, key, def string) string {
	if v, ok := flags[key]; ok {
		return v
	}
	return def
}

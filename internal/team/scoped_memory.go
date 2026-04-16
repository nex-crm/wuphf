package team

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	privateMemoryScope = "private"
	sharedMemoryScope  = "shared"
)

type privateMemoryNote struct {
	Key       string `json:"key"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type brokerMemoryEntry struct {
	Key       string `json:"key"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func privateMemoryNamespace(slug string) string {
	return "agent/" + strings.TrimSpace(slug)
}

func encodePrivateMemoryNote(note privateMemoryNote) string {
	note.Key = strings.TrimSpace(note.Key)
	note.Title = strings.TrimSpace(note.Title)
	note.Content = strings.TrimSpace(note.Content)
	note.Author = strings.TrimSpace(note.Author)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(note.CreatedAt) == "" {
		note.CreatedAt = now
	}
	note.UpdatedAt = now
	data, err := json.Marshal(note)
	if err != nil {
		return note.Content
	}
	return string(data)
}

func decodePrivateMemoryNote(key string, raw string) privateMemoryNote {
	key = strings.TrimSpace(key)
	raw = strings.TrimSpace(raw)
	note := privateMemoryNote{
		Key:     key,
		Content: raw,
		Title:   key,
	}
	if raw == "" {
		return note
	}
	var decoded privateMemoryNote
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return note
	}
	if strings.TrimSpace(decoded.Key) == "" {
		decoded.Key = key
	}
	if strings.TrimSpace(decoded.Title) == "" {
		decoded.Title = decoded.Key
	}
	if strings.TrimSpace(decoded.Content) == "" {
		decoded.Content = raw
	}
	return decoded
}

func brokerEntryFromNote(note privateMemoryNote) brokerMemoryEntry {
	return brokerMemoryEntry{
		Key:       note.Key,
		Title:     note.Title,
		Content:   note.Content,
		Author:    note.Author,
		CreatedAt: note.CreatedAt,
		UpdatedAt: note.UpdatedAt,
	}
}

func searchPrivateMemory(entries map[string]string, query string, limit int) []privateMemoryNote {
	if limit <= 0 {
		limit = 5
	}
	query = strings.TrimSpace(strings.ToLower(query))
	type scoredNote struct {
		note  privateMemoryNote
		score int
	}
	notes := make([]scoredNote, 0, len(entries))
	for key, raw := range entries {
		note := decodePrivateMemoryNote(key, raw)
		haystack := normalizeMemorySearchText(strings.Join([]string{note.Key, note.Title, note.Content}, "\n"))
		score := privateMemoryMatchScore(haystack, query)
		if query != "" && score == 0 {
			continue
		}
		notes = append(notes, scoredNote{note: note, score: score})
	}
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].score != notes[j].score {
			return notes[i].score > notes[j].score
		}
		return noteTimestamp(notes[i].note).After(noteTimestamp(notes[j].note))
	})
	if len(notes) > limit {
		notes = notes[:limit]
	}
	out := make([]privateMemoryNote, 0, len(notes))
	for _, item := range notes {
		out = append(out, item.note)
	}
	return out
}

func noteTimestamp(note privateMemoryNote) time.Time {
	for _, candidate := range []string{note.UpdatedAt, note.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(candidate)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func slugify(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeMemorySearchText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func privateMemoryMatchScore(haystack string, query string) int {
	if query == "" {
		return 1
	}
	query = normalizeMemorySearchText(query)
	if query == "" {
		return 1
	}
	score := 0
	if strings.Contains(haystack, query) {
		score += 100
	}
	for _, token := range strings.Fields(query) {
		if strings.Contains(haystack, token) {
			score += 10
		}
	}
	return score
}

func formatPrivateMemoryBrief(slug string, entries map[string]string, query string) string {
	if strings.TrimSpace(slug) == "" || len(entries) == 0 {
		return ""
	}
	matches := searchPrivateMemory(entries, query, 2)
	if len(matches) == 0 {
		return ""
	}
	lines := []string{"== PRIVATE MEMORY =="}
	for _, note := range matches {
		title := strings.TrimSpace(note.Title)
		if title == "" {
			title = strings.TrimSpace(note.Key)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", title, truncate(strings.TrimSpace(strings.ReplaceAll(note.Content, "\n", " ")), 220)))
	}
	lines = append(lines, "== END PRIVATE MEMORY ==")
	return strings.Join(lines, "\n")
}

func fetchScopedMemoryBrief(ctx context.Context, slug string, notification string, broker *Broker) string {
	query := strings.TrimSpace(notification)
	if query == "" {
		return ""
	}
	var blocks []string
	if broker != nil {
		broker.mu.Lock()
		entries := map[string]string{}
		if broker.sharedMemory != nil {
			if stored := broker.sharedMemory[privateMemoryNamespace(slug)]; stored != nil {
				entries = make(map[string]string, len(stored))
				for key, value := range stored {
					entries[key] = value
				}
			}
		}
		broker.mu.Unlock()
		if brief := formatPrivateMemoryBrief(slug, entries, query); brief != "" {
			blocks = append(blocks, brief)
		}
	}
	if brief := fetchMemoryBrief(ctx, notification); brief != "" {
		blocks = append(blocks, brief)
	}
	return strings.Join(blocks, "\n\n")
}

package team

// wiki_obsidian_bootstrap.go writes a minimal .obsidian/ config inside the
// vault root so the wiki opens cleanly when pointed at by Obsidian. See
// docs/specs/WIKI-OBSIDIAN-COMPATIBILITY.md §4 for the contract.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// obsidianRequiredAppKeys are the keys WUPHF owns inside .obsidian/app.json.
// On every bootstrap these values are written verbatim; user customisation of
// these keys is intentionally overridden so the vault keeps matching the
// wikilink contract in WIKI-OBSIDIAN-COMPATIBILITY.md §5.
var obsidianRequiredAppKeys = map[string]any{
	"useMarkdownLinks":     false,
	"newLinkFormat":        "absolute",
	"alwaysUpdateLinks":    false,
	"attachmentFolderPath": "inbox/raw",
	"userIgnoreFilters":    []any{"playbooks/.compiled/", "entities/.graph.jsonl"},
}

// obsidianGitignoreEntries are the user-specific files that must never be
// committed to the wiki repo. Listed in .obsidian/.gitignore.
var obsidianGitignoreEntries = []string{
	"workspace.json",
	"workspace-mobile.json",
	"graph.json",
}

// ensureObsidianVaultLocked writes .obsidian/app.json and .obsidian/.gitignore
// at the vault root (<wiki-root>/team/). Idempotent: existing app.json is
// merged so WUPHF-owned keys cannot drift while user customisations (theme,
// hotkeys, plugins, etc.) survive.
//
// Caller must hold r.mu.
func (r *Repo) ensureObsidianVaultLocked() error {
	dir := filepath.Join(r.root, "team", ".obsidian")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("wiki: mkdir %s: %w", dir, err)
	}
	if err := writeObsidianAppJSON(filepath.Join(dir, "app.json")); err != nil {
		return err
	}
	if err := writeObsidianGitignore(filepath.Join(dir, ".gitignore")); err != nil {
		return err
	}
	return nil
}

// writeObsidianAppJSON reads any existing app.json, merges WUPHF's required
// keys over the top (WUPHF wins on required keys, user wins on everything
// else), and rewrites the file. A missing file is bootstrapped with just the
// required keys. A corrupted file surfaces as an error rather than being
// silently overwritten — see the failure-mode note in the spec.
func writeObsidianAppJSON(path string) error {
	merged := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &merged); err != nil {
				return fmt.Errorf("wiki: parse existing %s: %w", path, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("wiki: read %s: %w", path, err)
	}

	for k, v := range obsidianRequiredAppKeys {
		merged[k] = v
	}

	out, err := marshalStableJSON(merged)
	if err != nil {
		return fmt.Errorf("wiki: marshal app.json: %w", err)
	}

	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, out) {
		return nil
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("wiki: write %s: %w", path, err)
	}
	return nil
}

// writeObsidianGitignore writes the user-specific exclusion list. If the file
// already exists and contains all required entries, it is left byte-for-byte
// alone. If it exists but is missing entries, the missing entries are
// appended — set semantics, never duplicating.
func writeObsidianGitignore(path string) error {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		// Continue to merge below.
	case os.IsNotExist(err):
		existing = nil
	default:
		return fmt.Errorf("wiki: read %s: %w", path, err)
	}

	present := map[string]bool{}
	var lines []string
	if len(existing) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(existing))
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				present[trimmed] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("wiki: scan %s: %w", path, err)
		}
	}

	var missing []string
	for _, entry := range obsidianGitignoreEntries {
		if !present[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 && existing != nil {
		return nil
	}

	if existing == nil {
		var buf bytes.Buffer
		for _, entry := range obsidianGitignoreEntries {
			buf.WriteString(entry)
			buf.WriteByte('\n')
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			return fmt.Errorf("wiki: write %s: %w", path, err)
		}
		return nil
	}

	var buf bytes.Buffer
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		// Trailing newline already covered by the loop above; nothing extra.
	}
	for _, entry := range missing {
		buf.WriteString(entry)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("wiki: write %s: %w", path, err)
	}
	return nil
}

// marshalStableJSON renders m with sorted keys and indentation so re-runs
// produce byte-identical output when the logical content is unchanged.
func marshalStableJSON(m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		valJSON, err := json.MarshalIndent(m[k], "  ", "  ")
		if err != nil {
			return nil, err
		}
		buf.WriteString("  ")
		buf.Write(keyJSON)
		buf.WriteString(": ")
		buf.Write(valJSON)
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

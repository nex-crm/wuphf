// Package onboarding manages first-run state, prerequisite detection,
// task templates, and the HTTP handlers that power the onboarding UI.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// currentStateVersion is the schema version written to onboarded.json.
// A file with a different version is treated as not-yet-onboarded so the
// user goes through the flow again after a breaking upgrade.
const currentStateVersion = 1

// State mirrors the full contents of ~/.wuphf/onboarded.json.
type State struct {
	// CompletedAt is the RFC-3339 timestamp of when the user finished onboarding.
	// Empty string means onboarding is not complete.
	CompletedAt string `json:"completed_at,omitempty"`

	// Version is the schema version of the file. Used to invalidate stale state
	// after breaking changes.
	Version int `json:"version"`

	// CompanyName is the canonical company name captured during onboarding.
	CompanyName string `json:"company_name,omitempty"`

	// CompletedSteps lists the step IDs the user has finished.
	CompletedSteps []string `json:"completed_steps,omitempty"`

	// ChecklistDismissed is true when the user has closed the post-onboarding
	// checklist permanently.
	ChecklistDismissed bool `json:"checklist_dismissed"`

	// Partial holds in-progress answers when the user has not finished onboarding.
	Partial *PartialProgress `json:"partial,omitempty"`

	// Checklist is the list of post-onboarding action items.
	Checklist []ChecklistItem `json:"checklist,omitempty"`
}

// Onboarded reports whether the user has successfully completed onboarding.
// Returns false when the file is missing, the version has changed, or
// CompletedAt is empty.
func (s *State) Onboarded() bool {
	return s.Version == currentStateVersion && s.CompletedAt != ""
}

// PartialProgress captures answers the user has submitted so far while
// stepping through the multi-step onboarding flow.
type PartialProgress struct {
	// Step is the ID of the step the user is currently on.
	Step string `json:"step,omitempty"`

	// Answers maps step IDs to the free-form answers submitted for that step.
	Answers map[string]map[string]interface{} `json:"answers,omitempty"`
}

// ChecklistItem is a single post-onboarding action item shown in the UI
// until the user completes or dismisses the checklist.
type ChecklistItem struct {
	// ID is the stable identifier for this item (e.g. "pick_team").
	ID string `json:"id"`

	// Done is true when the user has marked this item complete.
	Done bool `json:"done"`
}

// StatePath returns the absolute path to ~/.wuphf/onboarded.json.
// It expands $HOME via os.UserHomeDir; falls back to a relative path on
// error (only occurs in extremely restricted environments).
func StatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "onboarded.json")
	}
	return filepath.Join(home, ".wuphf", "onboarded.json")
}

// Load reads and parses ~/.wuphf/onboarded.json.
// When the file does not exist it returns a fresh State with Onboarded()==false
// and a default checklist — no error is returned in that case.
func Load() (*State, error) {
	data, err := os.ReadFile(StatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{
				Version:   currentStateVersion,
				Checklist: DefaultChecklist(),
			}, nil
		}
		return nil, fmt.Errorf("onboarding: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("onboarding: parse state: %w", err)
	}
	// If the file was written by an older schema version, return a fresh state
	// so the user re-runs onboarding rather than hitting subtle bugs.
	if s.Version != currentStateVersion {
		return &State{
			Version:   currentStateVersion,
			Checklist: DefaultChecklist(),
		}, nil
	}
	// Back-fill checklist when the field was never written (e.g. old file).
	if len(s.Checklist) == 0 {
		s.Checklist = DefaultChecklist()
	}
	return &s, nil
}

// Save atomically writes s to ~/.wuphf/onboarded.json by first writing to a
// sibling temp file and then renaming it into place.
func Save(s *State) error {
	path := StatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("onboarding: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("onboarding: marshal state: %w", err)
	}
	data = append(data, '\n')

	// Write to a temp file in the same directory so the rename is atomic.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".onboarded-*.json")
	if err != nil {
		return fmt.Errorf("onboarding: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		// Best-effort cleanup of the temp file if something goes wrong.
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("onboarding: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("onboarding: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("onboarding: rename temp: %w", err)
	}
	return nil
}

// SaveProgress loads the current state, updates the partial-progress record
// for the given step, and saves it back atomically.
func SaveProgress(step string, answers map[string]interface{}) error {
	s, err := Load()
	if err != nil {
		return err
	}
	if s.Partial == nil {
		s.Partial = &PartialProgress{}
	}
	s.Partial.Step = step
	if s.Partial.Answers == nil {
		s.Partial.Answers = make(map[string]map[string]interface{})
	}
	s.Partial.Answers[step] = answers
	return Save(s)
}

// MarkChecklistItem loads the current state, sets the Done flag on the item
// with the given id, and saves. Unknown IDs are silently ignored.
func MarkChecklistItem(id string, done bool) error {
	s, err := Load()
	if err != nil {
		return err
	}
	for i := range s.Checklist {
		if s.Checklist[i].ID == id {
			s.Checklist[i].Done = done
			break
		}
	}
	return Save(s)
}

// DismissChecklist loads the current state, sets ChecklistDismissed=true,
// and saves.
func DismissChecklist() error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.ChecklistDismissed = true
	return Save(s)
}

// DefaultChecklist returns the canonical ordered list of post-onboarding
// action items. These are the five items shown in the Getting-Started panel.
func DefaultChecklist() []ChecklistItem {
	return []ChecklistItem{
		{ID: "pick_team", Done: false},
		{ID: "second_key", Done: false},
		{ID: "github_repo", Done: false},
		{ID: "github_star", Done: false},
		{ID: "discord", Done: false},
	}
}

// completeState builds a State that represents a fully-onboarded user.
// The caller must still call Save to persist it.
func completeState(s *State, companyName string) {
	s.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	s.Version = currentStateVersion
	if companyName != "" {
		s.CompanyName = companyName
	}
	s.Partial = nil
}

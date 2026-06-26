// Package onboarding manages first-run state, prerequisite detection,
// task templates, and the HTTP handlers that power the onboarding UI.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// currentStateVersion is the schema version written to onboarded.json.
// Bump to 2 for the chat-mode Phase 2 additions (Phase, FormAnswers,
// PendingSuggestion, CEODMChannelID, FirstIssueApprovedAt).
// v1 files are migrated on read via migrateV1ToV2.
const currentStateVersion = 2

// Phase constants for the deterministic CEO conversation state machine.
// Phase 4 (LLM) phases (draft/approve/kickoff) are defined here so the
// state type knows the full legal set, even though Phase 2 only wires
// greet→bridge.
const (
	PhaseGreet     = "greet"
	PhaseIdentity  = "identity"
	PhaseWebsite   = "website"
	PhaseScan      = "scan"
	PhaseBlueprint = "blueprint"
	PhaseTeam      = "team"
	PhaseSeed      = "seed"
	PhaseBridge    = "bridge"
	PhaseDraft     = "draft"
	PhaseApprove   = "approve"
	PhaseKickoff   = "kickoff"
	PhaseComplete  = "complete"
)

// CEOOnboardingDMSlug is the reserved channel slug for the CEO onboarding DM.
// The CEO transcript lives in b.messages under this channel — not in state.
const CEOOnboardingDMSlug = "dm:ceo:onboarding"

// FormAnswers holds the staged deterministic form answers collected during
// the onboarding conversation. Fields are committed incrementally via
// POST /onboarding/answer; the atomic office seed runs once at the seed
// phase boundary.
type FormAnswers struct {
	CompanyName  string   `json:"company_name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	WebsiteURL   string   `json:"website_url,omitempty"`
	OwnerName    string   `json:"owner_name,omitempty"`
	OwnerRole    string   `json:"owner_role,omitempty"`
	OwnerEmail   string   `json:"owner_email,omitempty"`  // captured in onboarding; PII, stored locally
	BlueprintID  string   `json:"blueprint_id,omitempty"` // empty = scratch path
	PickedAgents []string `json:"picked_agents,omitempty"`
	ScanComplete bool     `json:"scan_complete,omitempty"`
	TaskPrompt   string   `json:"task_prompt,omitempty"`
}

// Suggestion is an idempotent re-emittable CEO message card. The ID is
// stable per (phase, options-hash) so a crash-recovery resume can re-emit
// the same card without creating duplicates (the frontend dedupes by ID).
type Suggestion struct {
	ID       string          `json:"id"`
	Phase    string          `json:"phase"`
	Kind     string          `json:"kind"` // ceo_form_field | ceo_chip_row | ceo_checklist | ceo_team_trim | ceo_scan_chip
	Payload  json.RawMessage `json:"payload"`
	IssuedAt time.Time       `json:"issued_at"`
}

// State mirrors the full contents of ~/.wuphf/onboarded.json.
type State struct {
	// v1 fields — preserved for back-compat. v1 callers still read these.

	// CompletedAt is the RFC-3339 timestamp of when the user finished onboarding.
	// Empty string means onboarding is not complete. Set at end of bridge phase
	// for both "start an issue" and "look around first" paths.
	CompletedAt string `json:"completed_at,omitempty"`

	// Version is the schema version of the file. Used for migrations.
	Version int `json:"version"`

	// CompanyName is the canonical company name captured during onboarding.
	CompanyName string `json:"company_name,omitempty"`

	// CompletedSteps lists the step IDs the user has finished (legacy wizard).
	CompletedSteps []string `json:"completed_steps,omitempty"`

	// ChecklistDismissed is true when the user has closed the post-onboarding
	// checklist permanently.
	ChecklistDismissed bool `json:"checklist_dismissed"`

	// Partial holds in-progress answers when the user has not finished onboarding
	// via the legacy wizard path.
	Partial *PartialProgress `json:"partial,omitempty"`

	// Checklist is the list of post-onboarding action items.
	Checklist []ChecklistItem `json:"checklist,omitempty"`

	// v2 chat-mode additions (Phase 2+).

	// Phase is the current phase cursor in the deterministic CEO conversation
	// state machine. Legal values: greet | identity | website | scan |
	// blueprint | team | seed | bridge | draft | approve | kickoff | complete.
	Phase string `json:"phase,omitempty"`

	// CEODMChannelID is the reserved channel slug for the CEO onboarding DM
	// (dm:ceo:onboarding). The CEO transcript lives in b.messages — not here.
	CEODMChannelID string `json:"ceo_dm_channel_id,omitempty"`

	// PendingSuggestion is the last CEO suggestion card emitted that the user
	// has not yet acknowledged. On resume, if non-nil, it is re-emitted into
	// the DM (idempotent by Suggestion.ID). Cleared by POST /onboarding/suggestion/ack.
	PendingSuggestion *Suggestion `json:"pending_suggestion,omitempty"`

	// FormAnswers holds staged deterministic form answers. Committed
	// incrementally via POST /onboarding/answer; the atomic seed runs once at
	// the seed phase boundary.
	FormAnswers FormAnswers `json:"form_answers,omitempty"`

	// FirstIssueID is the ID of the first issue created after onboarding.
	FirstIssueID string `json:"first_issue_id,omitempty"`

	// FirstIssueApprovedAt is the RFC-3339 timestamp of when the first issue
	// was approved. Distinct from CompletedAt — used for activation-depth
	// tracking. Marcus path (look around first) sets CompletedAt but not this.
	FirstIssueApprovedAt *time.Time `json:"first_issue_approved_at,omitempty"`
}

// Onboarded reports whether the user has successfully completed onboarding.
// Back-compat: returns true for v1 files (CompletedAt set) and for v2 files
// (Phase == "complete" OR CompletedAt set).
func (s *State) Onboarded() bool {
	if s.Version == currentStateVersion {
		return s.CompletedAt != "" || s.Phase == PhaseComplete
	}
	// v1: only CompletedAt matters (version check already handled by Load).
	return s.CompletedAt != ""
}

// Activated reports whether the user has completed onboarding AND had their
// first issue approved. Marcus path (look around first) is onboarded but not
// activated.
func (s *State) Activated() bool {
	return s.Onboarded() && s.FirstIssueApprovedAt != nil
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
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "onboarded.json")
	}
	return filepath.Join(home, ".wuphf", "onboarded.json")
}

// Load reads and parses ~/.wuphf/onboarded.json.
// When the file does not exist it returns a fresh State with Onboarded()==false
// and a default checklist — no error is returned in that case.
// v1 files are migrated in-memory to v2 (Phase = "complete", FormAnswers empty)
// so existing onboarded users are not forced back through onboarding.
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
		log.Printf("onboarding: corrupt state file, resetting to fresh state: %v", err)
		fresh := &State{
			Version:   currentStateVersion,
			Checklist: DefaultChecklist(),
		}
		// Persist the recovered state so the next Load reads valid JSON
		// instead of triggering this branch again on every call. Best-
		// effort: a write failure here just means we'll log+recover again
		// next time, which is the same as the pre-fix behavior.
		if writeErr := Save(fresh); writeErr != nil {
			log.Printf("onboarding: failed to overwrite corrupt state file: %v", writeErr)
		}
		return fresh, nil
	}
	// v1 → v2 migration: a completed v1 user should not be forced through
	// onboarding again. Migrate in-memory and persist the upgraded state so
	// subsequent Loads are clean.
	if s.Version == 1 {
		migrateV1ToV2(&s)
		// Best-effort persist. A failure here just means the migration runs
		// again on the next Load, which is harmless.
		if writeErr := Save(&s); writeErr != nil {
			log.Printf("onboarding: v1→v2 migration persist failed: %v", writeErr)
		}
		// Back-fill checklist when the field was never written.
		if len(s.Checklist) == 0 {
			s.Checklist = DefaultChecklist()
		}
		return &s, nil
	}
	// Version mismatch (neither v1 nor v2): return fresh state so the user
	// re-runs onboarding rather than hitting subtle bugs.
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
	if recoverUnwiredPhase(&s) {
		if writeErr := Save(&s); writeErr != nil {
			log.Printf("onboarding: unwired phase recovery persist failed: %v", writeErr)
		}
	}
	return &s, nil
}

func recoverUnwiredPhase(s *State) bool {
	if s == nil || s.CompletedAt != "" {
		return false
	}
	switch s.Phase {
	case PhaseDraft, PhaseApprove, PhaseKickoff:
	default:
		return false
	}
	log.Printf("onboarding: recovering from unwired phase %q by completing onboarding", s.Phase)
	s.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	s.Phase = PhaseComplete
	s.PendingSuggestion = nil
	s.Version = currentStateVersion
	return true
}

// migrateV1ToV2 upgrades a v1 State in-place to v2.
// Rules:
//   - Version bumped to 2.
//   - If CompletedAt is set (the user already onboarded via the wizard), Phase
//     is set to "complete" so Onboarded() returns true under the new logic.
//   - FormAnswers is left zero — the v2 staged answers were never collected.
//   - PendingSuggestion, CEODMChannelID, FirstIssueApprovedAt left nil/zero.
//   - All other fields preserved verbatim.
func migrateV1ToV2(s *State) {
	s.Version = currentStateVersion
	if s.CompletedAt != "" && s.Phase == "" {
		s.Phase = PhaseComplete
	}
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
	if s.Onboarded() {
		return nil
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
// Sets CompletedAt + Phase = "complete" so both v1 and v2 callers agree.
// The caller must still call Save to persist it.
func completeState(s *State, companyName string) {
	s.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	s.Version = currentStateVersion
	s.Phase = PhaseComplete
	if companyName != "" {
		s.CompanyName = companyName
	}
	s.Partial = nil
}

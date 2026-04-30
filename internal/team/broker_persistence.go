package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/config"
)

// State persistence: the broker writes a JSON snapshot of every
// non-trivial entity to ~/.wuphf/team/broker-state.json (overridable
// via WUPHF_BROKER_STATE_PATH for tests). On restart it loads the
// snapshot and replays state into the in-memory broker.
//
// Two files: <path> (current state) and <path>.last-good (snapshot of
// the last save where activity-score > 0). On load we pick whichever
// has the higher activity-score, defending against a corrupted current
// file overwriting a healthy snapshot.
//
// atomicWriteFile uses os.CreateTemp to generate a unique sibling tmp
// filename + rename — concurrent saves to the same path cannot race on
// a fixed `.tmp` source path. See broker_save_race_test.go for the
// regression repro.

func defaultBrokerStatePath() string {
	// Env override lets probes and test harnesses isolate broker state from
	// the user's real ~/.wuphf/team/ dir without needing to remap HOME (which
	// breaks macOS keychain-backed auth for bundled CLIs like Claude Code).
	if p := strings.TrimSpace(os.Getenv("WUPHF_BROKER_STATE_PATH")); p != "" {
		return p
	}
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "broker-state.json")
	}
	return filepath.Join(home, ".wuphf", "team", "broker-state.json")
}

// stateSnapshotPath returns the path the Broker writes its last-good
// crash-recovery snapshot to. Bound to b.statePath (set at construction).
func (b *Broker) stateSnapshotPath() string {
	return b.statePath + ".last-good"
}

func loadBrokerStateFile(path string) (brokerState, error) {
	var state brokerState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func brokerStateActivityScore(state brokerState) int {
	score := 0
	score += len(state.Messages) * 10
	score += len(state.AgentIssues) * 8
	score += len(state.Tasks) * 20
	score += len(activeRequests(state.Requests)) * 10
	score += len(state.Actions) * 4
	score += len(state.Signals) * 4
	score += len(state.Decisions) * 4
	score += len(state.Skills) * 2
	score += len(state.Policies)
	for _, ns := range state.SharedMemory {
		score += len(ns)
	}
	if state.PendingInterview != nil {
		score += 5
	}
	return score
}

func brokerStateShouldSnapshot(state brokerState) bool {
	return brokerStateActivityScore(state) > 0
}

func (b *Broker) loadState() error {
	if b.statePath == "" {
		// Direct &Broker{} literal in a unit test that exercises only
		// in-memory logic — no file to load from. Treat as a fresh
		// no-state broker and let the caller proceed.
		return nil
	}
	path := b.statePath
	state, err := loadBrokerStateFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		state = brokerState{}
	}
	snapshotPath := b.stateSnapshotPath()
	if snapshot, snapErr := loadBrokerStateFile(snapshotPath); snapErr == nil {
		if brokerStateActivityScore(snapshot) > brokerStateActivityScore(state) {
			state = snapshot
		}
	}
	b.messages = state.Messages
	b.agentIssues = state.AgentIssues
	b.members = state.Members
	b.channels = state.Channels
	b.sessionMode = state.SessionMode
	b.oneOnOneAgent = state.OneOnOneAgent
	b.focusMode = state.FocusMode
	b.tasks = state.Tasks
	b.requests = state.Requests
	b.actions = state.Actions
	b.signals = state.Signals
	b.decisions = state.Decisions
	b.watchdogs = state.Watchdogs
	b.policies = state.Policies
	b.scheduler = state.Scheduler
	b.skills = state.Skills
	b.sharedMemory = state.SharedMemory
	b.counter = state.Counter
	b.notificationSince = state.NotificationSince
	b.insightsSince = state.InsightsSince
	b.pendingInterview = state.PendingInterview
	b.usage = state.Usage
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	b.usage.Session = usageTotals{}
	if len(b.requests) == 0 && b.pendingInterview != nil {
		b.requests = []humanInterview{*b.pendingInterview}
	}
	// Load channel store: if present, unmarshal it.
	// Legacy states without channel_store start with an empty store; DMs are created on demand.
	if len(state.ChannelStore) > 0 {
		if err := json.Unmarshal(state.ChannelStore, b.channelStore); err != nil {
			return fmt.Errorf("unmarshal channel_store: %w", err)
		}
		b.channelStore.MigrateLegacyDM()
	}
	// Migrate channel refs from dm-* to deterministic pair slugs across all entities.
	// Messages are the primary data loss risk: legacy Channel:"dm-engineering" would not
	// match Store lookups keyed by "engineering__human".
	for i := range b.messages {
		b.messages[i].Channel = channel.MigrateDMSlugString(b.messages[i].Channel)
	}
	for i := range b.tasks {
		b.tasks[i].Channel = channel.MigrateDMSlugString(b.tasks[i].Channel)
	}
	for i := range b.requests {
		b.requests[i].Channel = channel.MigrateDMSlugString(b.requests[i].Channel)
	}
	// b.ensureDefaultChannelsLocked() // channels come from saved state
	b.ensureDefaultOfficeMembersLocked()
	b.normalizeLoadedStateLocked()
	return nil
}

func (b *Broker) saveLocked() error {
	if b.statePath == "" {
		// A direct &Broker{} literal (no NewBrokerAt/NewBroker) reaching the
		// persistence path means a test wired in-memory state but accidentally
		// triggered a save — without this guard the empty path would silently
		// resolve to "" + cwd-adjacent files. Fail loudly so the caller fixes
		// the construction site instead of corrupting the test workdir.
		return errors.New("broker: saveLocked requires a non-empty statePath; construct via NewBrokerAt(path)")
	}
	path := b.statePath
	snapshotPath := b.stateSnapshotPath()
	if len(b.messages) == 0 && len(b.agentIssues) == 0 && len(b.tasks) == 0 && len(activeRequests(b.requests)) == 0 && len(b.actions) == 0 && len(b.signals) == 0 && len(b.decisions) == 0 && len(b.watchdogs) == 0 && len(b.policies) == 0 && len(b.scheduler) == 0 && len(b.skills) == 0 && len(b.sharedMemory) == 0 && isDefaultChannelState(b.channels) && isDefaultOfficeMemberState(b.members) && b.counter == 0 && b.notificationSince == "" && b.insightsSince == "" && usageStateIsZero(b.usage) && b.sessionMode == SessionModeOffice && b.oneOnOneAgent == DefaultOneOnOneAgent {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(snapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var channelStoreRaw json.RawMessage
	if b.channelStore != nil {
		if raw, err := json.Marshal(b.channelStore); err == nil {
			channelStoreRaw = raw
		}
	}
	state := brokerState{
		ChannelStore:      channelStoreRaw,
		Messages:          b.messages,
		AgentIssues:       b.agentIssues,
		Members:           b.members,
		Channels:          b.channels,
		SessionMode:       b.sessionMode,
		OneOnOneAgent:     b.oneOnOneAgent,
		FocusMode:         b.focusMode,
		Tasks:             b.tasks,
		Requests:          b.requests,
		Actions:           b.actions,
		Signals:           b.signals,
		Decisions:         b.decisions,
		Watchdogs:         b.watchdogs,
		Policies:          b.policies,
		Scheduler:         b.scheduler,
		Skills:            b.skills,
		SharedMemory:      b.sharedMemory,
		Counter:           b.counter,
		NotificationSince: b.notificationSince,
		InsightsSince:     b.insightsSince,
		PendingInterview:  firstBlockingRequest(b.requests),
		Usage: func() teamUsageState {
			usage := b.usage
			usage.Session = usageTotals{}
			return usage
		}(),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, data); err != nil {
		return err
	}
	if brokerStateShouldSnapshot(state) {
		if err := atomicWriteFile(snapshotPath, data); err != nil {
			return err
		}
	}
	return nil
}

// atomicWriteFile writes data to path atomically by creating a uniquely-named
// sibling tmp file (mode 0o600 via os.CreateTemp) and renaming. Each call
// uses a fresh tmp filename, so concurrent writers to the same destination
// cannot race on the source path of the rename.
//
// The previous fixed `<path>.tmp` filename was safe in production (one broker
// owns one path) but broke the test suite: many *_test.go files used to
// monkey-patch the package-level state-path var and a leaked tempdir from
// worktree_guard_test.go init() was shared across every unisolated test.
// Two saves landing on the same path could interleave like A.WriteFile /
// B.WriteFile / A.Rename / B.Rename — and B's Rename failed with
// "no such file or directory" because
// A had already renamed the shared tmp out from under it. That was the CI
// flake on PR #281's `test` job. See broker_save_race_test.go for the
// regression repro.
//
// 0o600 is hard-coded because the only callers (broker state file +
// snapshot) want exactly that mode; CreateTemp already produces it on the
// platforms we support, so no os.Chmod is needed.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmpf, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpf.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmpf.Write(data); err != nil {
		_ = tmpf.Close()
		cleanup()
		return err
	}
	if err := tmpf.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

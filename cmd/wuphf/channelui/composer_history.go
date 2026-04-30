package channelui

import "strings"

// Snapshot captures the composer's input buffer and cursor at a point in time.
type Snapshot struct {
	Input []rune
	Pos   int
}

// History is a per-composer ring of recently submitted drafts plus the
// in-flight stash so users can rewind and return to what they were
// typing. Stable across recall — pressing up then down then up should
// land on the same entry.
type History struct {
	entries     []Snapshot
	recallIndex int
	stash       *Snapshot
}

const maxComposerHistoryEntries = 50

// NewHistory returns an empty composer history positioned at the live
// draft (no recall in progress).
func NewHistory() History {
	return History{recallIndex: -1}
}

// Len reports the number of submitted drafts retained in the history.
// Useful for hint logic that wants to know whether recall is even
// available.
func (h *History) Len() int {
	if h == nil {
		return 0
	}
	return len(h.entries)
}

// Record stores a non-empty draft. Identical-to-most-recent inputs are
// silently deduped (so repeated submissions don't pad the history) but
// any record clears the stash and pulls the cursor out of recall mode.
func (h *History) Record(input []rune, pos int) {
	if strings.TrimSpace(string(input)) == "" {
		return
	}
	snapshot := Snapshot{
		Input: append([]rune(nil), input...),
		Pos:   normalizeCursorPos(input, pos),
	}
	if len(h.entries) > 0 && snapshotsEqual(h.entries[len(h.entries)-1], snapshot) {
		h.ResetRecall()
		return
	}
	h.entries = append(h.entries, snapshot)
	if len(h.entries) > maxComposerHistoryEntries {
		h.entries = append([]Snapshot(nil), h.entries[len(h.entries)-maxComposerHistoryEntries:]...)
	}
	h.ResetRecall()
}

// Previous moves recall one step back and returns that entry. The
// caller's current draft is stashed on the first call so Next can
// restore it later.
func (h *History) Previous(current []rune, pos int) (Snapshot, bool) {
	if len(h.entries) == 0 {
		return Snapshot{}, false
	}
	if h.stash == nil {
		snapshot := Snapshot{
			Input: append([]rune(nil), current...),
			Pos:   normalizeCursorPos(current, pos),
		}
		h.stash = &snapshot
		h.recallIndex = len(h.entries)
	}
	if h.recallIndex > 0 {
		h.recallIndex--
	}
	return cloneSnapshot(h.entries[h.recallIndex]), true
}

// Next moves recall forward. Past the last entry it returns the stashed
// in-flight draft and exits recall mode.
func (h *History) Next() (Snapshot, bool) {
	if h.stash == nil {
		return Snapshot{}, false
	}
	if h.recallIndex >= 0 && h.recallIndex < len(h.entries)-1 {
		h.recallIndex++
		return cloneSnapshot(h.entries[h.recallIndex]), true
	}
	snapshot := cloneSnapshot(*h.stash)
	h.ResetRecall()
	return snapshot, true
}

// ResetRecall drops any in-flight stash and parks the cursor back at
// the live draft.
func (h *History) ResetRecall() {
	h.recallIndex = -1
	h.stash = nil
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	return Snapshot{
		Input: append([]rune(nil), snapshot.Input...),
		Pos:   snapshot.Pos,
	}
}

func snapshotsEqual(a, b Snapshot) bool {
	if a.Pos != b.Pos || len(a.Input) != len(b.Input) {
		return false
	}
	for i := range a.Input {
		if a.Input[i] != b.Input[i] {
			return false
		}
	}
	return true
}

func normalizeCursorPos(input []rune, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(input) {
		return len(input)
	}
	return pos
}

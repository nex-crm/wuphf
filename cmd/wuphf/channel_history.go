package main

import "strings"

type composerSnapshot struct {
	input []rune
	pos   int
}

type composerHistory struct {
	entries     []composerSnapshot
	recallIndex int
	stash       *composerSnapshot
}

const maxComposerHistoryEntries = 50

func newComposerHistory() composerHistory {
	return composerHistory{recallIndex: -1}
}

func (h *composerHistory) record(input []rune, pos int) {
	if strings.TrimSpace(string(input)) == "" {
		return
	}
	snapshot := composerSnapshot{
		input: append([]rune(nil), input...),
		pos:   normalizeCursorPos(input, pos),
	}
	if len(h.entries) > 0 && snapshotsEqual(h.entries[len(h.entries)-1], snapshot) {
		h.resetRecall()
		return
	}
	h.entries = append(h.entries, snapshot)
	if len(h.entries) > maxComposerHistoryEntries {
		h.entries = append([]composerSnapshot(nil), h.entries[len(h.entries)-maxComposerHistoryEntries:]...)
	}
	h.resetRecall()
}

func (h *composerHistory) previous(current []rune, pos int) (composerSnapshot, bool) {
	if len(h.entries) == 0 {
		return composerSnapshot{}, false
	}
	if h.stash == nil {
		snapshot := composerSnapshot{
			input: append([]rune(nil), current...),
			pos:   normalizeCursorPos(current, pos),
		}
		h.stash = &snapshot
		h.recallIndex = len(h.entries)
	}
	if h.recallIndex > 0 {
		h.recallIndex--
	}
	return cloneSnapshot(h.entries[h.recallIndex]), true
}

func (h *composerHistory) next() (composerSnapshot, bool) {
	if h.stash == nil {
		return composerSnapshot{}, false
	}
	if h.recallIndex >= 0 && h.recallIndex < len(h.entries)-1 {
		h.recallIndex++
		return cloneSnapshot(h.entries[h.recallIndex]), true
	}
	snapshot := cloneSnapshot(*h.stash)
	h.resetRecall()
	return snapshot, true
}

func (h *composerHistory) resetRecall() {
	h.recallIndex = -1
	h.stash = nil
}

func cloneSnapshot(snapshot composerSnapshot) composerSnapshot {
	return composerSnapshot{
		input: append([]rune(nil), snapshot.input...),
		pos:   snapshot.pos,
	}
}

func snapshotsEqual(a, b composerSnapshot) bool {
	if a.pos != b.pos || len(a.input) != len(b.input) {
		return false
	}
	for i := range a.input {
		if a.input[i] != b.input[i] {
			return false
		}
	}
	return true
}

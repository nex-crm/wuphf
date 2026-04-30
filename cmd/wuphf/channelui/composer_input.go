package channelui

// NormalizeCursorPos clamps pos to the valid rune-cursor range
// [0, len(input)]. Used everywhere the composer's cursor is reset
// from outside (paste, history recall, slash command insertion).
func NormalizeCursorPos(input []rune, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(input) {
		return len(input)
	}
	return pos
}

// InsertComposerRunes inserts ch into input at pos and returns the
// updated slice and the new cursor position (just after the
// inserted runes). pos is normalized via NormalizeCursorPos before
// insertion. Empty ch is a no-op. The result is always a freshly
// allocated slice so the caller's input/ch backing arrays are never
// mutated regardless of their spare capacity.
func InsertComposerRunes(input []rune, pos int, ch []rune) ([]rune, int) {
	pos = NormalizeCursorPos(input, pos)
	if len(ch) == 0 {
		return input, pos
	}
	out := make([]rune, 0, len(input)+len(ch))
	out = append(out, input[:pos]...)
	out = append(out, ch...)
	out = append(out, input[pos:]...)
	return out, pos + len(ch)
}

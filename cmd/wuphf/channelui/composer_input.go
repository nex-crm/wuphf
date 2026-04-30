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
// insertion. Empty ch is a no-op.
func InsertComposerRunes(input []rune, pos int, ch []rune) ([]rune, int) {
	pos = NormalizeCursorPos(input, pos)
	if len(ch) == 0 {
		return input, pos
	}
	tail := make([]rune, len(input[pos:]))
	copy(tail, input[pos:])
	input = append(input[:pos], append(ch, tail...)...)
	return input, pos + len(ch)
}

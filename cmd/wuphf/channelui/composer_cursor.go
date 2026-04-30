package channelui

import (
	"strings"
	"unicode"
)

// ReplaceMentionInInput replaces the in-progress "@…" token at or before
// pos with mention plus a trailing space. The new cursor position lands
// just after the inserted space. If no "@" is found before pos the input
// and pos are returned unchanged.
func ReplaceMentionInInput(input []rune, pos int, mention string) ([]rune, int) {
	text := string(input)
	if pos < 0 {
		pos = 0
	}
	if pos > len(input) {
		pos = len(input)
	}
	atIdx := strings.LastIndex(text[:pos], "@")
	if atIdx < 0 {
		return input, pos
	}
	updated := []rune(text[:atIdx] + mention + " " + text[pos:])
	return updated, atIdx + len([]rune(mention)) + 1
}

// IsComposerWordRune reports whether r is part of a composer "word"
// for word-motion purposes — letters, digits, '_' and '-'.
func IsComposerWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// MoveCursorBackwardWord returns the cursor position one word to the
// left of pos, skipping a leading run of non-word runes then a run of
// word runes.
func MoveCursorBackwardWord(input []rune, pos int) int {
	pos = NormalizeCursorPos(input, pos)
	for pos > 0 && !IsComposerWordRune(input[pos-1]) {
		pos--
	}
	for pos > 0 && IsComposerWordRune(input[pos-1]) {
		pos--
	}
	return pos
}

// MoveCursorForwardWord returns the cursor position one word to the
// right of pos, skipping a run of word runes then a run of non-word
// runes.
func MoveCursorForwardWord(input []rune, pos int) int {
	pos = NormalizeCursorPos(input, pos)
	for pos < len(input) && IsComposerWordRune(input[pos]) {
		pos++
	}
	for pos < len(input) && !IsComposerWordRune(input[pos]) {
		pos++
	}
	return pos
}

// MoveComposerCursor maps a key string ("left", "ctrl+a", "alt+b", …)
// to a new cursor position. The bool return reports whether the key was
// recognized as a motion; callers use this to decide whether to consume
// the keypress as a motion or fall through to other handling.
func MoveComposerCursor(input []rune, pos int, key string) (int, bool) {
	pos = NormalizeCursorPos(input, pos)
	switch key {
	case "left", "ctrl+b", "alt+h":
		if pos > 0 {
			pos--
		}
		return pos, true
	case "right", "ctrl+f", "alt+l":
		if pos < len(input) {
			pos++
		}
		return pos, true
	case "ctrl+a", "alt+0":
		return 0, true
	case "ctrl+e", "alt+$":
		return len(input), true
	case "alt+b":
		return MoveCursorBackwardWord(input, pos), true
	case "alt+w":
		return MoveCursorForwardWord(input, pos), true
	default:
		return pos, false
	}
}

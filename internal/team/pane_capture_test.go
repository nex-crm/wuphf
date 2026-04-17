package team

import (
	"reflect"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text untouched",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "simple color CSI",
			in:   "\x1b[31mred\x1b[0m",
			want: "red",
		},
		{
			name: "cursor movement CSI",
			in:   "abc\x1b[2Kclear\x1b[1;1H",
			want: "abcclear",
		},
		{
			name: "OSC terminated by BEL",
			in:   "\x1b]0;window title\x07content",
			want: "content",
		},
		{
			name: "OSC terminated by ST",
			in:   "\x1b]0;title\x1b\\content",
			want: "content",
		},
		{
			name: "standalone ESC control",
			in:   "prefix\x1bMsuffix",
			want: "prefixsuffix",
		},
		{
			name: "carriage return stripped",
			in:   "line\rmore",
			want: "linemore",
		},
		{
			name: "BEL stripped",
			in:   "beep\aboop",
			want: "beepboop",
		},
		{
			name: "CSI with question mark prefix (private mode)",
			in:   "\x1b[?25hvisible",
			want: "visible",
		},
		{
			name: "multiple sequences in a line",
			in:   "\x1b[1;34mBold Blue\x1b[0m and \x1b[32mGreen\x1b[0m",
			want: "Bold Blue and Green",
		},
		{
			name: "claude thinking box",
			in:   "\x1b[38;5;245m│ \x1b[0mthinking about the task\x1b[38;5;245m │\x1b[0m",
			want: "│ thinking about the task │",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripANSI(tc.in)
			if got != tc.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDiffPaneLines(t *testing.T) {
	tests := []struct {
		name string
		prev []string
		next []string
		want []string
	}{
		{
			name: "no previous, new lines returned",
			prev: nil,
			next: []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "identical capture returns nothing",
			prev: []string{"a", "b", "c"},
			next: []string{"a", "b", "c"},
			want: nil,
		},
		{
			name: "appended lines returned",
			prev: []string{"a", "b"},
			next: []string{"a", "b", "c", "d"},
			want: []string{"c", "d"},
		},
		{
			name: "TUI rerender with scrolled content preserves order of new lines",
			prev: []string{"old1", "old2", "shared1"},
			next: []string{"shared1", "new1", "new2"},
			want: []string{"new1", "new2"},
		},
		{
			name: "blank lines always skipped",
			prev: nil,
			next: []string{"", "content", "   ", "\t", "more"},
			want: []string{"content", "more"},
		},
		{
			name: "trailing whitespace trimmed",
			prev: []string{"hello"},
			next: []string{"hello   ", "world\t"},
			want: []string{"world"},
		},
		{
			name: "duplicate line in new capture is emitted only as many times as it newly appears",
			prev: []string{"dup"},
			next: []string{"dup", "dup", "dup"},
			want: []string{"dup", "dup"},
		},
		{
			name: "empty next returns nil",
			prev: []string{"a"},
			next: nil,
			want: nil,
		},
		{
			name: "order preserved even when some shared lines interleave",
			prev: []string{"a", "b", "c"},
			next: []string{"a", "x", "b", "y", "c", "z"},
			want: []string{"x", "y", "z"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diffPaneLines(tc.prev, tc.next)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("diffPaneLines(%v, %v) = %v, want %v", tc.prev, tc.next, got, tc.want)
			}
		})
	}
}

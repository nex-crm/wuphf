package emoji

import "testing"

func TestToShortcode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"🎉", ":tada:"},
		{"🚀", ":rocket:"},
		{"✅", ":white_check_mark:"},
		{"🎉 launch 🚀", ":tada: launch :rocket:"},
		{"hello world", "hello world"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ToShortcode(tt.input)
		if got != tt.want {
			t.Errorf("ToShortcode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

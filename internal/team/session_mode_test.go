package team

import "testing"

func TestNormalizeSessionMode(t *testing.T) {
	cases := map[string]string{
		"1o1":        SessionModeOneOnOne,
		"1:1":        SessionModeOneOnOne,
		"one-on-one": SessionModeOneOnOne,
		"one_on_one": SessionModeOneOnOne,
		"1on1":       SessionModeOneOnOne,
		"solo":       SessionModeOneOnOne,
		"  1:1  ":    SessionModeOneOnOne,
		"1:1 ":       SessionModeOneOnOne,
		"office":     SessionModeOffice,
		"":           SessionModeOffice,
		"nonsense":   SessionModeOffice,
	}
	for in, want := range cases {
		if got := NormalizeSessionMode(in); got != want {
			t.Errorf("NormalizeSessionMode(%q) = %q, want %q", in, got, want)
		}
	}
}

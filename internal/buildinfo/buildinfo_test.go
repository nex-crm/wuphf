package buildinfo

import "testing"

func TestIsDev(t *testing.T) {
	cases := map[string]bool{
		"":         true,
		"dev":      true,
		"  dev  ":  true,
		"0.79.10":  false,
		"v0.79.10": false,
	}
	for in, want := range cases {
		if got := IsDev(in); got != want {
			t.Errorf("IsDev(%q)=%v want %v", in, got, want)
		}
	}
}

package teammcp

import (
	"slices"
	"testing"
)

func TestLearningToolsRegisteredOnlyInMarkdownBackend(t *testing.T) {
	toolNames := []string{"team_learning_record", "team_learning_search"}
	cases := []struct {
		backend  string
		mustHave bool
	}{
		{"markdown", true},
		{"nex", false},
		{"gbrain", false},
		{"none", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.backend, func(t *testing.T) {
			t.Setenv("WUPHF_MEMORY_BACKEND", tc.backend)
			names := listRegisteredTools(t, "general", false)
			for _, tool := range toolNames {
				has := slices.Contains(names, tool)
				if tc.mustHave && !has {
					t.Errorf("backend=%s missing tool %q", tc.backend, tool)
				}
				if !tc.mustHave && has {
					t.Errorf("backend=%s unexpectedly has tool %q", tc.backend, tool)
				}
			}
		})
	}
}

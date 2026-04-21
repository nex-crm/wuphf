package teammcp

import (
	"slices"
	"testing"
)

func TestPlaybookToolsRegisteredOnlyInMarkdownBackend(t *testing.T) {
	toolNames := []string{"playbook_list", "playbook_compile", "playbook_execution_record"}
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

func TestPlaybookToolsRegisteredInOneOnOne(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	names := listRegisteredTools(t, "dm-ceo", true)
	for _, want := range []string{"playbook_list", "playbook_compile", "playbook_execution_record"} {
		if !slices.Contains(names, want) {
			t.Errorf("1:1 mode missing tool %q", want)
		}
	}
}

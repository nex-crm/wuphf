package teammcp

// server_slice1_tools_test.go — regression guard for Slice 1 wiki intelligence
// MCP tools.
//
// The three tools added in Slice 1 (wuphf_wiki_lookup, run_lint,
// resolve_contradiction) MUST be registered only when
// WUPHF_MEMORY_BACKEND=markdown. Any other backend (nex, gbrain, none) must
// not expose them — they depend on the markdown substrate to exist.

import (
	"slices"
	"testing"
)

func TestConfigureServerTools_Slice1Tools_MarkdownOnly(t *testing.T) {
	tc := []struct {
		name        string
		backend     string
		mustHave    []string
		mustNotHave []string
	}{
		{
			name:    "markdown_exposes_all_slice1_tools",
			backend: "markdown",
			mustHave: []string{
				"wuphf_wiki_lookup",
				"run_lint",
				"resolve_contradiction",
			},
		},
		{
			name:    "nex_hides_slice1_tools",
			backend: "nex",
			mustNotHave: []string{
				"wuphf_wiki_lookup",
				"run_lint",
				"resolve_contradiction",
			},
		},
		{
			name:    "gbrain_hides_slice1_tools",
			backend: "gbrain",
			mustNotHave: []string{
				"wuphf_wiki_lookup",
				"run_lint",
				"resolve_contradiction",
			},
		},
		{
			name:    "none_hides_slice1_tools",
			backend: "none",
			mustNotHave: []string{
				"wuphf_wiki_lookup",
				"run_lint",
				"resolve_contradiction",
			},
		},
	}
	for _, c := range tc {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("WUPHF_MEMORY_BACKEND", c.backend)
			names := listRegisteredTools(t, "general", false)
			for _, want := range c.mustHave {
				if !slices.Contains(names, want) {
					t.Errorf("backend=%s expected %q registered; tools=%v", c.backend, want, names)
				}
			}
			for _, wantAbsent := range c.mustNotHave {
				if slices.Contains(names, wantAbsent) {
					t.Errorf("backend=%s expected %q NOT registered; tools=%v", c.backend, wantAbsent, names)
				}
			}
		})
	}
}

// TestConfigureServerTools_Slice1BackendFlipRemovesTools verifies that
// flipping the backend env var between two server instances changes the
// registered tool set — i.e. there is no process-wide cache that
// accidentally keeps slice-1 tools alive after a backend switch.
func TestConfigureServerTools_Slice1BackendFlipRemovesTools(t *testing.T) {
	// Instance 1: markdown.
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	markdownTools := listRegisteredTools(t, "general", false)
	if !slices.Contains(markdownTools, "wuphf_wiki_lookup") {
		t.Fatalf("markdown instance missing wuphf_wiki_lookup; tools=%v", markdownTools)
	}

	// Instance 2: nex.
	t.Setenv("WUPHF_MEMORY_BACKEND", "nex")
	nexTools := listRegisteredTools(t, "general", false)
	if slices.Contains(nexTools, "wuphf_wiki_lookup") {
		t.Errorf("nex instance leaked wuphf_wiki_lookup; tools=%v", nexTools)
	}
	if slices.Contains(nexTools, "run_lint") {
		t.Errorf("nex instance leaked run_lint; tools=%v", nexTools)
	}
}

// TestConfigureServerTools_Slice1_OneOnOneAlsoGated verifies the same gate
// applies in 1:1 DM contexts — slice-1 tools must not leak there either.
func TestConfigureServerTools_Slice1_OneOnOneAlsoGated(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "nex")
	names := listRegisteredTools(t, "dm-ceo", true)
	forbidden := []string{"wuphf_wiki_lookup", "run_lint", "resolve_contradiction"}
	for _, f := range forbidden {
		if slices.Contains(names, f) {
			t.Errorf("1:1 DM with nex backend leaked %q; tools=%v", f, names)
		}
	}
}

// TestConfigureServerTools_Slice1_MarkdownOneOnOneStillRegisters covers the
// positive case: a 1:1 DM over markdown backend must still expose the
// slice-1 tools.
func TestConfigureServerTools_Slice1_MarkdownOneOnOneStillRegisters(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	names := listRegisteredTools(t, "dm-ceo", true)
	for _, want := range []string{"wuphf_wiki_lookup", "run_lint", "resolve_contradiction"} {
		if !slices.Contains(names, want) {
			t.Errorf("1:1 DM with markdown should register %q; tools=%v", want, names)
		}
	}
}

package team

import "testing"

func TestBuildCapabilityRegistryIncludesRuntimeAndWorkflowEntries(t *testing.T) {
	registry := BuildCapabilityRegistry(RuntimeCapabilities{
		Items: []CapabilityStatus{{
			Name:   "tmux",
			Level:  CapabilityReady,
			Detail: "tmux is installed.",
		}},
	})

	if len(registry.Entries) == 0 {
		t.Fatal("expected capability registry entries")
	}

	foundRuntime := false
	foundWorkflow := false
	for _, item := range registry.Entries {
		switch item.Key {
		case "tmux":
			foundRuntime = true
			if item.Category != CapabilityCategoryRuntime {
				t.Fatalf("expected tmux to be runtime category, got %+v", item)
			}
		case "workflows":
			foundWorkflow = true
			if item.Category != CapabilityCategoryWorkflow {
				t.Fatalf("expected workflows to be workflow category, got %+v", item)
			}
		}
	}
	if !foundRuntime {
		t.Fatalf("expected tmux entry in registry, got %+v", registry.Entries)
	}
	if !foundWorkflow {
		t.Fatalf("expected workflow entry in registry, got %+v", registry.Entries)
	}
}

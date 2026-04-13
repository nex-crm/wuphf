// internal/channel/slug_test.go
package channel

import "testing"

func TestDirectSlugIsSorted(t *testing.T) {
	// Same pair, different order → same slug
	if DirectSlug("engineering", "human") != DirectSlug("human", "engineering") {
		t.Error("DirectSlug must be order-independent")
	}
}

func TestDirectSlugFormat(t *testing.T) {
	slug := DirectSlug("human", "engineering")
	if slug != "engineering__human" {
		t.Errorf("expected engineering__human, got %s", slug)
	}
}

func TestGroupSlugDeterministic(t *testing.T) {
	a := GroupSlug([]string{"human", "engineering", "design"})
	b := GroupSlug([]string{"design", "human", "engineering"})
	if a != b {
		t.Error("GroupSlug must be order-independent")
	}
	if len(a) != 40 {
		t.Errorf("expected 40-char SHA1 hex, got %d chars", len(a))
	}
}

func TestDirectSlugDoesNotCollideWithGroup(t *testing.T) {
	d := DirectSlug("human", "engineering")
	g := GroupSlug([]string{"human", "engineering"})
	if d == g {
		t.Error("direct and group slugs must not collide")
	}
}

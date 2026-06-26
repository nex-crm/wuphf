package team

import (
	"testing"
)

func TestObsidianWatcherEnabled_DefaultOn(t *testing.T) {
	t.Setenv("WUPHF_OBSIDIAN_WATCHER", "")
	if !obsidianWatcherEnabled() {
		t.Fatalf("default should be enabled when env unset")
	}
}

func TestObsidianWatcherEnabled_OptOut(t *testing.T) {
	cases := []string{"0", "off", "OFF", "false", "False", "no", "disabled", "  off  "}
	for _, v := range cases {
		t.Setenv("WUPHF_OBSIDIAN_WATCHER", v)
		if obsidianWatcherEnabled() {
			t.Errorf("env=%q should disable", v)
		}
	}
}

func TestObsidianWatcherEnabled_UnknownValuesEnable(t *testing.T) {
	cases := []string{"1", "on", "true", "yes", "anything"}
	for _, v := range cases {
		t.Setenv("WUPHF_OBSIDIAN_WATCHER", v)
		if !obsidianWatcherEnabled() {
			t.Errorf("env=%q should leave watcher enabled (only explicit opt-out disables)", v)
		}
	}
}

func TestBrokerObsidianIdentity_ResolvesLocalSlug(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	reg := NewHumanIdentityRegistryAt(t.TempDir())
	// Observe a real identity so registry.Local() returns it instead of
	// probing the host's git config (CI may or may not have one set).
	if _, err := reg.Observe("Sarah Chen", "sarah@acme.com"); err != nil {
		t.Fatalf("observe: %v", err)
	}
	fn := brokerObsidianIdentity(reg)
	slug, ok := fn()
	if !ok {
		t.Fatalf("expected ok=true; got false")
	}
	if slug == "" {
		t.Fatalf("expected non-empty slug")
	}
}

func TestBrokerObsidianIdentity_NilRegistryReturnsFalse(t *testing.T) {
	fn := brokerObsidianIdentity(nil)
	if _, ok := fn(); ok {
		t.Fatalf("nil registry should return ok=false")
	}
}

func TestBrokerObsidianNormalizer_SlugWins(t *testing.T) {
	idx := newSpyIndex()
	idx.add(resolverEntity{Slug: "acme-corp", Kind: EntityKindCompanies, Name: "Acme Corp"})
	fn := brokerObsidianNormalizer(idx)
	kind, slug, ok := fn("acme-corp")
	if !ok || kind != EntityKindCompanies || slug != "acme-corp" {
		t.Fatalf("slug lookup: kind=%q slug=%q ok=%v", kind, slug, ok)
	}
}

func TestBrokerObsidianNormalizer_DisplayStringSingleMatch(t *testing.T) {
	idx := newSpyIndex()
	idx.add(resolverEntity{Slug: "acme-corp", Kind: EntityKindCompanies, Name: "Acme Corp"})
	fn := brokerObsidianNormalizer(idx)
	kind, slug, ok := fn("Acme Corp")
	if !ok || kind != EntityKindCompanies || slug != "acme-corp" {
		t.Fatalf("display-string lookup: kind=%q slug=%q ok=%v", kind, slug, ok)
	}
}

func TestBrokerObsidianNormalizer_AmbiguousReturnsFalse(t *testing.T) {
	idx := newSpyIndex()
	idx.add(resolverEntity{Slug: "acme", Kind: EntityKindPeople, Name: "Acme"})
	idx.add(resolverEntity{Slug: "acme-corp", Kind: EntityKindCompanies, Name: "Acme"})
	fn := brokerObsidianNormalizer(idx)
	// Both entities have Name "Acme" — a display-string lookup must NOT
	// silently pick one. This is the basename-collision guard surfaced at
	// the normalizer layer.
	if _, _, ok := fn("Unrelated-Slug-That-Hits-Name-Search-Acme"); ok {
		t.Fatalf("ambiguous name match must not resolve")
	}
}

func TestBrokerObsidianNormalizer_NotFoundReturnsFalse(t *testing.T) {
	idx := newSpyIndex()
	idx.add(resolverEntity{Slug: "acme-corp", Kind: EntityKindCompanies, Name: "Acme Corp"})
	fn := brokerObsidianNormalizer(idx)
	if _, _, ok := fn("nonexistent"); ok {
		t.Fatalf("unknown input should not resolve")
	}
}

func TestBrokerObsidianNormalizer_NilIndexReturnsNilCallback(t *testing.T) {
	if fn := brokerObsidianNormalizer(nil); fn != nil {
		t.Fatalf("nil signal index should return nil callback so watcher.SetNormalizer is a no-op")
	}
}

func TestBrokerObsidianNormalizer_EmptyInputReturnsFalse(t *testing.T) {
	fn := brokerObsidianNormalizer(newSpyIndex())
	for _, in := range []string{"", "   ", "\t"} {
		if _, _, ok := fn(in); ok {
			t.Errorf("empty/whitespace input %q should not resolve", in)
		}
	}
}

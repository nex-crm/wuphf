package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"testing"
)

func spriteDigest(s pixelSprite) string {
	h := sha1.New()
	for _, row := range s {
		for _, v := range row {
			fmt.Fprintf(h, "%d,", v)
		}
		fmt.Fprintln(h)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

func paletteDigest(p map[int][3]int) string {
	h := sha1.New()
	keys := make([]int, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, i := range keys {
		rgb := p[i]
		fmt.Fprintf(h, "%d:%d,%d,%d\n", i, rgb[0], rgb[1], rgb[2])
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

func TestProceduralSpriteStableAndUnique(t *testing.T) {
	slugs := []string{
		"agent-1", "agent-2", "agent-3",
		"alice", "bob", "carol",
		"devops-1", "devops-2", "qa-lead",
		"growth", "sre", "unknown",
	}

	seen := make(map[string]string)
	for _, slug := range slugs {
		a := proceduralSpriteForSlug(slug)
		b := proceduralSpriteForSlug(slug)
		ad := spriteDigest(a)
		bd := spriteDigest(b)
		if ad != bd {
			t.Errorf("slug %q is not stable across calls: %s vs %s", slug, ad, bd)
		}
		if prev, ok := seen[ad]; ok {
			t.Errorf("slug %q collides with %q on digest %s", slug, prev, ad)
		}
		seen[ad] = slug
	}

	if len(seen) != len(slugs) {
		t.Errorf("expected %d unique sprites, got %d", len(slugs), len(seen))
	}
}

func TestProceduralPaletteHasIndependentChannels(t *testing.T) {
	// Different slugs should at least sometimes differ in skin, hair, and accent
	// independently — not all three locked to one dimension.
	slugs := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	skins := make(map[[3]int]bool)
	hairs := make(map[[3]int]bool)
	accents := make(map[[3]int]bool)
	for _, slug := range slugs {
		p := proceduralPaletteForSlug(slug)
		skins[p[pxSkin]] = true
		hairs[p[pxHair]] = true
		accents[p[pxAccent]] = true
	}
	if len(skins) < 2 {
		t.Errorf("expected skin variation across %d slugs, got %d unique", len(slugs), len(skins))
	}
	if len(hairs) < 2 {
		t.Errorf("expected hair variation across %d slugs, got %d unique", len(slugs), len(hairs))
	}
	if len(accents) < 2 {
		t.Errorf("expected accent variation across %d slugs, got %d unique", len(slugs), len(accents))
	}
}

func TestKnownSlugsUseHandDesignedSprites(t *testing.T) {
	for _, slug := range []string{"ceo", "pm", "fe", "be", "ai", "designer", "cmo", "cro"} {
		if isProceduralSlug(slug) {
			t.Errorf("slug %q should not be procedural", slug)
		}
	}
	for _, slug := range []string{"custom", "agent-1", "random"} {
		if !isProceduralSlug(slug) {
			t.Errorf("slug %q should be procedural", slug)
		}
	}
}

func TestAgentColorUsesProceduralAccentForDynamicSlugs(t *testing.T) {
	slug := "qa-lead"
	if got, want := agentColor(slug), proceduralOfficeAccentForSlug(slug); got != want {
		t.Fatalf("agentColor(%q) = %q, want procedural accent %q", slug, got, want)
	}
	if got := agentColor("ceo"); got != agentColorMap["ceo"] {
		t.Fatalf("agentColor(%q) = %q, want fixed built-in color %q", "ceo", got, agentColorMap["ceo"])
	}
}

func TestOperationSlugsUseGeneratedOfficeSprites(t *testing.T) {
	for _, slug := range []string{"planner", "builder", "growth", "reviewer", "operator"} {
		if _, ok := knownOfficeSpriteForSlug(slug); !ok {
			t.Fatalf("expected %q to resolve to generated office avatar sprite", slug)
		}
	}
}

func TestDynamicSlugsUseProceduralOfficeSprites(t *testing.T) {
	first, ok := proceduralOfficeSpriteForSlug("custom-ops-agent")
	if !ok {
		t.Fatal("expected custom slug to resolve to procedural office sprite")
	}
	again, ok := proceduralOfficeSpriteForSlug("custom-ops-agent")
	if !ok {
		t.Fatal("expected repeated custom slug to resolve to procedural office sprite")
	}
	second, ok := proceduralOfficeSpriteForSlug("custom-sales-agent")
	if !ok {
		t.Fatal("expected second custom slug to resolve to procedural office sprite")
	}
	if len(first.Portrait) != 16 {
		t.Fatalf("expected generated office portrait height 16, got %d", len(first.Portrait))
	}
	if spriteDigest(first.Portrait) != spriteDigest(again.Portrait) {
		t.Fatalf("expected procedural office sprite to be stable for same slug")
	}
	if spriteDigest(first.Portrait) == spriteDigest(second.Portrait) && paletteDigest(first.Palette) == paletteDigest(second.Palette) {
		t.Fatalf("expected different custom slugs to vary sprite or palette")
	}
}

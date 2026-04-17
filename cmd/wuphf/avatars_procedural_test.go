package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
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

func TestProceduralSpriteStableAndUnique(t *testing.T) {
	slugs := []string{
		"agent-1", "agent-2", "agent-3",
		"alice", "bob", "carol",
		"devops-1", "devops-2", "qa-lead",
		"growth", "sre", "unknown",
	}

	seen := make(map[string]string)
	for _, slug := range slugs {
		a := spriteForSlug(slug, 0)
		b := spriteForSlug(slug, 0)
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

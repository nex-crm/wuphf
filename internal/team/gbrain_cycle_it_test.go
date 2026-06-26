package team

// gbrain_cycle_it_test.go is a live integration test for the compounding
// loop: office chat -> capture into gbrain -> retrieved as agent context with
// NO prompting. It drives the real production code paths (the gbrainSourceWriter
// capture path from G4 and gbrainMemoryBackend.FetchBrief, which headless_claude
// injects into every turn), not fakes.
//
// Skipped by default. To run it you need a gbrain brain initialised with an
// embedder (Ollama works with no API key):
//
//	export HOME=/tmp/gbrain-demo
//	gbrain init --pglite --embedding-model ollama:nomic-embed-text
//	WUPHF_GBRAIN_IT=1 HOME=/tmp/gbrain-demo go test ./internal/team/ \
//	  -run TestGBrainCompoundingCycle -count=1 -v

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

func TestGBrainCompoundingCycle(t *testing.T) {
	if os.Getenv("WUPHF_GBRAIN_IT") != "1" {
		t.Skip("set WUPHF_GBRAIN_IT=1 (and HOME at an Ollama-embedded gbrain brain) to run the live compounding-cycle test")
	}

	ctx := context.Background()
	client := gbrain.NewClient(gbrain.WithCallTimeout(90*time.Second), gbrain.WithConnectTimeout(60*time.Second))
	defer client.Close()
	if _, err := client.Identity(ctx); err != nil {
		t.Fatalf("gbrain not reachable (init a brain + HOME): %v", err)
	}

	// Register the real client as the process-wide backend client so both the
	// capture writer and the memory backend use it — exactly as the broker wires
	// it in production.
	setSharedGBrainClient(client)
	defer setSharedGBrainClient(nil)

	// 1) CHAT HAPPENS — a meaningful thread reaching a concrete decision.
	chat := "@sam: what are we charging for the Pro plan?\n" +
		"@pam: proposal is 49 dollars per month, annual billing as the default, with a 14-day free trial.\n" +
		"@sam: works for me — let's lock that in for launch.\n" +
		"@pam: done. Pro = $49/mo, annual-default, 14-day trial."
	job := SourceCaptureJob{
		Kind:       SourceKindChat,
		Title:      "#pricing digest — Pro plan",
		Origin:     "pricing-2026-06-26",
		Content:    chat,
		CapturedAt: time.Date(2026, 6, 26, 17, 0, 0, 0, time.UTC),
	}
	slug := DeriveSourceID(job.Kind, job.Origin, job.Title, job.Content)

	// Seed a RELATED page first so auto-association on capture has a target.
	const relatedSlug = "pricing-strategy"
	if _, err := client.PutPage(ctx,
		"---\ntitle: Pricing Strategy\ntags: [pricing]\n---\n\nOur pricing strategy favors premium tiers, annual billing defaults, and free trials to drive conversion.",
		gbrain.PutOptions{Slug: relatedSlug}); err != nil {
		t.Fatalf("seed related page: %v", err)
	}
	time.Sleep(2 * time.Second)

	// 2) PROCESS INTO GBRAIN — the real G4 capture writer (chat -> put_page),
	// which also auto-associates the new page with related pages.
	writer := newGBrainSourceWriter()
	if err := writer.WriteSource(ctx, job); err != nil {
		t.Fatalf("capture WriteSource: %v", err)
	}
	time.Sleep(2 * time.Second) // let gbrain finish embedding/reconcile

	// 3) WRITTEN WITH SOURCE ATTRIBUTION — read the page back from gbrain.
	page, err := client.GetPage(ctx, slug)
	if err != nil {
		t.Fatalf("GetPage(%q): %v", slug, err)
	}
	t.Logf("captured gbrain page %q:\n  body: %s\n  tags: %v\n  frontmatter: %v", slug, page.Content, page.Tags, page.Frontmatter)
	// The decision survives in the body.
	if !strings.Contains(page.Content, "$49") {
		t.Errorf("captured page body missing the decision content ($49)")
	}
	// Source attribution survives in gbrain's parsed metadata: tags + the
	// frontmatter map (gbrain server-stamps its own source_kind=mcp:put_page,
	// so our office provenance lives here, not in gbrain's native source_kind).
	if !containsStr(page.Tags, "chat") || !containsStr(page.Tags, "office") {
		t.Errorf("captured page tags missing office/chat attribution: %v", page.Tags)
	}
	for k, want := range map[string]string{"kind": "chat", "origin": "pricing-2026-06-26", "source": "wuphf-office"} {
		if got, _ := page.Frontmatter[k].(string); got != want {
			t.Errorf("captured page frontmatter[%q] = %q, want %q", k, got, want)
		}
	}
	// Provenance is ALSO queryable in the body (not only the frontmatter map).
	if !strings.Contains(page.Content, "Captured from WUPHF office") {
		t.Errorf("captured page body missing the queryable provenance line:\n%s", page.Content)
	}

	// 3b) ASSOCIATIONS ON CAPTURE — the writer auto-linked to the related page,
	// so the graph is populated immediately (not only via gbrain's dream-cycle).
	links, err := client.GetLinks(ctx, slug)
	if err != nil {
		t.Fatalf("GetLinks(%q): %v", slug, err)
	}
	t.Logf("auto-associations from %q: %+v", slug, links)
	foundRelated := false
	for _, l := range links {
		if l.To == relatedSlug {
			foundRelated = true
			if l.Source != captureLinkSource {
				t.Errorf("auto-link source = %q, want %q", l.Source, captureLinkSource)
			}
		}
	}
	if !foundRelated {
		t.Errorf("expected an auto-association from %q to %q on capture, got %+v", slug, relatedSlug, links)
	}

	// 4) RETRIEVE IN A NEW CHAT AS CONTEXT — NO PROMPTING.
	// This is exactly what headless_claude injects on every turn: the new chat
	// message becomes the FetchBrief query. The wording is deliberately
	// different from the captured text, so a hit proves semantic retrieval.
	newChatMessage := "remind me how much the professional tier costs and the billing terms"
	brief := gbrainMemoryBackend{}.FetchBrief(ctx, newChatMessage)
	t.Logf("auto-injected brief for new chat %q:\n%s", newChatMessage, brief)
	if brief == "" {
		t.Fatal("expected a non-empty GBRAIN CONTEXT brief for the new chat (retrieval without prompting failed)")
	}
	if !strings.Contains(brief, "GBRAIN CONTEXT") {
		t.Errorf("brief missing the GBRAIN CONTEXT frame: %q", brief)
	}
	low := strings.ToLower(brief)
	if !strings.Contains(low, "49") && !strings.Contains(low, "pro") && !strings.Contains(low, "pricing") {
		t.Errorf("brief did not surface the captured pricing knowledge:\n%s", brief)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

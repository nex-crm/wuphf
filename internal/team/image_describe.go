package team

// image_describe.go is the vision alt-text generator. Mirrors the
// entity_synthesizer pattern: shell out to the user's configured LLM CLI,
// capture a single-line description, commit the sidecar via the shared
// WikiWorker queue under the "archivist" identity.
//
// Vision CLI availability
// =======================
//
// Not every configured LLM CLI supports image inputs out of the box. Our
// strategy is:
//
//  1. Point the model at the absolute on-disk path of the asset.
//  2. Ask it to describe the image in one concise sentence.
//  3. If the provider surfaced a text response, commit it.
//  4. If the provider errored, log and bail — agents can retry via the
//     wiki_image_describe MCP tool, and operators can retry via the
//     /wiki/images/describe HTTP endpoint.
//
// We do NOT try to detect "this CLI cannot see images" automatically. The
// pluggable provider layer gives the model whatever shape it supports; a
// text-only CLI will generate a filename-derived description that is better
// than empty alt-text.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// VisionArchivistAuthor is the git identity used for vision-synthesized alt
// text. Mirrors ArchivistAuthor (for entity briefs) but kept separate so
// audit views can filter image-specific synthesis out if desired.
const VisionArchivistAuthor = "archivist"

// VisionPromptSystem is the exact system prompt sent to the LLM. Locked so
// the behavior is reproducible across sessions. Keep it short so text-only
// CLIs without real vision still produce something usable.
const VisionPromptSystem = `You generate concise, factual alt-text for images. Respond with ONE sentence under 160 characters describing what the image shows. Do not speculate about context. Output plain text only — no markdown, no quotes, no "Alt text:" prefix.`

// MaxAltTextBytes caps what we'll commit. Prevents a runaway LLM from
// dumping a full article into the alt sidecar.
const MaxAltTextBytes = 800

// DefaultVisionTimeout bounds a single vision shell-out. Configurable via
// WUPHF_IMAGE_VISION_TIMEOUT (seconds).
const DefaultVisionTimeout = 45 * time.Second

// defaultVisionLLMCall shells out to the configured LLM CLI with the asset
// path and the canned prompt. Extracted so tests can replace it.
var defaultVisionLLMCall = func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	_ = ctx
	return provider.RunConfiguredOneShot(systemPrompt, userPrompt, "")
}

// requestVisionAltText runs the full describe-then-commit pipeline for one
// asset. Designed to be invoked as `go b.requestVisionAltText(path)` — it
// has no return value and logs failures rather than surfacing them, since
// the caller has already replied to the client.
func (b *Broker) requestVisionAltText(assetRelPath string) {
	if err := validateAssetPath(assetRelPath); err != nil {
		log.Printf("image vision: invalid asset path %q: %v", assetRelPath, err)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		log.Printf("image vision: wiki worker not attached; skipping %s", assetRelPath)
		return
	}
	// Skip if a sidecar already exists — idempotent retries shouldn't thrash
	// git history, and the caller is expected to re-request explicitly via
	// /wiki/images/describe when they want a re-synthesis.
	if existing, _ := worker.Repo().ReadImageAlt(assetRelPath); strings.TrimSpace(existing) != "" {
		return
	}

	absPath := filepath.Join(worker.Repo().Root(), assetRelPath)
	userPrompt := fmt.Sprintf(
		"Describe the image at this path in one concise sentence under 160 characters. Image path: %s",
		absPath,
	)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultVisionTimeout)
	defer cancel()
	output, err := defaultVisionLLMCall(ctx, VisionPromptSystem, userPrompt)
	if err != nil {
		log.Printf("image vision: llm call failed for %s: %v", assetRelPath, err)
		return
	}
	alt := cleanVisionOutput(output)
	if alt == "" {
		log.Printf("image vision: llm returned empty alt-text for %s", assetRelPath)
		return
	}
	if len(alt) > MaxAltTextBytes {
		alt = alt[:MaxAltTextBytes]
	}

	altRel := altSidecarRelPath(assetRelPath)
	msg := fmt.Sprintf("archivist: vision alt text for %s", filepath.Base(assetRelPath))
	if _, _, werr := worker.EnqueueImageAlt(context.Background(), VisionArchivistAuthor, altRel, alt, msg); werr != nil {
		log.Printf("image vision: commit alt for %s failed: %v", assetRelPath, werr)
	}
}

// cleanVisionOutput strips stray markdown artefacts, surrounding quotes, and
// "Alt text:" prefixes the model sometimes volunteers anyway. Result is
// trimmed + single-line.
func cleanVisionOutput(raw string) string {
	out := strings.TrimSpace(raw)
	// Split on newline and take the first non-empty line — defensive
	// against models that echo a multi-line answer.
	if idx := strings.IndexByte(out, '\n'); idx > 0 {
		out = strings.TrimSpace(out[:idx])
	}
	// Strip common prefixes.
	for _, prefix := range []string{"Alt text:", "Alt-text:", "alt:", "ALT:"} {
		out = strings.TrimSpace(strings.TrimPrefix(out, prefix))
	}
	// Strip surrounding quotes.
	if len(out) >= 2 {
		first := out[0]
		last := out[len(out)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			out = out[1 : len(out)-1]
		}
	}
	return strings.TrimSpace(out)
}

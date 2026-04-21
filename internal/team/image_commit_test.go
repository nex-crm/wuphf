package team

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// imageRecordingPublisher satisfies wikiEventPublisher + imageEventPublisher
// so the worker's event-routing branches can be asserted end-to-end.
type imageRecordingPublisher struct {
	mu        sync.Mutex
	uploaded  []imageUploadedEvent
	altEvents []imageAltUpdatedEvent
}

func (p *imageRecordingPublisher) PublishWikiEvent(wikiWriteEvent)         {}
func (p *imageRecordingPublisher) PublishNotebookEvent(notebookWriteEvent) {}
func (p *imageRecordingPublisher) PublishImageUploaded(evt imageUploadedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.uploaded = append(p.uploaded, evt)
}
func (p *imageRecordingPublisher) PublishImageAltUpdated(evt imageAltUpdatedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.altEvents = append(p.altEvents, evt)
}

func (p *imageRecordingPublisher) uploadedSnapshot() []imageUploadedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]imageUploadedEvent, len(p.uploaded))
	copy(out, p.uploaded)
	return out
}
func (p *imageRecordingPublisher) altSnapshot() []imageAltUpdatedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]imageAltUpdatedEvent, len(p.altEvents))
	copy(out, p.altEvents)
	return out
}

func newImageTestWorker(t *testing.T) (*WikiWorker, *Repo, *imageRecordingPublisher, context.CancelFunc) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	pub := &imageRecordingPublisher{}
	worker := NewWikiWorker(repo, pub)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, repo, pub, func() {
		cancel()
		worker.Stop()
	}
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestEnqueueImageAsset_HappyPath covers upload → commit → event fan-out.
func TestEnqueueImageAsset_HappyPath(t *testing.T) {
	worker, repo, pub, teardown := newImageTestWorker(t)
	defer teardown()

	payload := makePNG(t, 800, 600)
	sum := sha256.Sum256(payload)
	rel, _ := assetRelPathForHash(sum[:], "hero.png", FormatPNG, time.Now().UTC())

	var thumbBuf bytes.Buffer
	if _, err := GenerateThumbnail(payload, FormatPNG, &thumbBuf); err != nil {
		t.Fatalf("thumb: %v", err)
	}
	thumbRel := thumbRelPathForHash(sum[:], time.Now().UTC().Format("200601"))

	sha, n, err := worker.EnqueueImageAsset(context.Background(), "nazz", rel, payload, thumbRel, thumbBuf.Bytes(), "upload hero")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if sha == "" || n != len(payload) {
		t.Fatalf("bad result: sha=%q n=%d want %d", sha, n, len(payload))
	}

	// Asset + thumb on disk.
	if _, err := os.Stat(filepath.Join(repo.Root(), rel)); err != nil {
		t.Fatalf("asset missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), thumbRel)); err != nil {
		t.Fatalf("thumb missing: %v", err)
	}

	// SSE event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ev := pub.uploadedSnapshot(); len(ev) == 1 {
			if ev[0].Path != rel {
				t.Fatalf("event path mismatch: %q vs %q", ev[0].Path, rel)
			}
			if ev[0].AuthorSlug != "nazz" {
				t.Fatalf("event author: got %q", ev[0].AuthorSlug)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected upload event; got %d", len(pub.uploadedSnapshot()))
}

// TestEnqueueImageAsset_DedupeBySameContent — a second upload with identical
// bytes must not create a second commit.
func TestEnqueueImageAsset_DedupeBySameContent(t *testing.T) {
	worker, repo, _, teardown := newImageTestWorker(t)
	defer teardown()

	payload := makePNG(t, 200, 200)
	sum := sha256.Sum256(payload)
	rel, _ := assetRelPathForHash(sum[:], "same.png", FormatPNG, time.Now().UTC())

	sha1, _, err := worker.EnqueueImageAsset(context.Background(), "nazz", rel, payload, "", nil, "first")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	sha2, _, err := worker.EnqueueImageAsset(context.Background(), "nazz", rel, payload, "", nil, "second")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if sha1 != sha2 {
		t.Fatalf("expected dedupe: sha1=%q sha2=%q", sha1, sha2)
	}

	// Verify only one commit introduced the asset by counting log entries
	// touching it.
	repo.mu.Lock()
	out, gerr := repo.runGitLocked(context.Background(), "system", "log", "--oneline", "--", rel)
	repo.mu.Unlock()
	if gerr != nil {
		t.Fatalf("git log: %v", gerr)
	}
	lines := strings.Count(strings.TrimSpace(out), "\n")
	if lines > 0 {
		// 0 count + presence of content is 1 line (no trailing \n after
		// TrimSpace). Over 0 means multiple.
		t.Fatalf("expected single commit, got log %q", out)
	}
}

// TestEnqueueImageAlt writes a sidecar and verifies readback + SSE.
func TestEnqueueImageAlt(t *testing.T) {
	worker, repo, pub, teardown := newImageTestWorker(t)
	defer teardown()

	payload := makePNG(t, 100, 100)
	sum := sha256.Sum256(payload)
	rel, _ := assetRelPathForHash(sum[:], "diag.png", FormatPNG, time.Now().UTC())
	if _, _, err := worker.EnqueueImageAsset(context.Background(), "nazz", rel, payload, "", nil, "upload"); err != nil {
		t.Fatalf("upload: %v", err)
	}

	altRel := altSidecarRelPath(rel)
	altText := "A small checkerboard test image."
	if _, _, err := worker.EnqueueImageAlt(context.Background(), "archivist", altRel, altText, "alt"); err != nil {
		t.Fatalf("alt commit: %v", err)
	}

	got, err := repo.ReadImageAlt(rel)
	if err != nil {
		t.Fatalf("read alt: %v", err)
	}
	if got != altText {
		t.Fatalf("alt mismatch: %q vs %q", got, altText)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ev := pub.altSnapshot(); len(ev) == 1 {
			if ev[0].AltPath != altRel {
				t.Fatalf("alt event path: %q vs %q", ev[0].AltPath, altRel)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("alt event missing")
}

// TestCommitImageAsset_RejectsTraversal — a crafted path must never escape
// the team/assets/ subtree.
func TestCommitImageAsset_RejectsTraversal(t *testing.T) {
	worker, _, _, teardown := newImageTestWorker(t)
	defer teardown()
	_, _, err := worker.EnqueueImageAsset(context.Background(), "nazz", "team/../etc/passwd", []byte("x"), "", nil, "bad")
	if err == nil {
		t.Fatal("expected traversal rejection")
	}
}

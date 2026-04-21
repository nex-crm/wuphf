package team

// broker_images.go wires the v1.3 wiki image surface onto the broker:
//   - POST /wiki/images         multipart upload → content-addressed commit
//   - GET  /wiki/assets/*       serves asset bytes with strict CSP
//   - POST /wiki/images/describe triggers vision alt-text synthesis
//   - GET  /wiki/images/alt     reads the sidecar alt-text
//   - subscribe / publish seams for "wiki:image_uploaded" + "wiki:image_alt_updated"
//
// The handlers route all writes through WikiWorker so the same single-writer
// guarantee that protects the wiki also protects image commits.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// maxMultipartSlack is the overhead we allow on top of the per-image size
// ceiling for multipart boundary bytes + form fields. 64 KiB is comfortable
// for any reasonable uploader.
const maxMultipartSlack = 64 * 1024

// SubscribeImageEvents returns a channel of image upload notifications plus
// an unsubscribe func. The /events SSE loop uses this to emit
// "wiki:image_uploaded" events distinct from "wiki:write".
func (b *Broker) SubscribeImageEvents(buffer int) (<-chan imageUploadedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.imageSubscribers == nil {
		b.imageSubscribers = make(map[int]chan imageUploadedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan imageUploadedEvent, buffer)
	b.imageSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.imageSubscribers[id]; ok {
			delete(b.imageSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// SubscribeImageAltEvents returns a channel for alt-text updates.
func (b *Broker) SubscribeImageAltEvents(buffer int) (<-chan imageAltUpdatedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.imageAltSubscribers == nil {
		b.imageAltSubscribers = make(map[int]chan imageAltUpdatedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan imageAltUpdatedEvent, buffer)
	b.imageAltSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.imageAltSubscribers[id]; ok {
			delete(b.imageAltSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishImageUploaded fans out to all /events subscribers. Implements the
// imageEventPublisher interface consumed by WikiWorker.
func (b *Broker) PublishImageUploaded(evt imageUploadedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.imageSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// PublishImageAltUpdated fans out alt-text updates.
func (b *Broker) PublishImageAltUpdated(evt imageAltUpdatedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.imageAltSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// handleWikiImageUpload handles POST /wiki/images with multipart/form-data:
//
//	file          — the image bytes (required)
//	author_slug   — who is uploading (optional, defaults to "human")
//	alt           — optional alt-text; when absent we may auto-trigger vision
//	commit_message — optional commit message override
//
// Success response shape matches ImageUploadResult.
func (b *Broker) handleWikiImageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}

	maxBytes := ResolveImageMaxBytes()
	// Cap the raw request body before multipart parsing so a giant boundary
	// payload cannot exhaust memory. Per-file cap below is enforced again
	// after decode so a small outer body with one large file still fails.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+maxMultipartSlack)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > maxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": ErrImageTooLarge.Error()})
		return
	}
	var buf bytes.Buffer
	n, copyErr := io.Copy(&buf, io.LimitReader(file, maxBytes+1))
	if copyErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read upload: " + copyErr.Error()})
		return
	}
	if n > maxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": ErrImageTooLarge.Error()})
		return
	}
	payload := buf.Bytes()

	format, detectErr := DetectImageFormat(payload)
	if detectErr != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": detectErr.Error()})
		return
	}

	authorSlug := strings.TrimSpace(r.FormValue("author_slug"))
	if authorSlug == "" {
		authorSlug = "human"
	}
	alt := strings.TrimSpace(r.FormValue("alt"))
	commitMsg := strings.TrimSpace(r.FormValue("commit_message"))

	now := time.Now().UTC()
	sum := hashBytes(payload)
	assetRel, _ := assetRelPathForHash(sum, header.Filename, format, now)

	// Raster images get a thumbnail; SVGs pass through with no thumb.
	var thumbRel string
	var thumbBytes []byte
	var dims ImageDimensions
	if format.IsRaster() {
		var thumbBuf bytes.Buffer
		d, terr := GenerateThumbnail(payload, format, &thumbBuf)
		if terr != nil {
			// Thumbnail failure is visible but not fatal — we still commit
			// the original so the asset is not lost. The caller will fall
			// back to the source URL on display.
			dims = d
		} else {
			dims = d
			thumbBytes = thumbBuf.Bytes()
			// Thumbnails only exist when the decoder actually produced a
			// smaller sized image. Skip if source was already <= ThumbWidth
			// (GenerateThumbnail may return the original-sized image).
			if dims.Width > 0 && dims.Width > ThumbWidth {
				yyyymm := now.Format("200601")
				thumbRel = thumbRelPathForHash(sum, yyyymm)
			} else {
				// Source is already small — skip the thumb, point callers
				// at the original.
				thumbBytes = nil
				thumbRel = ""
			}
		}
	}

	sha, _, werr := worker.EnqueueImageAsset(r.Context(), authorSlug, assetRel, payload, thumbRel, thumbBytes, commitMsg)
	if werr != nil {
		if errors.Is(werr, ErrQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": werr.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": werr.Error()})
		return
	}

	altRel := ""
	if alt != "" {
		altRel = altSidecarRelPath(assetRel)
		if _, _, aerr := worker.EnqueueImageAlt(r.Context(), authorSlug, altRel, alt, "alt: provided on upload"); aerr != nil {
			// Log but do not fail — the asset is already committed.
			altRel = ""
		}
	} else if ResolveAutoAltText() {
		// Fire-and-forget vision synthesis. Runs in its own goroutine so
		// the HTTP response is not blocked on an LLM shell-out.
		go b.requestVisionAltText(assetRel)
	}

	result := ImageUploadResult{
		AssetPath: assetRel,
		ThumbPath: thumbRel,
		Width:     dims.Width,
		Height:    dims.Height,
		SHA256:    fmt.Sprintf("%x", sum),
		SizeBytes: len(payload),
		Format:    string(format),
		CommitSHA: sha,
		AltPath:   altRel,
	}
	writeJSON(w, http.StatusOK, result)
}

// handleWikiAssetServe serves a single asset path under /wiki/assets/*. The
// path tail after /wiki/assets/ becomes the relative asset path inside the
// wiki repo, which is validated before any filesystem access.
func (b *Broker) handleWikiAssetServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}

	// The request path arrives url-decoded already. Strip the /wiki/assets/
	// prefix and recompose the canonical team/assets/... path.
	tail := strings.TrimPrefix(r.URL.Path, "/wiki/assets/")
	if tail == r.URL.Path {
		// Prefix wasn't present — shouldn't happen via the mux but defend
		// against direct Handler invocation.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid asset path"})
		return
	}
	relPath := "team/assets/" + tail
	if err := validateAssetPath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	data, err := worker.Repo().ReadImageAsset(relPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "asset not found"})
		return
	}

	// Determine Content-Type from the filesystem extension. Magic-byte
	// detection at upload time already verified it's one of our allowed
	// formats; re-detect here so a tampered path cannot pivot into a
	// different mime.
	format, derr := DetectImageFormat(data)
	if derr != nil {
		// Shouldn't happen — everything under team/assets/ was accepted by
		// DetectImageFormat at upload. Degrade safely to octet-stream.
		w.Header().Set("Content-Type", "application/octet-stream")
	} else {
		w.Header().Set("Content-Type", format.MIME())
	}
	// SVG XSS handling — any <script> element inside an SVG served same-
	// origin can run with the caller's credentials. We lock it down with
	// a strict CSP so even a successfully-uploaded SVG cannot exfiltrate.
	// Images outside SVG still receive the CSP because it costs nothing
	// and protects against future format shifts.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(relPath)+`"`)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

// handleWikiImageAltGet returns the sidecar alt-text for a given asset.
//
//	GET /wiki/images/alt?asset_path=team/assets/...
func (b *Broker) handleWikiImageAltGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("asset_path"))
	if err := validateAssetPath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	alt, err := worker.Repo().ReadImageAlt(relPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"asset_path": relPath, "alt": alt})
}

// handleWikiImageDescribe triggers vision alt-text synthesis on demand.
//
//	POST /wiki/images/describe
//	{ "asset_path": "team/assets/..." , "actor_slug": "nazz" }
//
// Returns 202 Accepted with the asset path — the result lands via SSE /
// /wiki/images/alt once the vision LLM responds.
func (b *Broker) handleWikiImageDescribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		AssetPath string `json:"asset_path"`
		ActorSlug string `json:"actor_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := validateAssetPath(body.AssetPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Confirm the asset exists before kicking off the LLM round-trip.
	if _, err := worker.Repo().ReadImageAsset(body.AssetPath); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "asset not found"})
		return
	}
	go b.requestVisionAltText(body.AssetPath)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"asset_path": body.AssetPath,
		"status":     "queued",
	})
}

package team

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// newImageTestServer wires a real httptest server with the auth-gated image
// upload route, plus the unauthenticated asset serve path, over a temp repo.
func newImageTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	// Point the wiki root env at our temp dir so WikiRootDir() resolves
	// correctly for asset path serve.
	t.Setenv("WUPHF_RUNTIME_HOME", filepath.Dir(root))
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := NewBroker()
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/images", b.requireAuth(b.handleWikiImageUpload))
	mux.HandleFunc("/wiki/images/alt", b.requireAuth(b.handleWikiImageAltGet))
	mux.HandleFunc("/wiki/assets/", b.handleWikiAssetServe)
	srv := httptest.NewServer(mux)

	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
	}
}

// generatePNG returns a deterministic PNG payload of the given dimensions.
func generatePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return makePNG(t, w, h)
}

// postImageMultipart builds a multipart/form-data upload POST request.
func postImageMultipart(t *testing.T, url, token, filename string, payload []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestImageUpload_HappyPath(t *testing.T) {
	// Stop auto-alt so we don't fire an LLM call during the test.
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	payload := generatePNG(t, 800, 600)
	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "diagram.png", payload, map[string]string{
		"author_slug": "nazz",
		"alt":         "A 800x600 test gradient.",
	})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, string(body))
	}
	var result ImageUploadResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(result.AssetPath, "team/assets/") {
		t.Fatalf("bad asset path %q", result.AssetPath)
	}
	if result.Width != 800 || result.Height != 600 {
		t.Fatalf("dims: got %dx%d", result.Width, result.Height)
	}
	if result.ThumbPath == "" {
		t.Fatal("expected thumbnail for 800-wide png")
	}
	if result.CommitSHA == "" {
		t.Fatal("expected commit sha")
	}
	if result.Format != "png" {
		t.Fatalf("format: got %q", result.Format)
	}

	// Serve the asset back via /wiki/assets/... and check CSP + content type.
	tail := strings.TrimPrefix(result.AssetPath, "team/assets/")
	getRes, err := http.Get(srv.URL + "/wiki/assets/" + tail)
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("asset status %d", getRes.StatusCode)
	}
	if ct := getRes.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type: got %q", ct)
	}
	if csp := getRes.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP missing or weak: %q", csp)
	}
	if xcto := getRes.Header.Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Fatalf("X-Content-Type-Options: got %q", xcto)
	}
	got, _ := io.ReadAll(getRes.Body)
	if !bytes.Equal(got, payload) {
		t.Fatalf("asset bytes mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	// Confirm alt sidecar was written.
	altURL := srv.URL + "/wiki/images/alt?asset_path=" + result.AssetPath
	altReq, _ := http.NewRequest(http.MethodGet, altURL, nil)
	altReq.Header.Set("Authorization", "Bearer "+b.Token())
	altRes, err := http.DefaultClient.Do(altReq)
	if err != nil {
		t.Fatalf("alt get: %v", err)
	}
	defer altRes.Body.Close()
	var altBody struct{ Alt string }
	_ = json.NewDecoder(altRes.Body).Decode(&altBody)
	if !strings.Contains(altBody.Alt, "test gradient") {
		t.Fatalf("alt missing: got %q", altBody.Alt)
	}
}

func TestImageUpload_RejectsOversize(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_MAX_MB", "1") // 1 MiB cap
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	// Build a PNG large enough to exceed 1 MiB after compression.
	payload := generatePNG(t, 3000, 3000)
	if len(payload) < 1024*1024 {
		// Unlikely — our gradient compresses fairly well but 3000x3000
		// should easily exceed 1 MiB. If it doesn't we can't reliably
		// test the cap; skip instead of a false pass.
		t.Skipf("synth png too small (%d bytes) to exceed 1 MiB", len(payload))
	}
	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "huge.png", payload, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge && res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 413 or 400, got %d: %s", res.StatusCode, string(body))
	}
}

func TestImageUpload_RejectsHEIC(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	// Minimal HEIC magic: ftypheic box at offset 4.
	payload := []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0, 'h', 'e', 'i', 'c', 'm', 'i', 'f', '1', 'h', 'e', 'v', 'c'}
	// Pad out so multipart has real bytes.
	payload = append(payload, make([]byte, 256)...)

	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "photo.heic", payload, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 415, got %d: %s", res.StatusCode, string(body))
	}
}

func TestImageUpload_RejectsExtensionSwap(t *testing.T) {
	// An attacker uploads a PNG named "foo.svg" hoping SVG processing
	// surface treats it as SVG. Our magic-byte detector says "png", and
	// the response format is png — the extension lied but we were not
	// fooled.
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	payload := generatePNG(t, 100, 100)
	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "attacker.svg", payload, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, string(body))
	}
	var result ImageUploadResult
	_ = json.NewDecoder(res.Body).Decode(&result)
	if result.Format != "png" {
		t.Fatalf("expected png detection, got %q", result.Format)
	}
	if !strings.HasSuffix(result.AssetPath, ".png") {
		t.Fatalf("expected .png extension despite .svg filename, got %q", result.AssetPath)
	}
}

func TestImageAssetServe_RejectsTraversal(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, _, teardown := newImageTestServer(t)
	defer teardown()

	bad := srv.URL + "/wiki/assets/../../../../etc/passwd"
	res, err := http.Get(bad)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		t.Fatalf("traversal returned 200")
	}
}

func TestImageAssetServe_SVGHasCSP(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false")
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	// Even a benign SVG must carry the lockdown CSP on response.
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>`)
	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "icon.svg", svg, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("upload failed: %d %s", res.StatusCode, string(body))
	}
	var result ImageUploadResult
	_ = json.NewDecoder(res.Body).Decode(&result)
	if result.Format != "svg" {
		t.Fatalf("expected svg detection, got %q", result.Format)
	}

	tail := strings.TrimPrefix(result.AssetPath, "team/assets/")
	getRes, err := http.Get(srv.URL + "/wiki/assets/" + tail)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getRes.Body.Close()
	ct := getRes.Header.Get("Content-Type")
	if ct != "image/svg+xml" {
		t.Fatalf("svg content-type: got %q", ct)
	}
	csp := getRes.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "sandbox") {
		t.Fatalf("SVG CSP must lock down; got %q", csp)
	}
}

// mockVisionLLM returns a canned response for the describe call and records
// that it was invoked. Satisfies the function signature swapped in from
// image_describe.go.
func mockVisionLLM(t *testing.T, response string, called *bool) func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	t.Helper()
	return func(_ context.Context, _, _ string) (string, error) {
		*called = true
		return response, nil
	}
}

func TestRequestVisionAltText_WritesSidecar(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "false") // we'll call explicitly
	srv, b, teardown := newImageTestServer(t)
	defer teardown()

	// First upload an asset so the describe path has something to point at.
	payload := generatePNG(t, 100, 100)
	req := postImageMultipart(t, srv.URL+"/wiki/images", b.Token(), "img.png", payload, nil)
	res, _ := http.DefaultClient.Do(req)
	var result ImageUploadResult
	_ = json.NewDecoder(res.Body).Decode(&result)
	res.Body.Close()

	// Swap in the mock LLM.
	called := false
	original := defaultVisionLLMCall
	defaultVisionLLMCall = mockVisionLLM(t, "A tiny test gradient rendered at 100 by 100 pixels.", &called)
	t.Cleanup(func() { defaultVisionLLMCall = original })

	b.requestVisionAltText(result.AssetPath)
	if !called {
		t.Fatal("vision LLM was not called")
	}
	alt, err := b.WikiWorker().Repo().ReadImageAlt(result.AssetPath)
	if err != nil {
		t.Fatalf("read alt: %v", err)
	}
	if !strings.Contains(alt, "tiny test gradient") {
		t.Fatalf("alt missing expected text: %q", alt)
	}

	// Second call is idempotent — should not call LLM again while an alt
	// already exists.
	called = false
	b.requestVisionAltText(result.AssetPath)
	if called {
		t.Fatal("vision LLM re-called despite existing alt")
	}
}

func TestCleanVisionOutput(t *testing.T) {
	cases := map[string]string{
		`"A diagram of the system."`:                          "A diagram of the system.",
		"Alt text: The login screen.":                         "The login screen.",
		"  multiline\nsecond line\nthird":                     "multiline",
		`'A single-quoted answer'`:                            "A single-quoted answer",
		"Plain text answer":                                   "Plain text answer",
	}
	for in, want := range cases {
		got := cleanVisionOutput(in)
		if got != want {
			t.Errorf("clean(%q)=%q want %q", in, got, want)
		}
	}
}

// Re-use helper dependency from image_commit_test.go (same package).
var _ = png.Decode

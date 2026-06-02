package team

// Tests for the cabinet asset-upload surface: POST /wiki/upload.
//
// Covers the happy path (write + single commit + path/sha response), the
// executable-extension blocklist (400), the script-capable markup blocklist
// (.html/.svg → 400), oversize rejection (413), bad-dir traversal (400),
// filename sanitisation (../ and absolute components stripped to a basename),
// Windows reserved device-name renaming (CON.txt/nul.md never stored bare), and
// collision suffixing (-1/-2). Reuses the package-level HTTP helpers (readBody)
// and the temp-dir Repo harness.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newWikiUploadTestServer spins up an httptest server wired to the upload
// handler, backed by a fresh temp-dir Repo. It returns the base URL, the repo
// (so tests can inspect / seed team/), and a cleanup func.
func newWikiUploadTestServer(t *testing.T) (baseURL string, repo *Repo, cleanup func()) {
	t.Helper()

	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo = NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	worker := NewWikiWorker(repo, &capturePublisher{events: make(chan wikiWriteEvent, 4)})
	broker := &Broker{wikiWorker: worker}

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/upload", broker.handleWikiUpload)

	srv := httptest.NewServer(mux)
	return srv.URL, repo, func() { srv.Close() }
}

// uploadMultipart builds a multipart/form-data body with a `dir` field and a
// `file` part carrying filename + content. Returns the body and content-type.
func uploadMultipart(t *testing.T, dir, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("dir", dir); err != nil {
		t.Fatalf("write dir field: %v", err)
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// postUpload posts a multipart body to /wiki/upload and returns the response.
func postUpload(t *testing.T, baseURL string, body *bytes.Buffer, contentType string) *http.Response {
	t.Helper()
	resp, err := http.Post(baseURL+"/wiki/upload", contentType, body)
	if err != nil {
		t.Fatalf("POST /wiki/upload: %v", err)
	}
	return resp
}

type uploadResponse struct {
	Path      string `json:"path"`
	CommitSHA string `json:"commit_sha"`
}

func decodeUpload(t *testing.T, resp *http.Response) uploadResponse {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return out
}

func TestWikiUpload_HappyPath(t *testing.T) {
	baseURL, repo, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	before := commitCount(t, repo)
	content := []byte("\x89PNG\r\n\x1a\nlogo bytes")
	body, ct := uploadMultipart(t, "team/assets", "logo.png", content)
	resp := postUpload(t, baseURL, body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}

	out := decodeUpload(t, resp)
	if out.Path != "team/assets/logo.png" {
		t.Fatalf("path = %q, want team/assets/logo.png", out.Path)
	}
	if out.CommitSHA == "" {
		t.Fatal("commit_sha is empty")
	}

	// File landed on disk with the exact bytes.
	disk := readDisk(t, repo, "team/assets/logo.png")
	if disk != string(content) {
		t.Fatalf("on-disk bytes mismatch: got %q", disk)
	}
	// Exactly one new commit.
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d", after, before+1)
	}
}

func TestWikiUpload_BlocksExecutableExtension(t *testing.T) {
	baseURL, repo, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	before := commitCount(t, repo)
	body, ct := uploadMultipart(t, "team/assets", "install.sh", []byte("#!/bin/sh\nrm -rf /\n"))
	resp := postUpload(t, baseURL, body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
	}
	// Nothing written, nothing committed.
	if _, err := os.Stat(filepath.Join(repo.TeamDir(), "assets", "install.sh")); !os.IsNotExist(err) {
		t.Fatalf("blocked upload should not write file: stat err = %v", err)
	}
	if after := commitCount(t, repo); after != before {
		t.Fatalf("commit count changed on blocked upload: %d -> %d", before, after)
	}
}

func TestWikiUpload_BlocksScriptCapableExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  []byte
	}{
		{name: "html", filename: "page.html", content: []byte("<script>alert(1)</script>")},
		{name: "svg", filename: "image.svg", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, repo, cleanup := newWikiUploadTestServer(t)
			defer cleanup()

			before := commitCount(t, repo)
			body, ct := uploadMultipart(t, "team/assets", tc.filename, tc.content)
			resp := postUpload(t, baseURL, body, ct)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
			}
			// Nothing written, nothing committed.
			if _, err := os.Stat(filepath.Join(repo.TeamDir(), "assets", tc.filename)); !os.IsNotExist(err) {
				t.Fatalf("blocked upload should not write file: stat err = %v", err)
			}
			if after := commitCount(t, repo); after != before {
				t.Fatalf("commit count changed on blocked upload: %d -> %d", before, after)
			}
		})
	}
}

func TestWikiUpload_ReservedDeviceNameRenamed(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "CON.txt", want: "team/assets/file-CON.txt"},
		{raw: "nul.md", want: "team/assets/file-nul.md"},
		{raw: "com1.png", want: "team/assets/file-com1.png"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			baseURL, repo, cleanup := newWikiUploadTestServer(t)
			defer cleanup()

			body, ct := uploadMultipart(t, "team/assets", tc.raw, []byte("data-"+tc.raw))
			resp := postUpload(t, baseURL, body, ct)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("raw=%q: status = %d, want 200; body=%s", tc.raw, resp.StatusCode, readBody(t, resp))
			}
			out := decodeUpload(t, resp)
			if out.Path != tc.want {
				t.Fatalf("raw=%q: path = %q, want %q", tc.raw, out.Path, tc.want)
			}
			// The stored name must NOT be a bare reserved device name.
			stored := filepath.Base(out.Path)
			if isWindowsReservedName(strings.TrimSuffix(stored, filepath.Ext(stored))) {
				t.Fatalf("raw=%q: stored under reserved device name %q", tc.raw, stored)
			}
			if _, err := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(out.Path))); err != nil {
				t.Fatalf("raw=%q: expected file at %q: %v", tc.raw, out.Path, err)
			}
		})
	}
}

func TestWikiUpload_Oversize413(t *testing.T) {
	baseURL, _, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	// One byte past the handler's MaxBytesReader allowance (cap + 1 MiB
	// envelope headroom). Far cheaper than allocating 25 MiB but the form
	// parse still trips because the whole envelope is larger than the limit.
	oversize := make([]byte, maxUploadBytes+(1<<20)+1)
	body, ct := uploadMultipart(t, "team/assets", "big.bin.png", oversize)
	resp := postUpload(t, baseURL, body, ct)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestWikiUpload_TraversalDirRejected(t *testing.T) {
	baseURL, _, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	cases := []string{
		"team/../secret",   // traversal out of team/
		"../etc",           // not under team/
		"/abs/team/assets", // absolute
		"team-secrets/x",   // sibling that must not be treated as team/
		"notteam/assets",   // not under team/
	}
	for _, dir := range cases {
		body, ct := uploadMultipart(t, dir, "x.png", []byte("data"))
		resp := postUpload(t, baseURL, body, ct)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("dir=%q: status = %d, want 400; body=%s", dir, resp.StatusCode, readBody(t, resp))
		}
		_ = resp.Body.Close()
	}
}

func TestWikiUpload_FilenameStrippedToBasename(t *testing.T) {
	baseURL, repo, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	// A filename carrying traversal + absolute components must collapse to the
	// sanitised basename under the destination dir — never escape it.
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "../../etc/passwd.png", want: "team/assets/passwd.png"},
		{raw: "/var/evil/x.png", want: "team/assets/x.png"},
		{raw: `C:\windows\system32\note.png`, want: "team/assets/note.png"},
		{raw: "my report (final).pdf", want: "team/assets/my-report-final.pdf"},
	}
	for _, tc := range cases {
		body, ct := uploadMultipart(t, "team/assets", tc.raw, []byte("data-"+tc.raw))
		resp := postUpload(t, baseURL, body, ct)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("raw=%q: status = %d, want 200; body=%s", tc.raw, resp.StatusCode, readBody(t, resp))
		}
		out := decodeUpload(t, resp)
		if out.Path != tc.want {
			t.Fatalf("raw=%q: path = %q, want %q", tc.raw, out.Path, tc.want)
		}
		// Verify the written path stays inside team/assets.
		if !strings.HasPrefix(out.Path, "team/assets/") {
			t.Fatalf("raw=%q: escaped destination: %q", tc.raw, out.Path)
		}
		if _, err := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(out.Path))); err != nil {
			t.Fatalf("raw=%q: expected file at %q: %v", tc.raw, out.Path, err)
		}
	}
}

func TestWikiUpload_CollisionAppendsSuffix(t *testing.T) {
	baseURL, repo, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	want := []string{
		"team/assets/photo.png",
		"team/assets/photo-1.png",
		"team/assets/photo-2.png",
	}
	for i, expect := range want {
		body, ct := uploadMultipart(t, "team/assets", "photo.png", []byte{byte(i)})
		resp := postUpload(t, baseURL, body, ct)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %d: status = %d, want 200; body=%s", i, resp.StatusCode, readBody(t, resp))
		}
		out := decodeUpload(t, resp)
		if out.Path != expect {
			t.Fatalf("upload %d: path = %q, want %q", i, out.Path, expect)
		}
	}
	// All three distinct files exist on disk.
	for _, p := range want {
		if _, err := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(p))); err != nil {
			t.Fatalf("expected %q on disk: %v", p, err)
		}
	}
}

func TestWikiUpload_NonPost405(t *testing.T) {
	baseURL, _, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	resp, err := http.Get(baseURL + "/wiki/upload")
	if err != nil {
		t.Fatalf("GET /wiki/upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestWikiUpload_MissingFieldsRejected(t *testing.T) {
	baseURL, _, cleanup := newWikiUploadTestServer(t)
	defer cleanup()

	// Missing dir field.
	body, ct := func() (*bytes.Buffer, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, _ := mw.CreateFormFile("file", "x.png")
		_, _ = part.Write([]byte("data"))
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	}()
	resp := postUpload(t, baseURL, body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing dir: status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
	}

	// Missing file part.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("dir", "team/assets")
	_ = mw.Close()
	resp2 := postUpload(t, baseURL, &buf, mw.FormDataContentType())
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing file: status = %d, want 400; body=%s", resp2.StatusCode, readBody(t, resp2))
	}
}

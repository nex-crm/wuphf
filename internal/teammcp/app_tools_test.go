package teammcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveRegisterAppHTML covers the publish-by-path fix: the App Builder
// can't reliably read a minified single-file bundle (100k+ char lines) back into
// a JSON string, so it passes html_path and the broker reads the file.
func TestResolveRegisterAppHTML(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "index.html")
	const content = "<!doctype html><html><body><div id=root></div></body></html>"
	if err := os.WriteFile(bundle, []byte(content), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	t.Run("reads the file at html_path", func(t *testing.T) {
		got, err := resolveRegisterAppHTML(RegisterAppArgs{HTMLPath: bundle})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content {
			t.Fatalf("got %q, want the file contents", got)
		}
	})

	t.Run("literal html still works (fallback)", func(t *testing.T) {
		got, err := resolveRegisterAppHTML(RegisterAppArgs{HTML: content})
		if err != nil || got != content {
			t.Fatalf("got %q err %v, want literal html", got, err)
		}
	})

	t.Run("html wins when both are set", func(t *testing.T) {
		got, _ := resolveRegisterAppHTML(RegisterAppArgs{HTML: "LITERAL", HTMLPath: bundle})
		if got != "LITERAL" {
			t.Fatalf("got %q, want the literal html", got)
		}
	})

	t.Run("neither provided is an error", func(t *testing.T) {
		if _, err := resolveRegisterAppHTML(RegisterAppArgs{}); err == nil {
			t.Fatal("expected an error when neither html nor html_path is set")
		}
	})

	t.Run("relative html_path is rejected", func(t *testing.T) {
		_, err := resolveRegisterAppHTML(RegisterAppArgs{HTMLPath: "dist/index.html"})
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("want absolute-path error, got %v", err)
		}
	})

	t.Run("missing file is a clear error", func(t *testing.T) {
		if _, err := resolveRegisterAppHTML(RegisterAppArgs{HTMLPath: filepath.Join(dir, "nope.html")}); err == nil {
			t.Fatal("expected an error for a missing file")
		}
	})

	t.Run("empty file is rejected", func(t *testing.T) {
		empty := filepath.Join(dir, "empty.html")
		if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveRegisterAppHTML(RegisterAppArgs{HTMLPath: empty}); err == nil {
			t.Fatal("expected an error for an empty bundle")
		}
	})
}

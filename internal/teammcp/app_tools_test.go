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

// TestResolveRegisterAppFiles covers the source_path fix: the broker copies the
// WHOLE project tree (minus build/VCS dirs) so the agent can't ship a partial,
// unbuildable source by dropping a file from a hand-listed map.
func TestResolveRegisterAppFiles(t *testing.T) {
	root := t.TempDir()
	must := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("package.json", "{}")
	must("src/main.tsx", "import { App } from './App'")
	must("src/App.tsx", "export const App = () => null")
	must("src/styles.css", "body{}")
	must("node_modules/react/index.js", "module.exports={}") // must be skipped
	must("dist/index.html", "<html></html>")                 // must be skipped

	t.Run("copies the whole tree and skips build dirs", func(t *testing.T) {
		files, err := resolveRegisterAppFiles(RegisterAppArgs{SourcePath: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, want := range []string{"package.json", "src/main.tsx", "src/App.tsx", "src/styles.css"} {
			if _, ok := files[want]; !ok {
				t.Errorf("missing %q from copied source", want)
			}
		}
		for bad := range files {
			if strings.HasPrefix(bad, "node_modules/") || strings.HasPrefix(bad, "dist/") {
				t.Errorf("build artifact %q should have been skipped", bad)
			}
		}
		// The exact gap that broke a real publish: main.tsx imports ./App, so
		// App.tsx MUST be persisted alongside it.
		if _, ok := files["src/App.tsx"]; !ok {
			t.Fatal("src/App.tsx was dropped — this is the bug source_path fixes")
		}
	})

	t.Run("explicit files map still wins", func(t *testing.T) {
		files, err := resolveRegisterAppFiles(RegisterAppArgs{
			Files:      map[string]string{"src/App.tsx": "x"},
			SourcePath: root,
		})
		if err != nil || len(files) != 1 {
			t.Fatalf("explicit files should win; got %v err %v", files, err)
		}
	})

	t.Run("neither source nor files is fine (html-only)", func(t *testing.T) {
		files, err := resolveRegisterAppFiles(RegisterAppArgs{})
		if err != nil || files != nil {
			t.Fatalf("want (nil,nil) for html-only; got %v %v", files, err)
		}
	})

	t.Run("relative source_path rejected", func(t *testing.T) {
		if _, err := resolveRegisterAppFiles(RegisterAppArgs{SourcePath: "src"}); err == nil {
			t.Fatal("expected an error for a relative source_path")
		}
	})
}

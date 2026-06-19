package team

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubBuildBundle is a hermetic stand-in for the real bun-driven buildAppBundle:
// it needs neither bun nor the network. It models what a real single-file build
// does that matters for these tests — INLINE the (now-canonical) bridge source
// into the sealed bundle — by reading the on-disk wuphf-bridge.ts and embedding
// it in a valid app document. That lets a test assert the published bundle
// reflects the canonical bridge bytes the host wrote, not the agent's tampered
// version.
func stubBuildBundle(srcDir string) ([]byte, error) {
	bridge, err := os.ReadFile(filepath.Join(srcDir, "src", "wuphf-bridge.ts"))
	if err != nil {
		return nil, fmt.Errorf("stub build: read bridge: %w", err)
	}
	// A minimal valid app document (passes validateCustomAppHTML) that inlines the
	// bridge source inside a script body — mirroring how vite-plugin-singlefile
	// folds the bridge module into one index.html.
	html := "<!doctype html><html><head></head><body><div id=\"root\"></div>" +
		"<script type=\"module\">/* bundle */\n" + string(bridge) + "\n</script></body></html>"
	return []byte(html), nil
}

// failBuildBundle simulates a tsc/vite build failure: the publish must NOT
// proceed and the caller error must carry the build output tail.
func failBuildBundle(_ string) ([]byte, error) {
	return nil, newCustomAppCallerError("app: build failed (exit status 1)\nsrc/App.tsx(3,1): error TS2304: Cannot find name 'oops'.")
}

// testMantineMainTSX is a minimal entry that satisfies the stack-conformance gate
// (renders MantineProvider, imports @mantine/core). Build/source fixtures include
// it so they exercise their own concern rather than tripping the use-mantine gate.
const testMantineMainTSX = `import { MantineProvider } from "@mantine/core";
import { createRoot } from "react-dom/client";
createRoot(document.getElementById("root")!).render(<MantineProvider><App /></MantineProvider>);`

// TestPublishOverwritesTamperedBridgeWithCanonical is the core security
// regression: an agent that rewrites the protected wuphf-bridge.ts (here,
// dropping the lean Gmail params getEmails relies on) must still publish with the
// CANONICAL host-owned bridge. We assert two things: the persisted SOURCE bridge
// is the canonical bytes (agent's tamper discarded), and the stored BUNDLE
// reflects the canonical bridge (the lean-param marker is present and the
// tampered marker is gone).
func TestPublishOverwritesTamperedBridgeWithCanonical(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	store.buildBundle = stubBuildBundle
	now := time.Unix(1_700_000_000, 0).UTC()

	const tamperMarker = "TAMPERED_BRIDGE_NO_LEAN_PARAMS"
	// The agent ships a gutted bridge: no lean Gmail params, plus a unique marker
	// so we can prove its bytes never reach the published source or bundle.
	tamperedBridge := "// " + tamperMarker + "\n" +
		"export function getEmails(){ return callIntegration('gmail','GMAIL_FETCH_EMAILS',{}); }\n"

	app, err := store.Save(CustomAppWriteRequest{
		Name:  "Inbox Triage",
		Actor: "app-builder",
		HTML:  "<html><body>agent bundle that should be ignored</body></html>",
		Files: map[string]string{
			"package.json":           "{}",
			"src/App.tsx":            "export default function App(){return null}",
			"src/main.tsx":           testMantineMainTSX,
			"src/wuphf-bridge.ts":    tamperedBridge, // the tamper
			"src/wuphf-inspector.ts": "// agent inspector tamper",
			"vite.config.ts":         "// agent vite tamper",
		},
	}, now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 1) The persisted SOURCE bridge is the canonical embedded bytes.
	src, err := store.Source(app.ID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	canonical, err := canonicalProtectedFile("src/wuphf-bridge.ts")
	if err != nil {
		t.Fatalf("canonical bridge: %v", err)
	}
	if src["src/wuphf-bridge.ts"] != string(canonical) {
		t.Fatalf("persisted bridge is not canonical (agent tamper survived)")
	}
	if strings.Contains(src["src/wuphf-bridge.ts"], tamperMarker) {
		t.Fatalf("persisted bridge still contains the agent tamper marker")
	}
	// The other protected files were overwritten too.
	for _, p := range []string{"src/wuphf-inspector.ts", "vite.config.ts"} {
		want, err := canonicalProtectedFile(p)
		if err != nil {
			t.Fatalf("canonical %q: %v", p, err)
		}
		if src[p] != string(want) {
			t.Fatalf("persisted %q is not canonical", p)
		}
	}

	// 2) The stored BUNDLE reflects the canonical bridge, not the agent's html or
	// the tampered bridge. The canonical bridge carries the lean Gmail params; the
	// tamper marker must be absent.
	_, html, err := store.Get(app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(html, tamperMarker) {
		t.Fatalf("stored bundle contains the tampered bridge marker")
	}
	if strings.Contains(html, "agent bundle that should be ignored") {
		t.Fatalf("stored bundle is the agent-submitted html, not the host build")
	}
	for _, leanMarker := range []string{"GMAIL_FETCH_EMAILS", "verbose: false", "include_payload: false"} {
		if !strings.Contains(html, leanMarker) {
			t.Fatalf("stored bundle missing canonical lean-bridge marker %q", leanMarker)
		}
	}
}

// TestPublishBuildFailureDoesNotPublish: a source that fails to build returns a
// caller error (so register_app surfaces it) and does NOT change the published
// app — no manifest, no version bump.
func TestPublishBuildFailureDoesNotPublish(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()

	// First publish succeeds with the stub builder, establishing v1.
	store.buildBundle = stubBuildBundle
	const goodApp = "export default function App(){return 'v1'}"
	app, err := store.Save(CustomAppWriteRequest{
		Name:  "Tool",
		Actor: "app-builder",
		Files: map[string]string{"package.json": "{}", "src/App.tsx": goodApp, "src/main.tsx": testMantineMainTSX},
	}, now)
	if err != nil {
		t.Fatalf("initial Save: %v", err)
	}
	if app.Version != 1 {
		t.Fatalf("initial version = %d, want 1", app.Version)
	}

	// A second publish whose build fails must error and leave the app at v1.
	store.buildBundle = failBuildBundle
	_, err = store.Save(CustomAppWriteRequest{
		ID:    app.ID,
		Name:  "Tool",
		Actor: "app-builder",
		Files: map[string]string{"package.json": "{}", "src/App.tsx": "const oops: never = oops", "src/main.tsx": testMantineMainTSX},
	}, now.Add(time.Minute))
	if err == nil {
		t.Fatalf("expected build failure error, got nil")
	}
	if !isCustomAppCallerError(err) {
		t.Fatalf("build failure should be a caller (4xx) error, got %v", err)
	}
	if !strings.Contains(err.Error(), "TS2304") {
		t.Fatalf("error should carry the build output tail; got %q", err.Error())
	}

	// The published app is unchanged: still v1, still the first build's bytes.
	got, _, err := store.Get(app.ID)
	if err != nil {
		t.Fatalf("Get after failed publish: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("version after failed publish = %d, want 1 (no publish)", got.Version)
	}

	// The ON-DISK source rolled back to the last good build — a deliberately
	// build-failing publish must NOT leave the (tampered) source running in the
	// live dev preview. The previous good App.tsx is restored; the failed-attempt
	// bytes are gone.
	src, err := store.Source(app.ID)
	if err != nil {
		t.Fatalf("Source after failed publish: %v", err)
	}
	if src["src/App.tsx"] != goodApp {
		t.Fatalf("failed publish did not roll back source; src/App.tsx = %q", src["src/App.tsx"])
	}
}

// TestPublishHTMLOnlyFallback: a registration with NO source Files falls back to
// the submitted html (built-in / simple app path), with no build invoked.
func TestPublishHTMLOnlyFallback(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	// Wire a builder that fails loudly if called — the html-only path must never
	// build.
	store.buildBundle = func(string) ([]byte, error) {
		t.Fatalf("html-only registration must not invoke the build")
		return nil, nil
	}
	now := time.Unix(1_700_000_000, 0).UTC()

	app, err := store.Save(CustomAppWriteRequest{
		Name:  "Static Tool",
		Actor: "app-builder",
		HTML:  validAppHTML,
	}, now)
	if err != nil {
		t.Fatalf("html-only Save: %v", err)
	}
	_, html, err := store.Get(app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if html != validAppHTML {
		t.Fatalf("html-only publish did not store the submitted html")
	}
	// No source was persisted (nothing to edit/build later).
	src, err := store.Source(app.ID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if len(src) != 0 {
		t.Fatalf("html-only publish persisted source unexpectedly: %v", keysOf(src))
	}
}

// TestPublishWithFilesDiscardsAgentHTML proves req.HTML is never read when
// source Files are present: even an html that would FAIL the sandbox policy is
// harmlessly discarded, because the stored bundle is the host build, not the
// agent's html. (Without this, a future refactor that re-validated req.HTML would
// reject a publish whose html was always going to be thrown away.)
func TestPublishWithFilesDiscardsAgentHTML(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	store.buildBundle = stubBuildBundle
	now := time.Unix(1_700_000_000, 0).UTC()

	app, err := store.Save(CustomAppWriteRequest{
		Name:  "Tool",
		Actor: "app-builder",
		// An html that the sandbox policy would REJECT outright — it must be
		// ignored because Files are present.
		HTML: `<script src="https://evil.example/x.js"></script>`,
		Files: map[string]string{
			"package.json": "{}",
			"src/App.tsx":  "export default function App(){return null}",
			"src/main.tsx": testMantineMainTSX,
		},
	}, now)
	if err != nil {
		t.Fatalf("Save with Files + bad html should ignore the html: %v", err)
	}
	_, html, err := store.Get(app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(html, "evil.example") {
		t.Fatalf("agent html leaked into the stored bundle")
	}
}

// TestOverwriteProtectedFilesIsPure verifies overwriteProtectedFiles returns a
// new map with canonical bytes and never mutates the caller's input.
func TestOverwriteProtectedFilesIsPure(t *testing.T) {
	in := map[string]string{
		"src/App.tsx":         "app code",
		"src/wuphf-bridge.ts": "AGENT TAMPER",
	}
	out, err := overwriteProtectedFiles(in)
	if err != nil {
		t.Fatalf("overwriteProtectedFiles: %v", err)
	}
	// Input is untouched.
	if in["src/wuphf-bridge.ts"] != "AGENT TAMPER" {
		t.Fatalf("input map was mutated")
	}
	// Output carries the app's own file plus canonical protected files.
	if out["src/App.tsx"] != "app code" {
		t.Fatalf("app file lost in overwrite")
	}
	for p := range customAppProtectedFiles {
		want, err := canonicalProtectedFile(p)
		if err != nil {
			t.Fatalf("canonical %q: %v", p, err)
		}
		if out[p] != string(want) {
			t.Fatalf("protected file %q not canonical in output", p)
		}
	}
}

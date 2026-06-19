package team

import (
	"strings"
	"testing"
	"time"
)

// TestCheckAppSourceEfficiency_FlagsFocusRefetch is the core regression: the
// Gmail-digest waste pattern — re-running work on a visibilitychange listener —
// must be rejected at publish, deterministically.
func TestCheckAppSourceEfficiency_FlagsFocusRefetch(t *testing.T) {
	files := map[string]string{
		"src/App.tsx": `
import { useEffect } from "react";
import { getEmails, ai } from "./wuphf-bridge";

export function App() {
  useEffect(() => {
    const onVisibility = () => {
      if (document.visibilityState === "visible") void runDigest();
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);
  return null;
}`,
	}
	vs := checkAppSourceEfficiency(files)
	if len(vs) == 0 {
		t.Fatal("expected a focus-refetch violation, got none")
	}
	found := false
	for _, v := range vs {
		if v.Rule == "no-focus-refetch" && v.File == "src/App.tsx" {
			found = true
			if v.Line == 0 {
				t.Errorf("violation has no line number: %+v", v)
			}
		}
	}
	if !found {
		t.Fatalf("expected a no-focus-refetch violation on src/App.tsx, got %+v", vs)
	}
}

func TestCheckAppSourceEfficiency_FlagsWindowFocusAndHandlers(t *testing.T) {
	cases := map[string]string{
		"window.addEventListener focus": `window.addEventListener("focus", () => refetch());`,
		"document focus handler":        `document.onfocus = () => refetch();`,
		"onpageshow handler":            `window.onpageshow = () => refetch();`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			vs := checkAppSourceEfficiency(map[string]string{"src/App.tsx": body})
			if len(vs) == 0 {
				t.Fatalf("expected a focus violation for %q", body)
			}
		})
	}
}

// TestCheckAppSourceEfficiency_FlagsTightPoll catches a sub-floor setInterval but
// leaves a computed-delay timer and a slow poll alone.
func TestCheckAppSourceEfficiency_FlagsTightPoll(t *testing.T) {
	t.Run("literal sub-floor interval is flagged", func(t *testing.T) {
		files := map[string]string{"src/App.tsx": `setInterval(() => refetch(), 5000);`}
		vs := checkAppSourceEfficiency(files)
		if len(vs) != 1 || vs[0].Rule != "no-tight-poll" {
			t.Fatalf("expected one no-tight-poll violation, got %+v", vs)
		}
	})
	t.Run("comma inside the callback is not mistaken for the delay", func(t *testing.T) {
		// The inner foo(a, 5) must not be read as a 5ms delay; the real delay
		// (60000) is above the floor, so this is clean.
		files := map[string]string{"src/App.tsx": `setInterval(() => foo(a, 5), 60000);`}
		if vs := checkAppSourceEfficiency(files); len(vs) != 0 {
			t.Fatalf("expected no violation, got %+v", vs)
		}
	})
	t.Run("computed delay is not flagged", func(t *testing.T) {
		files := map[string]string{"src/App.tsx": `setInterval(refetch, msUntilNext());`}
		if vs := checkAppSourceEfficiency(files); len(vs) != 0 {
			t.Fatalf("computed-delay interval must not be flagged, got %+v", vs)
		}
	})
	t.Run("multiline setInterval below floor is flagged", func(t *testing.T) {
		files := map[string]string{"src/App.tsx": "setInterval(\n  () => refetch(),\n  10000,\n);"}
		if vs := checkAppSourceEfficiency(files); len(vs) != 1 {
			t.Fatalf("expected one violation for the multiline interval, got %+v", vs)
		}
	})
}

// TestCheckAppSourceEfficiency_OptInMarkerSuppresses honors the deterministic
// "the human asked for it" escape hatch.
func TestCheckAppSourceEfficiency_OptInMarkerSuppresses(t *testing.T) {
	t.Run("marker on the same line", func(t *testing.T) {
		files := map[string]string{
			"src/App.tsx": `setInterval(() => tick(), 1000); // wuphf-allow: poll — user asked for a 1s ticker`,
		}
		if vs := checkAppSourceEfficiency(files); len(vs) != 0 {
			t.Fatalf("marker should suppress the violation, got %+v", vs)
		}
	})
	t.Run("marker on the line above", func(t *testing.T) {
		files := map[string]string{
			"src/App.tsx": "// wuphf-allow: focus-refresh — user asked to refresh on return\nwindow.addEventListener(\"focus\", () => refetch());",
		}
		if vs := checkAppSourceEfficiency(files); len(vs) != 0 {
			t.Fatalf("marker above should suppress the violation, got %+v", vs)
		}
	})
}

// TestCheckAppSourceEfficiency_NoFalsePositives keeps the harness high-precision:
// legitimate, common code must publish clean.
func TestCheckAppSourceEfficiency_NoFalsePositives(t *testing.T) {
	clean := map[string]string{
		"src/App.tsx": `
import { useEffect } from "react";

export function App() {
  // A daily refresh timer uses a COMPUTED delay — allowed.
  useEffect(() => {
    const t = setTimeout(() => refresh(), msUntilNext9am());
    return () => clearTimeout(t);
  }, []);

  // An input element focus handler is legitimate UI, not a tab-level reaction.
  return <input onFocus={() => select()} />;
}`,
		// A comment or string that merely mentions the patterns must not trip.
		"src/notes.ts": `
// Do not add a visibilitychange listener here — it would refetch on focus.
const doc = "see window.addEventListener('focus', ...) for the antipattern";
const help = "setInterval(fn, 100) is too tight";`,
		// Protected + artifact files are skipped entirely.
		"src/wuphf-bridge.ts":     `window.addEventListener("focus", () => {});`,
		"node_modules/x/index.js": `setInterval(() => spin(), 10);`,
		"dist/index.html":         `<script>setInterval(()=>x(),5)</script>`,
	}
	if vs := checkAppSourceEfficiency(clean); len(vs) != 0 {
		t.Fatalf("clean source produced false positives: %+v", vs)
	}
}

// TestCheckAppSourceEfficiency_MultilineTemplate is the triangulation HIGH-1
// regression: a multi-line backtick template that MENTIONS the patterns is a
// string, not code, and must not false-flag (which would block a legit publish).
// Real code AFTER the template is still scanned.
func TestCheckAppSourceEfficiency_MultilineTemplate(t *testing.T) {
	t.Run("template content is not flagged", func(t *testing.T) {
		src := "const help = `\n" +
			"  addEventListener('visibilitychange', refetch)\n" +
			"  setInterval(x, 100)\n" +
			"`;\n" +
			"setInterval(refresh, msUntilNext());" // computed delay → clean
		if vs := checkAppSourceEfficiency(map[string]string{"src/App.tsx": src}); len(vs) != 0 {
			t.Fatalf("multi-line template content must not be flagged, got %+v", vs)
		}
	})
	t.Run("real code after a template is still scanned", func(t *testing.T) {
		src := "const sql = `\nSELECT 1\n`;\n" +
			"setInterval(() => refetch(), 5000);"
		vs := checkAppSourceEfficiency(map[string]string{"src/App.tsx": src})
		if len(vs) != 1 || vs[0].Rule != "no-tight-poll" {
			t.Fatalf("a real sub-floor setInterval after a template must be flagged, got %+v", vs)
		}
	})
}

// TestCheckAppSourceEfficiency_MarkerCommentScoped pins the opt-in scoping: only a
// real comment marker suppresses; the token inside a string or identifier does not.
func TestCheckAppSourceEfficiency_MarkerCommentScoped(t *testing.T) {
	src := `const note = "wuphf-allow: not really an opt-in";` + "\n" +
		`setInterval(() => tick(), 1000);`
	vs := checkAppSourceEfficiency(map[string]string{"src/App.tsx": src})
	if len(vs) != 1 {
		t.Fatalf("a string containing the marker must not suppress, got %+v", vs)
	}
}

// TestAppEfficiencyGuardError surfaces every violation with file:line so the
// agent can fix them in one pass.
func TestAppEfficiencyGuardError(t *testing.T) {
	err := appEfficiencyGuardError([]appGuardViolation{
		{File: "src/App.tsx", Line: 12, Rule: "no-focus-refetch", Message: "x"},
		{File: "src/Poll.tsx", Line: 3, Rule: "no-tight-poll", Message: "y"},
	})
	if !isCustomAppCallerError(err) {
		t.Fatal("guard error must be a caller error (maps to HTTP 400)")
	}
	msg := err.Error()
	for _, want := range []string{"src/App.tsx:12", "no-focus-refetch", "src/Poll.tsx:3", "no-tight-poll"} {
		if !strings.Contains(msg, want) {
			t.Errorf("guard error missing %q in:\n%s", want, msg)
		}
	}
}

// TestCheckAppStackConformance pins the deterministic "design within Mantine"
// gate: a compliant app passes, an off-stack app (hand-rolled, no Mantine) is
// rejected, the opt-out marker allows it, and an html-only app is skipped.
func TestCheckAppStackConformance(t *testing.T) {
	t.Run("compliant app passes", func(t *testing.T) {
		files := map[string]string{
			"src/main.tsx": testMantineMainTSX,
			"src/App.tsx":  `import { Table } from "@mantine/core"; export function App(){ return <Table/>; }`,
		}
		if v := checkAppStackConformance(files); len(v) != 0 {
			t.Fatalf("compliant app flagged: %+v", v)
		}
	})
	t.Run("off-stack app is rejected", func(t *testing.T) {
		files := map[string]string{
			"src/main.tsx": `import { createRoot } from "react-dom/client"; createRoot(el).render(<App/>);`,
			"src/App.tsx":  `export function App(){ return <div className="x"/>; }`,
		}
		v := checkAppStackConformance(files)
		if len(v) != 1 || v[0].Rule != "use-mantine" {
			t.Fatalf("expected a use-mantine violation, got %+v", v)
		}
	})
	t.Run("opt-out marker allows non-Mantine", func(t *testing.T) {
		files := map[string]string{
			"src/main.tsx": "// wuphf-allow: no-mantine — human asked for a bare canvas\nrender(<App/>);",
		}
		if v := checkAppStackConformance(files); len(v) != 0 {
			t.Fatalf("opt-out should pass, got %+v", v)
		}
	})
	t.Run("html-only app skipped", func(t *testing.T) {
		if v := checkAppStackConformance(map[string]string{"index.html": "<html></html>"}); len(v) != 0 {
			t.Fatalf("html-only app should be skipped, got %+v", v)
		}
	})
}

// TestPublishRejectsWastefulSourceViaSave is the end-to-end gate: a publish whose
// source re-runs work on tab focus is rejected by Save BEFORE the build, as a
// caller error (HTTP 400) naming the rule — while a clean app with a
// computed-delay timer publishes normally. This wires the guard into the
// host-owned build path the agent cannot bypass.
func TestPublishRejectsWastefulSourceViaSave(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	store.buildBundle = stubBuildBundle
	now := time.Unix(1_700_000_000, 0).UTC()

	_, err := store.Save(CustomAppWriteRequest{
		Name:  "Burner",
		Actor: "app-builder",
		Files: map[string]string{
			"package.json": "{}",
			"src/App.tsx":  `document.addEventListener("visibilitychange", () => refetch());`,
		},
	}, now)
	if err == nil {
		t.Fatal("expected the efficiency harness to reject the wasteful publish")
	}
	if !isCustomAppCallerError(err) {
		t.Fatalf("guard rejection must be a caller error (HTTP 400), got %v", err)
	}
	if !strings.Contains(err.Error(), "no-focus-refetch") {
		t.Fatalf("rejection should name the rule, got: %v", err)
	}

	// A clean app (computed-delay refresh timer, Mantine-compliant) publishes fine.
	app, err := store.Save(CustomAppWriteRequest{
		Name:  "Clean",
		Actor: "app-builder",
		Files: map[string]string{
			"package.json": "{}",
			"src/App.tsx":  `const t = setTimeout(() => refresh(), msUntilNext9am());`,
			"src/main.tsx": testMantineMainTSX,
		},
	}, now)
	if err != nil {
		t.Fatalf("clean app should publish, got: %v", err)
	}
	if app.ID == "" {
		t.Fatal("clean publish returned no app id")
	}
}

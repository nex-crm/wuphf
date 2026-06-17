package workflowpress

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// generated_drift_test.go is the regenerate-on-change enforcement for the
// generated-tool ↔ kernel coupling policy (triangulation architect #2). The
// committed golden tree under testdata/generated/<id>/ is the EXACT output the
// current kernel emits for each of the three ground-truth example specs. This
// test regenerates all three and asserts the bytes are identical to what is
// committed.
//
// Why this matters: a generated tool both imports this kernel AND embeds a spec,
// on two axes that were previously unversioned against each other. A kernel or
// template change that would alter generated output — or break a committed tool —
// must NOT pass silently. With the golden committed, any such change makes this
// test FAIL, forcing the author to regenerate (regenerate-on-bump policy) and
// re-review the diff. That is the CI hook that keeps the kernel and every
// generated tool in lockstep.
//
// To regenerate after an intentional kernel/template/spec change:
//
//	go test ./internal/workflowpress -run TestGeneratedOutputMatchesCommitted -update
//
// then review and commit the diff under testdata/generated/. CI runs WITHOUT
// -update, so an un-regenerated change fails the build. The flag is registered at
// package scope (not via testing.M) so it is available to `go test -update`
// without a custom TestMain.
var updateGolden = flag.Bool("update", false,
	"regenerate the committed testdata/generated golden tree instead of comparing")

// generatedGoldenDir is the committed golden tree root, relative to the package.
const generatedGoldenDir = "testdata/generated"

// TestGeneratedOutputMatchesCommitted regenerates each ground-truth example tool
// and asserts byte-identical equality with the committed golden tree. A template
// tweak, a kernel-version bump, a runner contract change, or a spec edit that
// changes generated output all surface here as a diff — the regenerate-on-change
// guard. Run with -update to refresh the golden after an intentional change.
func TestGeneratedOutputMatchesCommitted(t *testing.T) {
	// NOT parallel: with -update it writes the golden tree, and the comparison
	// path reads files; keep it serial so an -update run is deterministic.
	for _, name := range exampleNames {
		spec := loadExample(t, name)
		gen, err := Generate(spec)
		if err != nil {
			t.Fatalf("Generate(%s): %v", name, err)
		}

		// The generated file map is keyed by "<id>/<file>"; the golden tree mirrors
		// it under testdata/generated/<id>/<file>. Sort paths for stable iteration.
		paths := make([]string, 0, len(gen.Files))
		for p := range gen.Files {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		if *updateGolden {
			writeGolden(t, name, gen.Files, paths)
			continue
		}
		compareGolden(t, name, gen.Files, paths)
	}
}

// writeGolden rewrites the committed golden tree for one workflow from the freshly
// generated files. It removes the existing per-workflow dir first so a renamed or
// removed generated file does not leave a stale golden behind.
func writeGolden(t *testing.T, name string, files map[string][]byte, paths []string) {
	t.Helper()
	wfDir := filepath.Join(generatedGoldenDir, name)
	if err := os.RemoveAll(wfDir); err != nil {
		t.Fatalf("%s: clearing stale golden: %v", name, err)
	}
	for _, p := range paths {
		dst := filepath.Join(generatedGoldenDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("%s: mkdir %s: %v", name, filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, files[p], 0o644); err != nil {
			t.Fatalf("%s: writing golden %s: %v", name, dst, err)
		}
	}
	t.Logf("%s: wrote %d golden files under %s", name, len(paths), wfDir)
}

// compareGolden asserts the committed golden tree for one workflow matches the
// freshly generated files exactly: same set of files, same bytes. It is the drift
// guard's assertion half.
func compareGolden(t *testing.T, name string, files map[string][]byte, paths []string) {
	t.Helper()

	// Every generated file must exist in the golden with identical bytes.
	committed := map[string]struct{}{}
	for _, p := range paths {
		golden := filepath.Join(generatedGoldenDir, filepath.FromSlash(p))
		committed[golden] = struct{}{}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Errorf("%s: missing committed golden %s (run with -update to regenerate): %v", name, golden, err)
			continue
		}
		if string(want) != string(files[p]) {
			t.Errorf("%s: generated output for %s drifted from the committed golden.\n"+
				"A kernel/template/spec change altered generated code without regenerating.\n"+
				"If intentional, run: go test ./internal/workflowpress -run TestGeneratedOutputMatchesCommitted -update",
				name, p)
		}
	}

	// The golden tree must not carry STALE files the generator no longer emits
	// (e.g. a removed template). Walk the committed dir and flag any file the
	// current generation did not produce. A walk error is propagated so a broken
	// golden tree surfaces rather than being swallowed.
	wfDir := filepath.Join(generatedGoldenDir, name)
	if err := filepath.Walk(wfDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, ok := committed[path]; !ok {
			t.Errorf("%s: committed golden %s is stale — the generator no longer emits it (run with -update)", name, path)
		}
		return nil
	}); err != nil {
		t.Errorf("%s: walking committed golden tree %s: %v", name, wfDir, err)
	}
}

package workflowpress

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// version_compat_test.go is the Phase D regression suite for the generated-tool ↔
// kernel coupling policy: the kernel-version stamp + compat assertion, and the
// simulated-drift proof that the regenerate-and-compare guard actually catches a
// template change.

// TestRequireKernelCompatAcceptsCurrent is the positive control: a tool stamped
// with the current kernel + spec-schema versions is compatible.
func TestRequireKernelCompatAcceptsCurrent(t *testing.T) {
	t.Parallel()
	if err := RequireKernelCompat(KernelVersion, SchemaVersionWorkflowSpec); err != nil {
		t.Fatalf("RequireKernelCompat rejected the current versions: %v", err)
	}
}

// TestRequireKernelCompatRejectsKernelMismatch proves a tool generated against a
// different KERNEL version fails closed — both an older and a newer kernel,
// because the kernel cannot prove compatibility across a bump in either
// direction.
func TestRequireKernelCompatRejectsKernelMismatch(t *testing.T) {
	t.Parallel()
	for _, toolKV := range []int{KernelVersion - 1, KernelVersion + 1, KernelVersion + 99} {
		toolKV := toolKV
		t.Run(label(toolKV), func(t *testing.T) {
			t.Parallel()
			err := RequireKernelCompat(toolKV, SchemaVersionWorkflowSpec)
			if err == nil {
				t.Fatalf("RequireKernelCompat accepted tool kernel v%d, want rejection", toolKV)
			}
			if !errors.Is(err, ErrKernelIncompatible) {
				t.Fatalf("err = %v, want wrapping ErrKernelIncompatible", err)
			}
		})
	}
}

// TestRequireKernelCompatRejectsSchemaMismatch proves the second coupling axis is
// asserted too: a tool generated against a different spec wire-format version
// fails closed even when the kernel version matches.
func TestRequireKernelCompatRejectsSchemaMismatch(t *testing.T) {
	t.Parallel()
	for _, toolSV := range []int{SchemaVersionWorkflowSpec - 1, SchemaVersionWorkflowSpec + 1} {
		toolSV := toolSV
		t.Run(label(toolSV), func(t *testing.T) {
			t.Parallel()
			err := RequireKernelCompat(KernelVersion, toolSV)
			if err == nil {
				t.Fatalf("RequireKernelCompat accepted tool schema v%d, want rejection", toolSV)
			}
			if !errors.Is(err, ErrKernelIncompatible) {
				t.Fatalf("err = %v, want wrapping ErrKernelIncompatible", err)
			}
		})
	}
}

// TestGeneratedToolStampsKernelVersion pins that the generator stamps BOTH
// coupling axes into the tool and wires loadSpec to assert them. If a future
// template edit drops the stamp or the compat call, the coupling guarantee is
// gone and this fails — the stamp is a generated-code property, not only a kernel
// constant.
func TestGeneratedToolStampsKernelVersion(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	gen, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src := string(gen.Files[spec.ID+"/workflow.go"])
	for _, want := range []string{
		"generatedKernelVersion = 1",
		"generatedSchemaVersion = 1",
		"wp.RequireKernelCompat(generatedKernelVersion, generatedSchemaVersion)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated workflow.go missing %q; the kernel-version coupling assertion is not stamped", want)
		}
	}
}

// TestGeneratedStampTracksKernelConstant guards against a stale literal in the
// committed golden: the stamped generatedKernelVersion in every committed
// workflow.go must equal the current KernelVersion constant. If KernelVersion is
// bumped without regenerating, this fails — a second, human-readable signal
// alongside the byte-level drift guard.
func TestGeneratedStampTracksKernelConstant(t *testing.T) {
	t.Parallel()
	wantKV := "generatedKernelVersion = " + itoa(KernelVersion)
	wantSV := "generatedSchemaVersion = " + itoa(SchemaVersionWorkflowSpec)
	for _, name := range exampleNames {
		path := filepath.Join(generatedGoldenDir, name, "workflow.go")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading committed %s: %v", path, err)
		}
		src := string(raw)
		if !strings.Contains(src, wantKV) {
			t.Errorf("%s: committed stamp does not match KernelVersion=%d (regenerate the golden)", name, KernelVersion)
		}
		if !strings.Contains(src, wantSV) {
			t.Errorf("%s: committed stamp does not match SchemaVersionWorkflowSpec=%d (regenerate the golden)", name, SchemaVersionWorkflowSpec)
		}
	}
}

// TestDriftGuardCatchesTemplateTweak is THE regression for the coupling policy:
// it proves the regenerate-and-compare guard actually FAILS when a kernel/template
// change alters generated output. It simulates a template tweak by re-rendering
// workflow.go with a perturbed template body, then asserts the perturbed bytes
// differ from the committed golden — i.e. the comparison the real guard performs
// would fail and force a regenerate. This is the safety net for the safety net:
// if the guard could ever be silently satisfied by drifted output, this test
// fails first.
func TestDriftGuardCatchesTemplateTweak(t *testing.T) {
	t.Parallel()

	spec := loadExample(t, "trial-to-ae-routing")
	view, err := newSpecView(spec)
	if err != nil {
		t.Fatalf("newSpecView: %v", err)
	}

	// A minimal, representative template tweak: append a harmless trailing comment
	// to the workflow.go body. Any real kernel/template change that alters emitted
	// bytes is equivalent for the guard's purposes.
	tweaked := tmplWorkflow + "\n// drift: simulated kernel/template change\n"
	tmpl := template.Must(template.New("workflow.go").Funcs(templateFuncs).Parse(tweaked))
	rendered, err := renderTemplate(tmpl, view, true)
	if err != nil {
		t.Fatalf("rendering tweaked template: %v", err)
	}

	committedPath := filepath.Join(generatedGoldenDir, spec.ID, "workflow.go")
	committed, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("reading committed golden %s: %v", committedPath, err)
	}

	if string(rendered) == string(committed) {
		t.Fatal("a perturbed template produced output identical to the committed golden; " +
			"the regenerate-and-compare drift guard would NOT catch a template change")
	}

	// And the SAME view through the UNMODIFIED template still matches the committed
	// golden — proving the difference above is caused by the tweak, not by view
	// nondeterminism. This is the positive control for the negative assertion.
	baseTmpl := template.Must(template.New("workflow.go").Funcs(templateFuncs).Parse(tmplWorkflow))
	base, err := renderTemplate(baseTmpl, view, true)
	if err != nil {
		t.Fatalf("rendering base template: %v", err)
	}
	if string(base) != string(committed) {
		t.Fatalf("unmodified template output differs from the committed golden; "+
			"regenerate the golden (go test -run TestGeneratedOutputMatchesCommitted -update). diff at %s", committedPath)
	}
}

// label renders an int as a subtest name.
func label(n int) string { return itoa(n) }

// itoa is a tiny local int→string without pulling strconv into more call sites in
// this file; the values here are small.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

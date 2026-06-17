package workflowpress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"strconv"
	"strings"
	"text/template"
)

// generate.go is the Generator: deterministic, TEMPLATED emission of the local
// internal tool from a frozen WorkflowSpec. It is INSIDE the kernel.
//
// Determinism is the load-bearing property — the same spec MUST produce
// byte-identical output. Every template iterates the spec in its declared order
// (or a sorted order for maps), no timestamps or random ids leak in, and the file
// map is built from a fixed set of paths. The shipcheck's "same spec, same bytes"
// assertion (next phase) depends on this.
//
// What it emits (the operator's tool), all under a workflow-id directory:
//
//   - workflow.go    — the generated package: the embedded spec, a typed state /
//                      event / action enumeration, and a Run entrypoint that
//                      drives the kernel Runner. This is the runtime.
//   - types.go       — the entity types the workflow moves (one struct per
//                      Entity, fields as strings — the contract is the schema).
//   - exceptions.go  — the known exceptions as typed, documented constants so the
//                      handling is discoverable in code.
//   - state.go       — the state-machine tables (states, transitions, guards) as
//                      data, the single source the runner reads.
//   - inngest.go     — the durable adapter: the InngestStep plan plus a Run that
//                      delegates to the same Runner, so the durable path is
//                      behavior-equivalent to the local one.
//   - fixtures.json  — the verification fixtures lifted from the spec, the inputs
//                      the generated test replays.
//   - workflow.md    — operator documentation of the generated tool.
//   - workflow_test.go — a generated test that replays every verification
//                      scenario through the runtime and asserts its transitions.
//
// None of this is executed by the generator; it is source the operator reviews,
// and any run goes through the Executor seam (fail-closed in this phase).

// Generate emits the deterministic local workflow from a frozen spec. It is pure:
// given the same spec it returns a GeneratedWorkflow whose Files are
// byte-identical across calls and processes. It validates the spec first so a
// malformed contract never produces a tool.
//
// The receiver is a value so Generate can be called without constructing a
// Generator, while TemplateGenerator still satisfies the kernel's Generator
// interface for callers that want the seam.
func Generate(spec *WorkflowSpec) (*GeneratedWorkflow, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: %w: spec is nil", ErrInvalidSpec, ErrEmptyField)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("workflowpress: cannot generate from invalid spec: %w", err)
	}

	view, err := newSpecView(spec)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(generatedTemplates)+1)

	for _, gt := range generatedTemplates {
		out, err := renderTemplate(gt.tmpl, view, strings.HasSuffix(gt.suffix, ".go"))
		if err != nil {
			return nil, fmt.Errorf("workflowpress: rendering template %q: %w", gt.suffix, err)
		}
		files[resolvePath(view.Dir, gt.suffix)] = out
	}

	// fixtures.json is emitted directly (not via text/template) so the JSON is
	// canonical and stable. Marshal with sorted keys via the view's fixture model.
	fixtures, err := marshalFixtures(view)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: marshalling fixtures: %w", err)
	}
	files[view.Dir+"/fixtures.json"] = fixtures

	return &GeneratedWorkflow{
		WorkflowID: spec.ID,
		Version:    spec.Version,
		Files:      files,
	}, nil
}

// renderTemplate executes one template against the view and, for Go files,
// gofmt-canonicalises the output. It is the single render step Generate uses for
// every templated file, so the production path and any test that re-renders a
// template (e.g. the simulated-drift guard) produce bytes the same way — gofmt
// is deterministic, preserving the "same spec, same bytes" guarantee.
func renderTemplate(tmpl *template.Template, view specView, gofmt bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	out := buf.Bytes()
	if gofmt {
		formatted, err := format.Source(out)
		if err != nil {
			return nil, fmt.Errorf("gofmt of generated source failed: %w", err)
		}
		out = formatted
	}
	return out, nil
}

// TemplateGenerator is the kernel Generator implemented over Generate. It lets a
// caller hold the kernel seam (accept interfaces) while the work stays a pure
// function (return structs).
type TemplateGenerator struct{}

// NewGenerator returns the deterministic templated generator.
func NewGenerator() Generator { return TemplateGenerator{} }

// Generate satisfies the Generator interface. The context is honoured for
// cancellation only; emission itself is pure.
func (TemplateGenerator) Generate(ctx context.Context, spec *WorkflowSpec) (*GeneratedWorkflow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("workflowpress: generate context: %w", err)
	}
	return Generate(spec)
}

// --- the view model the templates render ---

// specView is the deterministic projection of a WorkflowSpec the templates read.
// It carries the package name, the generated directory, and the spec embedded as
// canonical JSON (so the runtime can decode the exact contract it was generated
// from). Slices preserve spec order; the few maps are rendered through sorted
// accessors only.
type specView struct {
	Spec    *WorkflowSpec
	Dir     string
	Package string
	// SpecJSON is the indented canonical JSON of the spec (used for any human-
	// readable surface). SpecJSONLiteral is that same JSON wrapped in a
	// strconv.Quote-escaped Go double-quoted string literal, so the generated
	// `const specJSON = ...` survives any byte in the spec's free text (a backtick
	// would terminate a raw-string literal early).
	SpecJSON        string
	SpecJSONLiteral string
	Scenarios       []scenarioView
	Steps           []InngestStep
	StateNames      []string
	TerminalSet     map[string]bool
	// KernelVersion and SchemaVersion are the two coupling axes stamped into the
	// generated tool: the version of the kernel it was generated against and the
	// spec wire-format version it was generated for. The generated loadSpec asserts
	// both against the kernel it links via wp.RequireKernelCompat, so a kernel or
	// wire-format bump that would break the tool fails loudly at load instead of
	// running on a mismatched runtime. See version.go.
	KernelVersion int
	SchemaVersion int
}

// scenarioView is one verification scenario flattened for template/test emission,
// with its Given fixture rendered in deterministic key order.
type scenarioView struct {
	Name           string
	When           string
	GivenKeys      []string
	Given          map[string]string
	Expect         []Transition
	ExpectApproval bool
}

// newSpecView builds the deterministic view from a validated spec. It returns an
// error when the spec cannot be marshalled to its embedded JSON form — previously
// that error was silently dropped, leaving an empty specJSON in the generated tool
// (the runtime would then fail to decode an empty contract).
func newSpecView(spec *WorkflowSpec) (specView, error) {
	pkg := packageName(spec.ID)
	v := specView{
		Spec:    spec,
		Dir:     spec.ID,
		Package: pkg,
		Steps:   PlanInngestSteps(spec),
		// Stamp both coupling axes at generation time. KernelVersion ties the tool
		// to the kernel runtime/templates it was generated against; SchemaVersion is
		// the generator's view of the spec wire shape. loadSpec asserts both.
		KernelVersion: KernelVersion,
		SchemaVersion: SchemaVersionWorkflowSpec,
		TerminalSet:   make(map[string]bool, len(spec.States)),
	}
	for _, st := range spec.States {
		v.StateNames = append(v.StateNames, st.Name)
		if st.Terminal {
			v.TerminalSet[st.Name] = true
		}
	}
	// Embed the spec as canonical, indented JSON so the generated runtime decodes
	// the exact contract. Marshalling a struct is deterministic in Go (struct field
	// order is fixed; maps inside are the Given fixtures, handled separately).
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return specView{}, fmt.Errorf("workflowpress: marshalling spec for embedding: %w", err)
	}
	v.SpecJSON = string(b)
	// Emit the embedded JSON as a strconv.Quote-escaped Go string literal so the
	// generated `const specJSON = ...` survives any byte (notably a backtick in a
	// free-text field, which json.MarshalIndent does not escape and which would
	// terminate a raw-string literal early). The quoted form decodes identically.
	v.SpecJSONLiteral = strconv.Quote(v.SpecJSON)
	for _, sc := range spec.VerificationScenarios {
		v.Scenarios = append(v.Scenarios, scenarioView{
			Name:           sc.Name,
			When:           sc.When,
			GivenKeys:      sortedKeys(sc.Given),
			Given:          sc.Given,
			Expect:         sc.ExpectTransitions,
			ExpectApproval: sc.ExpectApproval,
		})
	}
	return v, nil
}

// marshalFixtures emits the verification fixtures as canonical JSON in scenario
// order with sorted Given keys, so the bytes are stable across runs.
func marshalFixtures(v specView) ([]byte, error) {
	type fixtureField struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	type fixture struct {
		Name           string         `json:"name"`
		When           string         `json:"when"`
		Given          []fixtureField `json:"given"`
		Expect         []Transition   `json:"expect_transitions"`
		ExpectApproval bool           `json:"expect_approval"`
	}
	out := struct {
		WorkflowID string    `json:"workflow_id"`
		Version    int       `json:"version"`
		Fixtures   []fixture `json:"fixtures"`
	}{WorkflowID: v.Spec.ID, Version: v.Spec.Version}

	for _, sc := range v.Scenarios {
		f := fixture{Name: sc.Name, When: sc.When, Expect: sc.Expect, ExpectApproval: sc.ExpectApproval}
		for _, k := range sc.GivenKeys {
			f.Given = append(f.Given, fixtureField{Key: k, Value: sc.Given[k]})
		}
		out.Fixtures = append(out.Fixtures, f)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// packageName derives a valid Go package identifier from a workflow id slug
// (trial-to-ae-routing -> trialtoaerouting). Deterministic; collapses any
// non-alphanumeric rune and lowercases.
func packageName(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "wf" + name
	}
	return name
}

// --- template helpers (pure, deterministic) ---

// templateFuncs are the deterministic helpers the templates use. None depend on
// time, randomness, or map iteration order.
var templateFuncs = template.FuncMap{
	"export": exportIdent,
	// quote renders a Go double-quoted string literal for any stringy value
	// (string or a defined string type like ActionKind / EventTrigger), so the
	// generated source quotes typed enums correctly.
	"quote": func(v any) string { return fmt.Sprintf("%q", fmt.Sprintf("%v", v)) },
}

// exportIdent turns a slug/snake/kebab name into an exported Go identifier
// (route_to_ae -> RouteToAe; trial_signed_up -> TrialSignedUp).
func exportIdent(name string) string {
	parts := splitIdent(name)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	out := b.String()
	if out == "" {
		out = "X"
	}
	return out
}

// splitIdent splits on the common slug separators.
func splitIdent(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.'
	})
}

// generatedTemplate pairs an output-file suffix with its parsed template. The
// full file-map key is view.Dir + "/" + suffix, computed in Generate so it stays
// deterministic per spec.
type generatedTemplate struct {
	suffix string
	tmpl   *template.Template
}

// generatedTemplates is the fixed, ordered set of templated files, built once at
// init from the source template bodies below. Order is fixed so iteration is
// deterministic; the file-map itself is keyed by path, so order only governs
// which template overwrites another on a (never-occurring) key collision.
var generatedTemplates []generatedTemplate

// templateSpec pairs a file suffix with its template body.
type templateSpec struct {
	suffix string
	body   string
}

func init() {
	specs := []templateSpec{
		{"workflow.go", tmplWorkflow},
		{"types.go", tmplTypes},
		{"exceptions.go", tmplExceptions},
		{"state.go", tmplState},
		{"inngest.go", tmplInngest},
		{"workflow.md", tmplDoc},
		{"workflow_test.go", tmplTest},
	}
	for _, s := range specs {
		t := template.Must(template.New(s.suffix).Funcs(templateFuncs).Parse(s.body))
		generatedTemplates = append(generatedTemplates, generatedTemplate{suffix: s.suffix, tmpl: t})
	}
}

// resolvePath joins the workflow directory and a file suffix into the file-map
// key (trial-to-ae-routing/workflow.go).
func resolvePath(dir, suffix string) string { return dir + "/" + suffix }

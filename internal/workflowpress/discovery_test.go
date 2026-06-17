package workflowpress

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadEvidence reads and decodes a raw-evidence fixture for one ground-truth
// example. These fixtures are synthetic-but-realistic: they carry concrete URLs,
// secret-laden headers/bodies, and sample records, exactly the messy shape the
// recorder hands Discover.
func loadEvidence(t *testing.T, name string) RawEvidence {
	t.Helper()
	path := filepath.Join("testdata", "evidence", name+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading evidence %q: %v", path, err)
	}
	var ev RawEvidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("decoding evidence %q: %v", path, err)
	}
	return ev
}

// essentialSignals declares, per example, the load-bearing signals Discover must
// surface from the raw evidence: the entities the schema inference must find and
// the templated endpoints the URL collapsing must produce. The test asserts each
// is present in the distilled research.
var essentialSignals = map[string]struct {
	entities          []string // entities that must appear in InferredSchemas
	endpointTemplates []string // host+template fragments that must appear
}{
	"trial-to-ae-routing": {
		entities:          []string{"TrialSignup", "Company", "AccountExecutive"},
		endpointTemplates: []string{"api.crm.test/accounts/{id}", "chat.test/channels/deals/messages"},
	},
	"renewal-risk-sweep": {
		entities:          []string{"Account", "UsageTrend", "CSTask"},
		endpointTemplates: []string{"warehouse.test/usage/accounts/{id}/trend", "api.crm.test/accounts/{id}/tasks"},
	},
	"inbound-lead-dedupe-merge": {
		entities:          []string{"InboundLead", "Contact", "MatchCandidate"},
		endpointTemplates: []string{"api.crm.test/contacts/{id}/merge", "api.crm.test/contacts/search"},
	},
}

// knownSecrets are byte sequences planted in the evidence fixtures that MUST NOT
// appear anywhere in the distilled research. This is the leak oracle for the
// "NO secrets leaked" half of the spec requirement.
var knownSecrets = []string{
	"sk_live_FAKEdoc0",
	"sk_test_FAKEold0",
	"AKIAFAKEEXAMPLE00",
	"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.s5y3aJ8Qk2",
	"xoxb-FAKEslackbot00",
	"sk_live_FAKEren0",
	"whk_live_8f3c9a1b2d4e6f7081923344556677aa",
	"ghp_FAKEgithubtok000",
	"eyJhbGciOiJIUzI1NiJ9.eyJ3aCI6InByb2QifQ.aZ9Qd2k",
	"hunter2-do-not-share",
	"cm9vdDpodW50ZXIy",
	"abcdef0123456789abcdef0123456789",
	"sk_live_FAKEjor0",
}

// TestDiscoverYieldsEssentialSignalsWithNoSecrets is the core Phase 2 test: for
// each ground-truth example, Discover(evidence) yields research that (a) carries
// the example's essential entities and templated endpoints and (b) leaks NO
// secret anywhere in the object.
func TestDiscoverYieldsEssentialSignalsWithNoSecrets(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ev := loadEvidence(t, name)
			research, err := Discover(ev)
			if err != nil {
				t.Fatalf("Discover(%s) error: %v", name, err)
			}
			if research.WorkflowID != name {
				t.Errorf("WorkflowID = %q, want %q", research.WorkflowID, name)
			}

			want := essentialSignals[name]

			// (a1) essential entities present in the inferred schemas.
			gotEntities := make(map[string]InferredSchema, len(research.InferredSchemas))
			for _, s := range research.InferredSchemas {
				gotEntities[s.Entity] = s
			}
			for _, e := range want.entities {
				if _, ok := gotEntities[e]; !ok {
					t.Errorf("inferred schemas missing essential entity %q; got %v", e, schemaEntityNames(research.InferredSchemas))
				}
			}

			// (a2) essential endpoint templates present (host+template fragment).
			endpointBlob := endpointsBlob(research.InferredEndpoints)
			for _, frag := range want.endpointTemplates {
				if !strings.Contains(endpointBlob, frag) {
					t.Errorf("inferred endpoints missing template fragment %q; got:\n%s", frag, endpointBlob)
				}
			}

			// (a3) at least one endpoint was actually templated (proves collapsing
			// ran, not just pass-through).
			if !strings.Contains(endpointBlob, "{id}") {
				t.Errorf("no endpoint was templated to {id}; got:\n%s", endpointBlob)
			}

			// (b) NO secret survives anywhere in the marshalled research.
			raw, err := json.Marshal(research)
			if err != nil {
				t.Fatalf("marshalling research: %v", err)
			}
			blob := string(raw)
			for _, secret := range knownSecrets {
				if strings.Contains(blob, secret) {
					t.Errorf("LEAKED secret %q in distilled research for %s", secret, name)
				}
			}

			// The distilled research must itself be schema-valid (Discover validates
			// before returning; re-assert for defence in depth).
			if err := ValidateResearchJSON(toGeneric(research)); err != nil {
				t.Errorf("distilled research failed schema validation: %v", err)
			}
		})
	}
}

// TestDiscoverIsDeterministic proves Discover is pure: the same evidence yields
// byte-identical research across runs (sorting + no map iteration in output).
func TestDiscoverIsDeterministic(t *testing.T) {
	t.Parallel()
	ev := loadEvidence(t, "trial-to-ae-routing")
	first, err := Discover(ev)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	second, err := Discover(ev)
	if err != nil {
		t.Fatalf("Discover (second): %v", err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("Discover not deterministic:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// TestDiscoverDoesNotMutateInput proves Discover treats its RawEvidence as
// immutable: the caller's evidence reads identically before and after, even
// though redaction scrubs the research copy. A snapshot is compared by value.
func TestDiscoverDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	ev := loadEvidence(t, "inbound-lead-dedupe-merge")
	before, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := Discover(ev); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	after, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("snapshot after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("Discover mutated its input evidence\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestDiscoverRejectsEmptyWorkflowID proves the one structural input gate.
func TestDiscoverRejectsEmptyWorkflowID(t *testing.T) {
	t.Parallel()
	_, err := Discover(RawEvidence{WorkflowID: "   "})
	if !errors.Is(err, ErrEmptyField) {
		t.Fatalf("Discover(empty id) = %v, want ErrEmptyField", err)
	}
}

// TestTemplateEndpoints covers the endpoint-templating inference directly.
func TestTemplateEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		traces   []HTTPTrace
		wantTmpl string
		wantHost string
		wantN    int // SampleCount expected for the single endpoint
	}{
		{
			name: "numeric ids collapse and dedupe",
			traces: []HTTPTrace{
				{Method: "get", URL: "https://api.test/accounts/123"},
				{Method: "GET", URL: "https://api.test/accounts/456"},
				{Method: "GET", URL: "https://api.test/accounts/789?token=sk_live_x"},
			},
			wantTmpl: "/accounts/{id}",
			wantHost: "api.test",
			wantN:    3,
		},
		{
			name: "uuid collapses",
			traces: []HTTPTrace{
				{Method: "GET", URL: "https://api.test/orgs/3f2504e0-4f89-41d3-9a0c-0305e82c3301/members"},
			},
			wantTmpl: "/orgs/{id}/members",
			wantHost: "api.test",
			wantN:    1,
		},
		{
			name: "opaque object id collapses",
			traces: []HTTPTrace{
				{Method: "GET", URL: "https://api.test/contacts/con_501abc9988776655/merge"},
			},
			wantTmpl: "/contacts/{id}/merge",
			wantHost: "api.test",
			wantN:    1,
		},
		{
			name: "pure word segments are preserved",
			traces: []HTTPTrace{
				{Method: "GET", URL: "https://api.test/contacts/search"},
			},
			wantTmpl: "/contacts/search",
			wantHost: "api.test",
			wantN:    1,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eps := templateEndpoints(tc.traces)
			if len(eps) != 1 {
				t.Fatalf("got %d endpoints, want 1: %+v", len(eps), eps)
			}
			ep := eps[0]
			if ep.Template != tc.wantTmpl {
				t.Errorf("Template = %q, want %q", ep.Template, tc.wantTmpl)
			}
			if ep.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", ep.Host, tc.wantHost)
			}
			if ep.SampleCount != tc.wantN {
				t.Errorf("SampleCount = %d, want %d", ep.SampleCount, tc.wantN)
			}
		})
	}
}

// TestInferSchemasCountBasedNullability covers nullability inference: a field in
// every record is required; a field in only some is nullable.
func TestInferSchemasCountBasedNullability(t *testing.T) {
	t.Parallel()
	records := []SampleRecord{
		{Entity: "Lead", Fields: map[string]string{"email": "a@x.test", "first_name": "A"}},
		{Entity: "Lead", Fields: map[string]string{"email": "b@x.test"}},
		{Entity: "Lead", Fields: map[string]string{"email": "c@x.test", "first_name": "C", "phone": "555"}},
	}
	schemas := inferSchemas(records)
	if len(schemas) != 1 {
		t.Fatalf("got %d schemas, want 1", len(schemas))
	}
	s := schemas[0]
	if s.Entity != "Lead" || s.SampleCount != 3 {
		t.Fatalf("schema = %+v, want entity Lead sampleCount 3", s)
	}
	byName := map[string]InferredField{}
	for _, f := range s.Fields {
		byName[f.Name] = f
	}
	if f := byName["email"]; f.Nullable || f.PresentCount != 3 {
		t.Errorf("email = %+v, want required (present 3, not nullable)", f)
	}
	if f := byName["first_name"]; !f.Nullable || f.PresentCount != 2 {
		t.Errorf("first_name = %+v, want nullable (present 2)", f)
	}
	if f := byName["phone"]; !f.Nullable || f.PresentCount != 1 {
		t.Errorf("phone = %+v, want nullable (present 1)", f)
	}
	// Fields must be sorted for determinism.
	if !sortedByName(s.Fields) {
		t.Errorf("inferred fields not sorted: %+v", s.Fields)
	}
}

// TestInferSchemasEmptyValueIsAbsent proves an empty-string value counts as
// absent for nullability (the field shape exists but was not populated).
func TestInferSchemasEmptyValueIsAbsent(t *testing.T) {
	t.Parallel()
	records := []SampleRecord{
		{Entity: "X", Fields: map[string]string{"a": "1"}},
		{Entity: "X", Fields: map[string]string{"a": ""}},
	}
	schemas := inferSchemas(records)
	if len(schemas) != 1 || len(schemas[0].Fields) != 1 {
		t.Fatalf("unexpected schemas %+v", schemas)
	}
	f := schemas[0].Fields[0]
	if f.Name != "a" || f.PresentCount != 1 || !f.Nullable {
		t.Errorf("field = %+v, want a present 1 nullable", f)
	}
}

// TestIsIDSegment spot-checks the id-segment classifier the templating relies on.
func TestIsIDSegment(t *testing.T) {
	t.Parallel()
	ids := []string{"123", "3f2504e0-4f89-41d3-9a0c-0305e82c3301", "con_501abc9988776655"}
	for _, s := range ids {
		if !isIDSegment(s) {
			t.Errorf("isIDSegment(%q) = false, want true", s)
		}
	}
	words := []string{"accounts", "search", "merge", "members", "trend", "", "v1"}
	for _, s := range words {
		if isIDSegment(s) {
			t.Errorf("isIDSegment(%q) = true, want false", s)
		}
	}
}

// --- small helpers ---

func schemaEntityNames(ss []InferredSchema) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Entity
	}
	return out
}

func endpointsBlob(eps []InferredEndpoint) string {
	var b strings.Builder
	for _, ep := range eps {
		b.WriteString(ep.Method + " " + ep.Host + ep.Template + "\n")
	}
	return b.String()
}

func sortedByName(fs []InferredField) bool {
	for i := 1; i < len(fs); i++ {
		if fs[i-1].Name > fs[i].Name {
			return false
		}
	}
	return true
}

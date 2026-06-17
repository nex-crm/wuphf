package workflow

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestGenerateReferral(t *testing.T) {
	s := mustLoad(t)
	gen, err := Generate(s)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The generated Go must be valid, parseable source.
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", gen.GoSource, parser.AllErrors); err != nil {
		t.Fatalf("generated Go does not parse: %v\n%s", err, gen.GoSource)
	}
	for _, want := range []string{
		"package genwf", "func Advance(", "StateIdentified",
		"ActionSlackSend", "EventProcess", "transitionsJSON",
	} {
		if !strings.Contains(gen.GoSource, want) {
			t.Errorf("generated Go missing %q", want)
		}
	}

	// The inngest adapter must encode the same contract.
	for _, want := range []string{"inngest.createFunction", "brex-referral-outreach", "transitionsJSON"} {
		if !strings.Contains(gen.InngestSource, want) {
			t.Errorf("inngest adapter missing %q", want)
		}
	}

	// Parity: transitions re-parsed from BOTH artifacts equal the spec.
	goT, err := extractTransitionsJSON(gen.GoSource)
	if err != nil {
		t.Fatalf("extract go transitions: %v", err)
	}
	if !transitionsEqual(goT, s.Transitions) {
		t.Fatal("generated Go transitions drifted from spec")
	}
	tsT, err := extractTransitionsJSON(gen.InngestSource)
	if err != nil {
		t.Fatalf("extract ts transitions: %v", err)
	}
	if !transitionsEqual(tsT, s.Transitions) {
		t.Fatal("generated inngest transitions drifted from spec")
	}
}

func TestShipcheckIncludesAdapterParity(t *testing.T) {
	s := mustLoad(t)
	rep := Shipcheck(s)
	var ap *Check
	for i := range rep.Checks {
		if rep.Checks[i].Name == "adapter_parity" {
			ap = &rep.Checks[i]
		}
	}
	if ap == nil || !ap.Pass {
		t.Fatalf("adapter_parity check should be present and pass: %+v", rep.Checks)
	}
}

func TestParityDetectsDrift(t *testing.T) {
	drifted := "anything transitionsJSON = `[{\"from\":\"a\",\"to\":\"b\",\"on\":\"go\"}]` more"
	got, err := extractTransitionsJSON(drifted)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	spec := []Transition{{From: "a", To: "c", On: "go"}} // different To
	if transitionsEqual(got, spec) {
		t.Fatal("parity must detect drift (b != c)")
	}
}

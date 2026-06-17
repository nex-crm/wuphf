package workflowpress

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// discovery.go is Phase 2 of the press: it distils already-captured, structured
// raw evidence (RawEvidence) into a validated, redacted WorkflowResearch — the
// raw discovery artifact at the head of the pipeline.
//
// SCOPE: live capture is OUT of this package. Discover does NOT drive a browser,
// CDP, or any recorder; its input is evidence that has ALREADY been captured by
// the recorder WUPHF has (browser-harness/CDP) and handed to it as a structured
// RawEvidence value. Discover is pure: same evidence in, same research out, no
// I/O. This keeps the lossy, side-effecting capture step outside the kernel and
// lets the deterministic distillation be tested in isolation.
//
// Discover implements the three inference IDEAS from the spec (ported in spirit
// from cli-printing-press, implemented from the spec description — not its
// source):
//
//   - endpoint templating: collapse concrete paths like /accounts/123 into
//     /accounts/{id} so a thousand traces become one endpoint shape (see
//     endpoints.go);
//   - count-based-nullability schema inference: over the sample records for an
//     entity, a field seen in every record is required, a field seen in only
//     some is nullable (see endpoints.go);
//   - secret redaction over the WHOLE research object: tokens, api keys, bearer
//     headers and credential-like values are scrubbed from the entire research
//     graph before it is stored, not just from on-disk samples (see redact.go).
//
// The output WorkflowResearch is append-only evidence, persisted OUTSIDE the
// kernel; it is never the source of truth for generation (the frozen
// WorkflowSpec is). Discovery only distils; the freeze step (Phase 3) and the
// human review turn research into a contract.

// RawEvidence is the messy, already-captured discovery input Discover distils.
// It is hostile-by-assumption: it can carry live credentials (bearer headers,
// api keys embedded in URLs or bodies), inconsistent records, and duplicate
// traces. Discover redacts the secrets and templates the endpoints; it never
// trusts the evidence as-is.
type RawEvidence struct {
	// WorkflowID ties this evidence to the workflow it informs (a stable slug
	// like "trial-to-ae-routing"). Required.
	WorkflowID string `json:"workflow_id"`
	// SessionContext is free-form context from the discovery session.
	SessionContext string `json:"session_context,omitempty"`
	// OperatorNotes are the operator's own words about the workflow.
	OperatorNotes []string `json:"operator_notes,omitempty"`
	// SampleRecords are raw example domain records observed during discovery,
	// grouped (after distillation) by entity for schema inference.
	SampleRecords []SampleRecord `json:"sample_records,omitempty"`
	// ObservedExceptions are edge/failure cases seen in the wild.
	ObservedExceptions []ObservedException `json:"observed_exceptions,omitempty"`
	// OperatorEdits are corrections the operator made to a synthesised spec.
	OperatorEdits []OperatorEdit `json:"operator_edits,omitempty"`
	// HTTPTraces are captured request/response traces with CONCRETE URLs (e.g.
	// /accounts/123). Discover templates them into InferredEndpoints and folds a
	// redacted summary into the research ToolTraces.
	HTTPTraces []HTTPTrace `json:"http_traces,omitempty"`
}

// HTTPTrace is one captured request/response pair from the recorder. URL is the
// concrete request URL; Discover collapses its path into an endpoint template.
// Headers and Body may carry live credentials and are redaction targets.
type HTTPTrace struct {
	Tool   string `json:"tool,omitempty"`
	Method string `json:"method"`
	// URL is the concrete request URL, e.g.
	// "https://api.crm.test/accounts/123?token=sk_live_x".
	URL string `json:"url"`
	// Headers are request headers; Authorization / api-key headers are redacted.
	Headers map[string]string `json:"headers,omitempty"`
	// RequestBody and ResponseBody are opaque payloads, redacted before storage.
	RequestBody  string `json:"request_body,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	Status       int    `json:"status,omitempty"`
}

// Discover distils already-captured RawEvidence into a validated, redacted
// WorkflowResearch. It is deterministic and performs no I/O: the same evidence
// always yields the same research. The pipeline is:
//
//  1. carry through the operator-supplied context (notes, exceptions, edits);
//  2. template the HTTP traces into InferredEndpoints and fold a summary into
//     ToolTraces;
//  3. infer count-based-nullability schemas over the sample records;
//  4. redact the WHOLE research object (secrets in any field), then
//  5. validate the result against the WorkflowResearch JSON Schema.
//
// A redaction or validation failure is returned wrapped; a caller never gets a
// partially-redacted research object back.
func Discover(inputs RawEvidence) (WorkflowResearch, error) {
	if strings.TrimSpace(inputs.WorkflowID) == "" {
		return WorkflowResearch{}, fmt.Errorf("workflowpress: discover: %w: workflow_id", ErrEmptyField)
	}

	research := WorkflowResearch{
		SchemaVersion:      SchemaVersionWorkflowResearch,
		WorkflowID:         inputs.WorkflowID,
		SessionContext:     inputs.SessionContext,
		OperatorNotes:      cloneStrings(inputs.OperatorNotes),
		SampleRecords:      cloneSampleRecords(inputs.SampleRecords),
		ObservedExceptions: cloneExceptions(inputs.ObservedExceptions),
		OperatorEdits:      cloneEdits(inputs.OperatorEdits),
	}

	// (2) Endpoint templating: concrete paths -> /accounts/{id}, deduped.
	research.InferredEndpoints = templateEndpoints(inputs.HTTPTraces)
	research.ToolTraces = summariseTraces(inputs.HTTPTraces)

	// (3) Count-based-nullability schema inference over the sample records.
	research.InferredSchemas = inferSchemas(inputs.SampleRecords)

	// (4) Redact the WHOLE research object before it is stored. This must run on
	// the assembled graph, not just on the on-disk samples, so a secret that
	// reached an inferred endpoint, a tool trace, or an operator note is also
	// scrubbed.
	redactResearch(&research)

	// (5) Validate the redacted research against the published JSON Schema so a
	// distillation bug cannot emit a research object the rest of the kernel
	// cannot read.
	if err := ValidateResearchJSON(toGeneric(research)); err != nil {
		return WorkflowResearch{}, fmt.Errorf("workflowpress: discover: %w", err)
	}

	return research, nil
}

// --- endpoint templating ---

// idSegment matches a single path segment that looks like a record identifier:
// a numeric id, a uuid, or a long opaque token. Such segments are collapsed to
// {id} so /accounts/123 and /accounts/456 become one endpoint shape.
var (
	numericID = regexp.MustCompile(`^\d+$`)
	uuidID    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// prefixedID matches the common record-id shape of a short alpha prefix, a
	// separator, then a digit-bearing suffix: acct_1001, con_501, ae_88,
	// user-42. The separator is what distinguishes it from a version-like word
	// such as "v1" (no separator), so "v1" is NOT collapsed.
	prefixedID = regexp.MustCompile(`^[A-Za-z]+[_-][A-Za-z0-9]*\d[A-Za-z0-9]*$`)
	// opaqueID matches a long mixed-case/alphanumeric token (>= 16 chars with at
	// least one digit), the shape of an object id or a slug-with-suffix. Pure
	// words like "accounts" are left alone.
	opaqueID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,}$`)
	hasDigit = regexp.MustCompile(`\d`)
)

// templateEndpoints collapses every concrete trace URL into a deduplicated set
// of endpoint templates, preserving the first-seen method/host and counting how
// many traces collapsed into each. The result is sorted for determinism.
func templateEndpoints(traces []HTTPTrace) []InferredEndpoint {
	if len(traces) == 0 {
		return nil
	}
	type agg struct {
		ep    InferredEndpoint
		count int
	}
	byKey := make(map[string]*agg)
	order := make([]string, 0, len(traces))

	for _, tr := range traces {
		host, tmpl := templatePath(tr.URL)
		method := strings.ToUpper(strings.TrimSpace(tr.Method))
		if method == "" {
			method = "GET"
		}
		key := method + " " + host + tmpl
		a, ok := byKey[key]
		if !ok {
			a = &agg{ep: InferredEndpoint{
				Method:   method,
				Host:     host,
				Template: tmpl,
			}}
			byKey[key] = a
			order = append(order, key)
		}
		a.count++
	}

	out := make([]InferredEndpoint, 0, len(order))
	for _, key := range order {
		a := byKey[key]
		a.ep.SampleCount = a.count
		out = append(out, a.ep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		if out[i].Template != out[j].Template {
			return out[i].Template < out[j].Template
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// templatePath splits a concrete URL into its host and a templated path. Each
// id-like segment is replaced with {id}; query strings are dropped (they often
// carry secrets and are not part of the endpoint shape). A URL that does not
// parse is returned host-less with its raw value as the template so no evidence
// is silently lost.
func templatePath(raw string) (host, template string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Path == "" {
		return "", raw
	}
	segs := strings.Split(u.Path, "/")
	for i, s := range segs {
		if isIDSegment(s) {
			segs[i] = "{id}"
		}
	}
	return u.Host, strings.Join(segs, "/")
}

// isIDSegment reports whether a path segment looks like a record identifier and
// should be collapsed to {id}.
func isIDSegment(s string) bool {
	switch {
	case s == "":
		return false
	case numericID.MatchString(s):
		return true
	case uuidID.MatchString(s):
		return true
	case prefixedID.MatchString(s):
		return true
	case opaqueID.MatchString(s) && hasDigit.MatchString(s):
		return true
	default:
		return false
	}
}

// summariseTraces folds the raw HTTP traces into the research ToolTraces, one
// per trace, with the concrete URL preserved (redaction scrubs any secret in it
// afterwards — including a query-string token AND credentials embedded as URL
// userinfo, scheme://user:pass@host). This keeps the evidence faithful while the
// InferredEndpoints carry the templated shape.
func summariseTraces(traces []HTTPTrace) []ToolTrace {
	if len(traces) == 0 {
		return nil
	}
	out := make([]ToolTrace, 0, len(traces))
	for _, tr := range traces {
		tool := tr.Tool
		if tool == "" {
			tool = "http"
		}
		out = append(out, ToolTrace{
			Tool:    tool,
			Action:  strings.TrimSpace(tr.Method + " " + tr.URL),
			Request: traceRequest(tr),
			Result:  tr.ResponseBody,
		})
	}
	return out
}

// traceRequest renders a trace's request side (headers + body) into a single
// opaque string for the ToolTrace. Redaction runs over the whole research
// afterwards, so any secret here is scrubbed before storage.
func traceRequest(tr HTTPTrace) string {
	var b strings.Builder
	keys := make([]string, 0, len(tr.Headers))
	for k := range tr.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, tr.Headers[k])
	}
	if tr.RequestBody != "" {
		b.WriteString(tr.RequestBody)
	}
	return strings.TrimRight(b.String(), "\n")
}

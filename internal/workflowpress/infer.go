package workflowpress

import (
	"encoding/json"
	"sort"
)

// infer.go holds the derived-signal types Discover attaches to a
// WorkflowResearch and the count-based-nullability schema inference. These are
// the "essential signals" the discovery distils out of raw evidence: the
// templated endpoints and the inferred entity schemas a reviewer reads before
// freezing a spec. They live ON the research object (not a separate artifact) so
// the secret-redaction sweep covers them too.

// InferredEndpoint is a templated API endpoint distilled from concrete HTTP
// traces. Many traces against /accounts/123, /accounts/456 collapse into one
// endpoint with Template "/accounts/{id}" and SampleCount = how many collapsed.
// It is an inference, not a contract: the freeze step decides whether it becomes
// an Action.
type InferredEndpoint struct {
	// Method is the HTTP verb (upper-cased).
	Method string `json:"method"`
	// Host is the request host (no scheme); empty if the URL did not parse.
	Host string `json:"host,omitempty"`
	// Template is the templated path, e.g. "/accounts/{id}". Id-like segments
	// are collapsed to {id}; query strings are dropped.
	Template string `json:"template"`
	// SampleCount is how many concrete traces collapsed into this template — a
	// rough signal of how central the endpoint is to the workflow.
	SampleCount int `json:"sample_count"`
}

// InferredSchema is the count-based-nullability schema inferred for one entity
// from its sample records. SampleCount is how many records were seen; each field
// records how often it was present, so a field present in every record is
// required and one present in only some is nullable.
type InferredSchema struct {
	// Entity is the domain object the schema describes (matches
	// SampleRecord.Entity).
	Entity string `json:"entity"`
	// SampleCount is how many sample records of this entity were observed.
	SampleCount int `json:"sample_count"`
	// Fields is the inferred field set, sorted by name for determinism.
	Fields []InferredField `json:"fields"`
}

// InferredField is one field of an inferred entity schema. Nullable is true when
// the field was absent from at least one sample record (count-based nullability:
// present in every record => required; present in only some => nullable).
type InferredField struct {
	Name string `json:"name"`
	// PresentCount is how many sample records carried this field.
	PresentCount int `json:"present_count"`
	// Nullable is true when PresentCount < the entity's SampleCount.
	Nullable bool `json:"nullable"`
}

// inferSchemas runs count-based-nullability inference over the sample records,
// grouped by entity. For each entity it counts how many records carried each
// field; a field present in every record of that entity is required (Nullable
// false), a field present in only some is Nullable. Output is sorted by entity
// then field for determinism.
func inferSchemas(records []SampleRecord) []InferredSchema {
	if len(records) == 0 {
		return nil
	}

	// Per entity: total record count + per-field present count, in first-seen
	// order for the entity list, but fields are sorted at emit time.
	type entityAgg struct {
		total   int
		present map[string]int
	}
	byEntity := make(map[string]*entityAgg)
	order := make([]string, 0)

	for _, rec := range records {
		if rec.Entity == "" {
			// Records with no entity cannot be grouped; skip them rather than
			// inventing a bucket. They remain in SampleRecords as raw evidence.
			continue
		}
		a, ok := byEntity[rec.Entity]
		if !ok {
			a = &entityAgg{present: make(map[string]int)}
			byEntity[rec.Entity] = a
			order = append(order, rec.Entity)
		}
		a.total++
		for field, val := range rec.Fields {
			// Count a field as present when it carries a non-empty value. An empty
			// string is treated as absent for nullability — the field exists in the
			// shape but was not populated in this record.
			if val != "" {
				a.present[field]++
			}
		}
	}

	if len(order) == 0 {
		return nil
	}
	sort.Strings(order)

	out := make([]InferredSchema, 0, len(order))
	for _, entity := range order {
		a := byEntity[entity]
		fields := make([]InferredField, 0, len(a.present))
		for name, count := range a.present {
			fields = append(fields, InferredField{
				Name:         name,
				PresentCount: count,
				Nullable:     count < a.total,
			})
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		out = append(out, InferredSchema{
			Entity:      entity,
			SampleCount: a.total,
			Fields:      fields,
		})
	}
	return out
}

// --- defensive clone helpers ---
//
// Discover never mutates its RawEvidence input; it deep-copies the slices it
// carries through so a later redaction pass scrubs the research object without
// reaching back into the caller's evidence. Immutability of the input is a hard
// rule: the caller's RawEvidence must read the same before and after Discover.

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneSampleRecords(in []SampleRecord) []SampleRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]SampleRecord, len(in))
	for i, r := range in {
		out[i] = SampleRecord{Entity: r.Entity, Source: r.Source}
		if len(r.Fields) > 0 {
			out[i].Fields = make(map[string]string, len(r.Fields))
			for k, v := range r.Fields {
				out[i].Fields[k] = v
			}
		}
	}
	return out
}

func cloneExceptions(in []ObservedException) []ObservedException {
	if len(in) == 0 {
		return nil
	}
	out := make([]ObservedException, len(in))
	copy(out, in)
	return out
}

func cloneEdits(in []OperatorEdit) []OperatorEdit {
	if len(in) == 0 {
		return nil
	}
	out := make([]OperatorEdit, len(in))
	copy(out, in)
	return out
}

// toGeneric marshals a research value to JSON and back to a generic any so it
// can be validated against the JSON Schema, which works on decoded generic
// values. A marshalling failure here would be a programming error (the research
// is composed of plain JSON-able fields), so the helper falls back to the typed
// value, letting the schema validator surface a precise error instead.
func toGeneric(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return v
	}
	return decoded
}

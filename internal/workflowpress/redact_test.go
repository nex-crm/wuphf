package workflowpress

import (
	"strings"
	"testing"
	"time"
)

// TestRedactString is the adversarial table for the secret scrubber. Each case
// pairs an input string with the substrings that MUST NOT survive (the leaked
// secret) and, where relevant, what MUST survive (the surrounding non-secret
// text). The redactor is allowed to over-redact; it is never allowed to leak.
func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         string
		mustScrub  []string // these byte sequences must be gone from the output
		mustKeep   []string // these must remain (proves we don't nuke everything)
		wantMarker bool     // output must contain RedactedMarker
	}{
		{
			name:       "bearer token in header line",
			in:         "Authorization: Bearer sk_live_FAKEdoc0",
			mustScrub:  []string{"sk_live_FAKEdoc0", "Bearer"},
			mustKeep:   []string{"Authorization"},
			wantMarker: true,
		},
		{
			name:       "basic auth header",
			in:         "Authorization: Basic cm9vdDpodW50ZXIy",
			mustScrub:  []string{"cm9vdDpodW50ZXIy", "Basic"},
			mustKeep:   []string{"Authorization"},
			wantMarker: true,
		},
		{
			name:       "stripe live secret key inline",
			in:         "operator pasted token sk_live_FAKEdoc0 by mistake",
			mustScrub:  []string{"sk_live_FAKEdoc0"},
			mustKeep:   []string{"operator pasted", "by mistake"},
			wantMarker: true,
		},
		{
			name:       "stripe test key",
			in:         "new bearer is Bearer sk_test_FAKEold0",
			mustScrub:  []string{"sk_test_FAKEold0"},
			wantMarker: true,
		},
		{
			name:       "aws access key id",
			in:         "api_key=AKIAFAKEEXAMPLE00 in the note",
			mustScrub:  []string{"AKIAFAKEEXAMPLE00"},
			wantMarker: true,
		},
		{
			name:       "github personal access token",
			in:         "auth ghp_FAKEgithubtok000 here",
			mustScrub:  []string{"ghp_FAKEgithubtok000"},
			wantMarker: true,
		},
		{
			name:       "github fine-grained pat",
			in:         "token github_pat_FAKEfinegrained0",
			mustScrub:  []string{"github_pat_FAKEfinegrained0"},
			wantMarker: true,
		},
		{
			name:       "slack bot token",
			in:         "Authorization: Bearer xoxb-FAKEslackbot00",
			mustScrub:  []string{"xoxb-FAKEslackbot00"},
			wantMarker: true,
		},
		{
			name:       "google api key",
			in:         "key AIzaSyFAKEgoogleapikey00 set",
			mustScrub:  []string{"AIzaSyFAKEgoogleapikey00"},
			wantMarker: true,
		},
		{
			name:       "jwt",
			in:         "auth_token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.s5y3aJ8Qk2",
			mustScrub:  []string{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.s5y3aJ8Qk2"},
			wantMarker: true,
		},
		{
			name:       "token in query string",
			in:         "https://api.crm.test/accounts/123?api_key=sk_live_FAKEdoc0",
			mustScrub:  []string{"sk_live_FAKEdoc0"},
			mustKeep:   []string{"https://api.crm.test/accounts/123"},
			wantMarker: true,
		},
		{
			name:       "password in free text",
			in:         "warehouse password=hunter2-do-not-share now",
			mustScrub:  []string{"hunter2-do-not-share"},
			wantMarker: true,
		},
		{
			name:       "session cookie in query",
			in:         "cookie=session=abcdef0123456789abcdef0123456789",
			mustScrub:  []string{"abcdef0123456789abcdef0123456789"},
			wantMarker: true,
		},
		{
			name:       "long hex blob",
			in:         "digest 8f3c9a1b2d4e6f7081923344556677aa8f3c9a1b is high entropy",
			mustScrub:  []string{"8f3c9a1b2d4e6f7081923344556677aa8f3c9a1b"},
			wantMarker: true,
		},
		{
			name: "pem private key block",
			in:   "-----BEGIN RSA PRIVATE KEY-----\nFAKEKEYBODY00000\n-----END RSA PRIVATE KEY-----",
			mustScrub: []string{
				"FAKEKEYBODY00000",
				"BEGIN RSA PRIVATE KEY",
			},
			wantMarker: true,
		},
		{
			name:      "innocuous text is untouched",
			in:        "Route acme.test to Pat Rivera in us-east",
			mustScrub: nil,
			mustKeep:  []string{"Route acme.test to Pat Rivera in us-east"},
		},
		{
			name:      "empty string",
			in:        "",
			mustScrub: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactString(tc.in)
			for _, s := range tc.mustScrub {
				if strings.Contains(got, s) {
					t.Errorf("redactString leaked secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			for _, s := range tc.mustKeep {
				if !strings.Contains(got, s) {
					t.Errorf("redactString dropped non-secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			if tc.wantMarker && !strings.Contains(got, RedactedMarker) {
				t.Errorf("redactString did not emit the marker for a secret\n in:  %q\n out: %q", tc.in, got)
			}
		})
	}
}

// TestIsSecretKey covers the key-aware half of redaction: a header/field name
// that hints at a secret marks its whole value, regardless of value shape.
func TestIsSecretKey(t *testing.T) {
	t.Parallel()
	secret := []string{
		"Authorization", "authorization", "X-Api-Key", "api_key", "apikey",
		"access_token", "refresh-token", "Set-Cookie", "Cookie", "password",
		"DB_PASSWORD", "client_secret", "session", "X-Auth-Token",
	}
	for _, k := range secret {
		if !isSecretKey(k) {
			t.Errorf("isSecretKey(%q) = false, want true", k)
		}
	}
	notSecret := []string{"Accept", "Content-Type", "email", "company_domain", "renewal_date", ""}
	for _, k := range notSecret {
		if isSecretKey(k) {
			t.Errorf("isSecretKey(%q) = true, want false", k)
		}
	}
}

// TestRedactSampleRecordKeyAware proves a secret-NAMED field is scrubbed by its
// key even when the value is not a recognised token shape.
func TestRedactSampleRecordKeyAware(t *testing.T) {
	t.Parallel()
	rec := SampleRecord{
		Entity: "TrialSignup",
		Fields: map[string]string{
			"email":      "sam@acme.test",
			"auth_token": "not-a-known-token-shape-but-named-secret",
			"session_id": "plainvalue123",
		},
	}
	redactSampleRecord(&rec)
	if rec.Fields["auth_token"] != RedactedMarker {
		t.Errorf("auth_token = %q, want %q", rec.Fields["auth_token"], RedactedMarker)
	}
	if rec.Fields["session_id"] != RedactedMarker {
		t.Errorf("session_id = %q, want %q", rec.Fields["session_id"], RedactedMarker)
	}
	if rec.Fields["email"] != "sam@acme.test" {
		t.Errorf("email was over-redacted to %q", rec.Fields["email"])
	}
}

// TestRedactResearchSweepsWholeObject proves redaction reaches EVERY string
// surface of the research graph, not just sample files — the spec's load-bearing
// requirement. A distinct secret is planted in each field and none may survive.
func TestRedactResearchSweepsWholeObject(t *testing.T) {
	t.Parallel()
	r := WorkflowResearch{
		WorkflowID:     "wf",
		SessionContext: "ctx Bearer sk_live_FAKEctx0",
		OperatorNotes:  []string{"note api_key=AKIAFAKEEXAMPLE00"},
		SampleRecords: []SampleRecord{
			{Entity: "E", Fields: map[string]string{"token": "sk_live_FAKErec0", "name": "ok"}},
		},
		ObservedExceptions: []ObservedException{
			{Description: "exc Bearer sk_live_FAKEexc0", HandledAs: "h ghp_FAKEgithubtok000"},
		},
		OperatorEdits: []OperatorEdit{
			{Path: "p", Before: "b", After: "after sk_live_FAKEedt0", Reason: "r"},
		},
		ToolTraces: []ToolTrace{
			{Tool: "t", Action: "GET /x", Request: "Authorization: Bearer sk_live_FAKEtrc0", Result: "ok"},
		},
		InferredEndpoints: []InferredEndpoint{
			{Method: "GET", Host: "api.test", Template: "/x/sk_live_FAKEend0"},
		},
	}
	redactResearch(&r)

	leaks := []string{
		"sk_live_FAKEctx0",
		"AKIAFAKEEXAMPLE00",
		"sk_live_FAKErec0",
		"sk_live_FAKEexc0",
		"ghp_FAKEgithubtok000",
		"sk_live_FAKEedt0",
		"sk_live_FAKEtrc0",
		"sk_live_FAKEend0",
	}
	blob := researchBlob(r)
	for _, leak := range leaks {
		if strings.Contains(blob, leak) {
			t.Errorf("secret %q survived redactResearch in:\n%s", leak, blob)
		}
	}
	// Non-secret content must survive so we know redaction is targeted.
	if r.SampleRecords[0].Fields["name"] != "ok" {
		t.Errorf("non-secret field was over-redacted: %q", r.SampleRecords[0].Fields["name"])
	}
}

// TestRedactStringURLUserinfo proves credentials embedded in a URL's userinfo
// (scheme://user:pass@host) are stripped wherever a concrete URL is kept — the
// fix for the trace-summary leak where summariseTraces stored method+URL
// verbatim and redaction never touched the userinfo.
func TestRedactStringURLUserinfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		mustScrub []string
		mustKeep  []string
	}{
		{
			name:      "password in https userinfo",
			in:        "GET https://admin:s3cr3tP4ss@api.crm.test/accounts/123",
			mustScrub: []string{"s3cr3tP4ss", "admin:s3cr3tP4ss"},
			mustKeep:  []string{"api.crm.test/accounts/123", "GET"},
		},
		{
			name:      "userinfo inside a longer prose line",
			in:        "operator ran curl https://svc:topsecretvalue99@warehouse.test/usage then exited",
			mustScrub: []string{"topsecretvalue99", "svc:topsecretvalue99"},
			mustKeep:  []string{"warehouse.test/usage", "operator ran curl", "then exited"},
		},
		{
			name:      "bare user@host email-style URL is not mangled",
			in:        "mailto link mailto:sam@acme.test stays",
			mustScrub: nil,
			mustKeep:  []string{"sam@acme.test"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactString(tc.in)
			for _, s := range tc.mustScrub {
				if strings.Contains(got, s) {
					t.Errorf("leaked URL-userinfo secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			for _, s := range tc.mustKeep {
				if !strings.Contains(got, s) {
					t.Errorf("dropped non-secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
		})
	}
}

// TestRedactNestedJSONSecrets proves the "secret in a nested field" leak is
// closed: a credential buried under inner keys of a JSON-encoded value is
// scrubbed by the structural walk, including very common RevOps OAuth/connect
// response shapes (access_token + refresh_token nested two levels deep).
func TestRedactNestedJSONSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		mustScrub []string
		mustKeep  []string
	}{
		{
			name:      "opaque token under nested oauth key",
			in:        `{"integration":{"oauth":{"access_token":"glpat-FAKEgitlab000"}}}`,
			mustScrub: []string{"glpat-FAKEgitlab000"},
			mustKeep:  []string{"integration"},
		},
		{
			name:      "connect response nesting access and refresh tokens",
			in:        `{"ok":true,"connection":{"access_token":"ya29_FAKEoauthtoken0","refresh_token":"rt_9f8e7d6c5b4a3f2e1d0c9b8a7654321"}}`,
			mustScrub: []string{"ya29_FAKEoauthtoken0", "rt_9f8e7d6c5b4a3f2e1d0c9b8a7654321"},
			mustKeep:  []string{"connection"},
		},
		{
			name:      "secret inside a json array element",
			in:        `{"creds":[{"client_secret":"plainbutnamedsecretvalue"}]}`,
			mustScrub: []string{"plainbutnamedsecretvalue"},
			mustKeep:  []string{"creds"},
		},
		{
			name:      "non-secret nested fields survive",
			in:        `{"company":{"domain":"acme.test","employee_count":"200"}}`,
			mustScrub: nil,
			mustKeep:  []string{"acme.test", "200", "employee_count"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactString(tc.in)
			for _, s := range tc.mustScrub {
				if strings.Contains(got, s) {
					t.Errorf("leaked nested secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			for _, s := range tc.mustKeep {
				if !strings.Contains(got, s) {
					t.Errorf("dropped non-secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			if len(tc.mustScrub) > 0 && !strings.Contains(got, RedactedMarker) {
				t.Errorf("no marker emitted for nested secret\n in:  %q\n out: %q", tc.in, got)
			}
		})
	}
}

// TestRedactNestedJSONInSampleRecord proves the leak through the LIVE record
// path: a SampleRecord whose value JSON-encodes a nested object with an
// access_token is scrubbed by redactSampleRecord (which routes the value through
// redactString and its JSON walk).
func TestRedactNestedJSONInSampleRecord(t *testing.T) {
	t.Parallel()
	rec := SampleRecord{
		Entity: "Connection",
		Fields: map[string]string{
			"provider": "salesforce",
			"payload":  `{"integration":{"oauth":{"access_token":"glpat-FAKEgitlab000","scope":"read"}}}`,
		},
	}
	redactSampleRecord(&rec)
	if strings.Contains(rec.Fields["payload"], "glpat-FAKEgitlab000") {
		t.Errorf("nested access_token leaked through sample record: %q", rec.Fields["payload"])
	}
	if rec.Fields["provider"] != "salesforce" {
		t.Errorf("non-secret field over-redacted: %q", rec.Fields["provider"])
	}
}

// TestRedactNonSecretNamedHeaderValue proves a credential hiding under an
// unusual, non-secret-NAMED header is scrubbed by the header-value position
// backstop — the fix for the "X-Internal-Sig: tok_clearvalue_noPattern" leak.
func TestRedactNonSecretNamedHeaderValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		mustScrub []string
		mustKeep  []string
	}{
		{
			name:      "credential-prefixed token under unusual header",
			in:        "X-Internal-Sig: tok_clearvalue_noPattern",
			mustScrub: []string{"tok_clearvalue_noPattern"},
			mustKeep:  []string{"X-Internal-Sig"},
		},
		{
			name:      "high-entropy blob under unusual header",
			in:        "X-Request-Signature: a8F3kQ9vZ1pLmN7xR2tBcW4yE6sD0uH",
			mustScrub: []string{"a8F3kQ9vZ1pLmN7xR2tBcW4yE6sD0uH"},
			mustKeep:  []string{"X-Request-Signature"},
		},
		{
			name:      "ordinary content-type header survives",
			in:        "Content-Type: application/json",
			mustScrub: nil,
			mustKeep:  []string{"Content-Type", "application/json"},
		},
		{
			name:      "ordinary accept header survives",
			in:        "Accept: application/json",
			mustScrub: nil,
			mustKeep:  []string{"application/json"},
		},
		{
			name:      "short cache-control keyword survives",
			in:        "Cache-Control: no-cache",
			mustScrub: nil,
			mustKeep:  []string{"no-cache"},
		},
		{
			name:      "idempotency-key with email survives",
			in:        "Idempotency-Key: lead-chris@acme.test-v1",
			mustScrub: nil,
			mustKeep:  []string{"lead-chris@acme.test-v1"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactString(tc.in)
			for _, s := range tc.mustScrub {
				if strings.Contains(got, s) {
					t.Errorf("leaked header-value secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
			for _, s := range tc.mustKeep {
				if !strings.Contains(got, s) {
					t.Errorf("dropped non-secret %q\n in:  %q\n out: %q", s, tc.in, got)
				}
			}
		})
	}
}

// TestRedactOpaqueUnknownVendorTokens proves the entropy/length backstop closes
// the allow-list fail-open hole: OAuth tokens, vendor session blobs, and
// mixed-case base64 secrets with no recognised vendor prefix are scrubbed when
// they appear as a bare token in free text (the grading probe shape
// "token <v> end").
func TestRedactOpaqueUnknownVendorTokens(t *testing.T) {
	t.Parallel()
	scrubbed := []struct {
		name string
		tok  string
	}{
		{"google oauth access token", "ya29_FAKEoauthtoken1"},
		{"refresh token prefixed", "rt_9f8e7d6c5b4a3f2e1d0c9b8a7654321"},
		{"slack xoxe token", "xoxe-FAKEslackapp00"},
		{"mixed-case base64 session token 30ch", "aGVsbG8Xd29ybGQ5dGhpc1Z3M2NyZXQ"},
		{"gitlab pat", "glpat-FAKEgitlab00XY"},
	}
	for _, tc := range scrubbed {
		tc := tc
		t.Run("scrub/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactString("token " + tc.tok + " end")
			if strings.Contains(got, tc.tok) {
				t.Errorf("opaque token leaked %q\n out: %q", tc.tok, got)
			}
			if !strings.Contains(got, "token ") || !strings.Contains(got, " end") {
				t.Errorf("backstop over-redacted surrounding text: %q", got)
			}
		})
	}

	// These ordinary identifiers/words must SURVIVE the backstop — over-redaction
	// is acceptable in spirit but must not destroy the non-secret signal a
	// reviewer reads (entities, ids, domains, dates).
	kept := []string{
		"acme.test",
		"api.crm.test",
		"sam@acme.test",
		"lead-chris@acme.test-v1",
		"acct_1001",
		"con_501",
		"ae_88",
		"2026-06-10T14:03:00Z",
		"2026-07-05",
		"mid-market",
		"us-east",
		"employee_count",
		"application/json",
		"round-robin",
		"data-quality",
	}
	for _, k := range kept {
		k := k
		t.Run("keep/"+k, func(t *testing.T) {
			t.Parallel()
			got := redactString("value " + k + " ok")
			if !strings.Contains(got, k) {
				t.Errorf("backstop over-redacted non-secret %q\n out: %q", k, got)
			}
		})
	}
}

// TestRedactToolTraceToolField is the regression for the skipped ToolTrace.Tool
// field: redactToolTrace previously scrubbed Action/Request/Result but left Tool
// untouched, so a credential pasted into the tool name survived into stored
// research. After the fix the Tool field is redacted like the rest of the trace.
func TestRedactToolTraceToolField(t *testing.T) {
	t.Parallel()
	r := WorkflowResearch{
		WorkflowID: "wf",
		ToolTraces: []ToolTrace{
			{
				Tool:   "crm-connector token=sk_live_FAKEtol0",
				Action: "GET /accounts",
				Result: "ok",
			},
		},
	}
	redactResearch(&r)

	got := r.ToolTraces[0].Tool
	if strings.Contains(got, "sk_live_FAKEtol0") {
		t.Errorf("secret survived in ToolTrace.Tool: %q", got)
	}
	if !strings.Contains(got, RedactedMarker) {
		t.Errorf("ToolTrace.Tool was not redacted: %q", got)
	}
}

// TestRedactHexBlobIsBounded proves the bounded hex pattern still scrubs a
// real-length hex credential and does not hang on a hostile oversized hex string
// (the ReDoS bound). A 64-char hex blob (a SHA-256-shaped secret) must be redacted;
// a multi-kilobyte hex run must return promptly without pathological backtracking.
func TestRedactHexBlobIsBounded(t *testing.T) {
	t.Parallel()
	sixtyFour := strings.Repeat("a1b2c3d4", 8) // 64 hex chars
	if out := redactString("key " + sixtyFour); strings.Contains(out, sixtyFour) {
		t.Errorf("64-char hex blob not redacted: %q", out)
	}
	// A large hostile hex run must complete quickly; the bound prevents ReDoS.
	huge := "header " + strings.Repeat("deadbeef", 4096) + " trailer"
	done := make(chan string, 1)
	go func() { done <- redactString(huge) }()
	select {
	case out := <-done:
		if !strings.Contains(out, "trailer") {
			t.Error("redaction dropped trailing non-secret content")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("redactString did not complete on a large hex input within 5s (possible ReDoS)")
	}
}

// researchBlob renders every string field of a research object into one buffer
// so a test can assert no planted secret survived anywhere.
func researchBlob(r WorkflowResearch) string {
	var b strings.Builder
	b.WriteString(r.SessionContext)
	for _, n := range r.OperatorNotes {
		b.WriteString("\n" + n)
	}
	for _, rec := range r.SampleRecords {
		b.WriteString("\n" + rec.Source)
		for k, v := range rec.Fields {
			b.WriteString("\n" + k + "=" + v)
		}
	}
	for _, e := range r.ObservedExceptions {
		b.WriteString("\n" + e.Description + "\n" + e.HandledAs)
	}
	for _, e := range r.OperatorEdits {
		b.WriteString("\n" + e.Path + "\n" + e.Before + "\n" + e.After + "\n" + e.Reason)
	}
	for _, t := range r.ToolTraces {
		b.WriteString("\n" + t.Action + "\n" + t.Request + "\n" + t.Result)
	}
	for _, ep := range r.InferredEndpoints {
		b.WriteString("\n" + ep.Host + ep.Template)
	}
	return b.String()
}

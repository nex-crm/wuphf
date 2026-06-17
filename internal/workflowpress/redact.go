package workflowpress

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
)

// redact.go scrubs secrets from the WHOLE research object before it is stored.
//
// ============================ SECURITY ============================
//
// Discovery captures LIVE credentials: bearer headers, api keys embedded in
// URLs or request bodies, basic-auth strings, private keys, and OAuth tokens
// nested deep inside captured connect-response JSON. The spec is explicit that
// redaction runs over the research GRAPH, not just over on-disk sample files —
// a secret that reached a tool trace, an inferred endpoint, an operator note, a
// URL's userinfo, or a value buried inside a JSON-encoded sample-record value
// must be scrubbed wherever it landed (workflow-press.md "Security model").
//
// The redactor is fail-CLOSED in spirit: it errs toward over-redacting. Four
// complementary strategies run together so a secret missed by one is caught by
// another:
//
//   - KEY-AWARE redaction: when a key/header name signals a secret
//     (authorization, api-key, token, password, secret, client_secret, cookie,
//     set-cookie, x-api-key, ...), its whole value is replaced, regardless of
//     the value's shape. This is applied at EVERY level of a nested structure,
//     not only the top, so a secret-named key inside a JSON blob is caught too.
//   - PATTERN redaction: known credential value shapes (Bearer <jwt>, sk_live_*,
//     AKIA* AWS keys, ghp_*/github tokens, glpat-* GitLab tokens, xox*-Slack
//     tokens, ya29.* Google OAuth tokens, Basic <b64>, long high-entropy
//     hex/base64 blobs, PEM private-key blocks) are replaced anywhere they
//     appear in free text — urls, bodies, notes, trace results.
//   - URL-USERINFO redaction: credentials embedded in a URL as
//     scheme://user:pass@host are stripped wherever a URL appears, including the
//     concrete URL kept verbatim in a tool-trace Action summary.
//   - ENTROPY/POSITION backstop: the pattern list is an allow-list of known
//     vendor prefixes and therefore fails OPEN for opaque/unknown-vendor tokens.
//     A length+entropy heuristic and a header-value position rule back it up so
//     a high-entropy blob or a lone credential-shaped header value with no known
//     prefix is still over-redacted rather than leaked.
//
// A redacted value becomes the constant RedactedMarker so a reviewer can see a
// secret WAS present without seeing its bytes. The function mutates the research
// in place; Discover hands it a deep copy of the caller's evidence, so the
// caller's RawEvidence is never touched.
//
// =================================================================

// RedactedMarker replaces a value identified as a secret. It is intentionally
// visible so a reviewer sees that a credential was present and scrubbed.
const RedactedMarker = "[REDACTED]"

// secretKeyHints are substrings that, when found in a field/header/query key
// name (case-insensitive), mark the whole associated value as a secret. Match is
// substring-based so "x-api-key", "client_secret" and "DB_PASSWORD" all hit.
var secretKeyHints = []string{
	"authorization",
	"api-key",
	"apikey",
	"api_key",
	"access-token",
	"access_token",
	"refresh-token",
	"refresh_token",
	"token",
	"secret",
	"password",
	"passwd",
	"cookie",
	"credential",
	"private-key",
	"private_key",
	"session",
	"bearer",
	"auth",
}

// secretValuePatterns match credential-shaped values anywhere in free text. They
// are deliberately broad; over-redaction is safer than leaking a key. Each is
// applied to every string field of the research object.
var secretValuePatterns = []*regexp.Regexp{
	// Bearer / token auth headers and inline mentions: "Bearer <token>".
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-+/=]{8,}`),
	// Basic auth: "Basic <base64>".
	regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]{8,}`),
	// Stripe-style live/test secret keys: sk_live_..., rk_test_..., pk_live_...
	regexp.MustCompile(`\b[a-z]{2}_(?:live|test)_[A-Za-z0-9]{8,}`),
	// AWS access key id.
	regexp.MustCompile(`\bAKIA[0-9A-Z]{12,}`),
	// AWS secret access key shape (40-char base64-ish) when prefixed with a hint
	// is caught by key-aware redaction; standalone we catch the common token
	// prefixes below and the entropy backstop.
	// GitHub tokens: ghp_, gho_, ghu_, ghs_, ghr_, github_pat_.
	regexp.MustCompile(`\bgh[opusr]_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{16,}`),
	// GitLab personal/project/group access tokens: glpat-...
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{8,}`),
	// Slack tokens: xoxb-, xoxp-, xoxa-, xoxr-, xoxs-, xoxe- (and other xox*-).
	regexp.MustCompile(`\bxox[a-z]-[A-Za-z0-9-]{8,}`),
	// Google API keys: AIza...
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}`),
	// Google OAuth access/refresh tokens: ya29.* / 1//*.
	regexp.MustCompile(`\bya29[._][A-Za-z0-9._\-]{10,}`),
	regexp.MustCompile(`\b1//[A-Za-z0-9._\-]{20,}`),
	// JWTs: three base64url segments separated by dots.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
	// PEM private-key blocks (single line or with literal escaped newlines).
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// Generic long high-entropy hex blob (32..128 hex chars) — api keys, hashes.
	// The upper bound is deliberate: an unbounded {32,} quantifier lets a huge
	// hostile tool-trace string drive pathological backtracking (ReDoS). Real
	// credentials/hashes sit well under 128 hex chars; anything longer is caught by
	// the entropy backstop instead.
	regexp.MustCompile(`\b[0-9a-fA-F]{32,128}\b`),
}

// assignedSecret matches a key=value or key:value assignment in free text where
// the key hints at a secret, capturing the key+delimiter so the value is
// replaced inline: "?token=sk_live_x" -> "?token=[REDACTED]" and
// "password=hunter2" -> "password=[REDACTED]". This catches secrets embedded in
// query strings AND in prose ("warehouse password=hunter2 now"), where a bare
// value would not match a token regex. The value is read up to the next
// whitespace, ampersand, quote, comma, or semicolon.
var assignedSecret = regexp.MustCompile(`(?i)([?&]?\b[a-z0-9_\-]*(?:token|api[_-]?key|secret|password|passwd|credential|session|cookie|bearer)[a-z0-9_\-]*\s*[=:]\s*)[^&\s"',;]+`)

// urlUserinfo matches the userinfo segment of a URL — scheme://user:pass@host —
// and captures the scheme prefix and the host onward so the credential between
// them is dropped. This catches "https://admin:s3cr3t@api.crm.test/x" wherever a
// concrete URL is kept verbatim (notably a tool-trace Action summary, which
// stores the raw method+URL). Only credentials carrying a ":" (a password) are
// scrubbed; a bare "user@host" is left alone so plain emails are not mangled.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/?#@\s]*:[^/?#@\s]*@`)

// redactResearch scrubs secrets from every string-bearing field of the research
// object, in place. It is the single entry point Discover calls after the
// research graph is assembled and before validation/return.
func redactResearch(r *WorkflowResearch) {
	r.SessionContext = redactString(r.SessionContext)
	for i := range r.OperatorNotes {
		r.OperatorNotes[i] = redactString(r.OperatorNotes[i])
	}
	for i := range r.SampleRecords {
		redactSampleRecord(&r.SampleRecords[i])
	}
	for i := range r.ObservedExceptions {
		r.ObservedExceptions[i].Description = redactString(r.ObservedExceptions[i].Description)
		r.ObservedExceptions[i].HandledAs = redactString(r.ObservedExceptions[i].HandledAs)
	}
	for i := range r.OperatorEdits {
		redactEdit(&r.OperatorEdits[i])
	}
	for i := range r.ToolTraces {
		redactToolTrace(&r.ToolTraces[i])
	}
	for i := range r.InferredEndpoints {
		// Templates are scrubbed too: a secret can hide in a path segment that did
		// not match the {id} rule, or in a host. Method/SampleCount are safe.
		r.InferredEndpoints[i].Host = redactString(r.InferredEndpoints[i].Host)
		r.InferredEndpoints[i].Template = redactString(r.InferredEndpoints[i].Template)
	}
	// InferredSchemas carry only field names + counts (no values), so there is
	// nothing secret to scrub there.
}

func redactSampleRecord(rec *SampleRecord) {
	rec.Source = redactString(rec.Source)
	for k, v := range rec.Fields {
		if isSecretKey(k) {
			rec.Fields[k] = RedactedMarker
			continue
		}
		// The value may itself be a JSON-encoded nested object whose inner keys
		// name secrets (a faithful capture of a connect-response record stores the
		// nested object as a string). redactString unwraps and walks it.
		rec.Fields[k] = redactString(v)
	}
}

func redactEdit(e *OperatorEdit) {
	// The path may name a field (e.g. "actions[x].target"); redact patterns from
	// it defensively but key-awareness does not apply to a path. Before/After/
	// Reason are free text and may carry a pasted credential.
	e.Path = redactString(e.Path)
	e.Before = redactString(e.Before)
	e.After = redactString(e.After)
	e.Reason = redactString(e.Reason)
}

func redactToolTrace(t *ToolTrace) {
	// Tool is a free-text field too (a tool/integration name an operator may have
	// typed a credential into); scrub it like the others. Skipping it left a leak
	// surface that the whole-graph redaction promise forbids.
	t.Tool = redactString(t.Tool)
	t.Action = redactString(t.Action)
	t.Request = redactString(t.Request)
	t.Result = redactString(t.Result)
}

// redactString applies URL-userinfo, JSON-structure, query-param, key-line,
// value-pattern and entropy-backstop redaction to a single free-text string.
// Order matters:
//
//  1. URL userinfo (scheme://user:pass@host) is stripped first so a credential
//     embedded in a verbatim URL is removed before anything else looks at it.
//  2. If the string IS (or contains) a JSON object/array, it is walked
//     structurally so nested secret-named keys and nested credential values are
//     scrubbed with full key-awareness — redactString alone has no structure
//     awareness and would miss a token buried under an inner key.
//  3. Header-style "Key: value" lines are scrubbed: a secret-named key drops its
//     whole value; a non-secret key whose value still looks like a lone
//     credential is dropped by the position backstop.
//  4. key=value / key:value assignments whose key hints at a secret are scrubbed.
//  5. Known vendor value patterns are scrubbed anywhere they appear.
//  6. The entropy/length backstop scrubs remaining opaque high-entropy tokens
//     that match no known vendor prefix (the allow-list's fail-open hole).
func redactString(s string) string {
	if s == "" {
		return s
	}
	s = urlUserinfo.ReplaceAllString(s, "${1}"+RedactedMarker+"@")
	s = redactJSONBlob(s)
	s = redactHeaderLines(s)
	s = assignedSecret.ReplaceAllString(s, "${1}"+RedactedMarker)
	for _, re := range secretValuePatterns {
		s = re.ReplaceAllString(s, RedactedMarker)
	}
	s = redactOpaqueTokens(s)
	return s
}

// redactJSONBlob detects when a string holds a JSON object or array and rewrites
// it through a structural walk so nested secrets are scrubbed with the same
// key-awareness applied at the top level. This closes the "secret in a nested
// field" leak: a value like
//
//	{"integration":{"oauth":{"access_token":"glpat-..."}}}
//
// stores the credential under an inner key, invisible to flat value-pattern
// matching when the token carries no known vendor prefix. The walk redacts any
// value whose key isSecretKey, recurses into nested objects/arrays, and runs the
// remaining string-redaction strategies on every leaf string. If the string does
// not parse as JSON it is returned unchanged so non-JSON free text falls through
// to the flat strategies in redactString.
func redactJSONBlob(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	// Only attempt a structural walk for object/array literals; a bare JSON
	// string/number/bool gains nothing from the round-trip and the flat
	// strategies already cover it.
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return s
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return s
	}
	// Reject trailing garbage after a valid JSON value: that means the string is
	// not purely JSON (e.g. prose that happens to start with "{"), so the flat
	// strategies, not the structural walk, should handle it.
	if dec.More() {
		return s
	}
	walked := redactJSONValue(v)
	out, err := json.Marshal(walked)
	if err != nil {
		return s
	}
	return string(out)
}

// redactJSONValue recursively scrubs a decoded JSON value. Object values whose
// key isSecretKey are replaced wholesale; every other value is recursed into,
// and leaf strings get the flat string strategies (URL userinfo, vendor
// patterns, entropy backstop) — but NOT another JSON round-trip, since the value
// is already structurally decoded here.
func redactJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			if isSecretKey(k) {
				out[k] = RedactedMarker
				continue
			}
			out[k] = redactJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redactJSONValue(child)
		}
		return out
	case string:
		return redactLeafString(t)
	default:
		// json.Number, bool, nil — no string surface to leak a secret.
		return v
	}
}

// redactLeafString runs the flat string-redaction strategies on a single leaf
// value pulled out of a decoded JSON structure. It deliberately does NOT recurse
// back into redactJSONBlob: the caller already decoded the structure, so a leaf
// is terminal text. It mirrors redactString minus the JSON step to avoid
// re-parsing a leaf that merely looks brace-ish.
func redactLeafString(s string) string {
	if s == "" {
		return s
	}
	s = urlUserinfo.ReplaceAllString(s, "${1}"+RedactedMarker+"@")
	s = redactHeaderLines(s)
	s = assignedSecret.ReplaceAllString(s, "${1}"+RedactedMarker)
	for _, re := range secretValuePatterns {
		s = re.ReplaceAllString(s, RedactedMarker)
	}
	s = redactOpaqueTokens(s)
	return s
}

// headerLine matches a "Key: value" line and captures the key so key-aware
// redaction can decide based on the name. Used for trace request strings that
// render headers as lines.
var headerLine = regexp.MustCompile(`(?m)^([A-Za-z0-9_\-]+):[ \t]*(.+)$`)

// redactHeaderLines scrubs the value of any "Key: value" line whose key hints at
// a secret, leaving the key visible so a reviewer knows which header carried a
// credential. When the key is NOT a known secret name, the value is still
// examined: a header value that is a single lone credential-shaped token (no
// spaces, opaque) is dropped by the position backstop, so a secret hiding under
// an unusual header name (e.g. "X-Internal-Sig: tok_clearvalue") does not leak.
func redactHeaderLines(s string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	return headerLine.ReplaceAllStringFunc(s, func(line string) string {
		m := headerLine.FindStringSubmatch(line)
		if len(m) != 3 {
			return line
		}
		key, val := m[1], m[2]
		if isSecretKey(key) {
			return key + ": " + RedactedMarker
		}
		if isLoneCredentialHeaderValue(val) {
			return key + ": " + RedactedMarker
		}
		return line
	})
}

// isLoneCredentialHeaderValue reports whether a non-secret-named header's value
// is itself a lone credential token that must be scrubbed by position. A header
// VALUE is a high-risk slot: a signature/token header can carry a raw secret
// under an unusual key the key-name allow-list never anticipated. The rule
// over-redacts a single opaque token and leaves ordinary header values
// (MIME types, short keywords, numbers, dates, multi-word values) intact.
func isLoneCredentialHeaderValue(val string) bool {
	v := strings.TrimSpace(val)
	if v == "" {
		return false
	}
	// Multi-token values (e.g. "application/json; charset=utf-8", a date) are not
	// lone credentials; their secret-bearing shapes are already handled by the
	// value patterns / assigned-secret rules in the surrounding strategies.
	if strings.ContainsAny(v, " \t") {
		return false
	}
	// MIME types and other slash/semicolon-structured tokens are not credentials.
	if strings.ContainsAny(v, "/;") {
		return false
	}
	// Plain emails and dotted hostnames are routine header values, not secrets.
	if strings.Contains(v, "@") || isDottedHostLike(v) {
		return false
	}
	// A token with a credential-ish prefix (tok_, key_, sig_, sk_, ...) or that is
	// long/high-entropy is treated as a secret. A short, all-lowercase keyword
	// such as "no-cache" or "deny" is not.
	return looksLikeCredentialToken(v)
}

// redactOpaqueTokens is the entropy/length backstop for the value half of
// redaction. The vendor-prefix pattern list is an allow-list and fails OPEN for
// any opaque token off the list (OAuth access/refresh tokens, vendor session
// blobs, mixed-case base64 secrets). This pass scans whitespace/quote-delimited
// tokens and replaces those that look like high-entropy credentials, so the
// value half fails CLOSED like the doc claims. It is deliberately conservative
// about ordinary identifiers (ids, dates, emails, domains, words) so it does not
// nuke the non-secret signal a reviewer needs.
func redactOpaqueTokens(s string) string {
	if s == "" {
		return s
	}
	return tokenSplit.ReplaceAllStringFunc(s, func(tok string) string {
		if looksLikeCredentialToken(tok) {
			return RedactedMarker
		}
		return tok
	})
}

// tokenSplit captures a maximal run of token characters. It deliberately
// EXCLUDES the path separator "/" so a URL path (https://api.crm.test/accounts)
// splits into host + path segments rather than collapsing into one giant token
// the entropy rule would over-redact; "/" is also rare mid-credential because
// the high-value vendor and base64 shapes that use it are already caught by the
// vendor/JWT/hex patterns. Whitespace, quotes, and JSON/prose punctuation also
// break tokens. Each run is offered to the credential heuristic.
var tokenSplit = regexp.MustCompile(`[A-Za-z0-9._\-+=~]+`)

// credentialPrefix matches the leading "<word><sep>" of common opaque tokens
// (tok_, key-, sig_, sess_, rt_, at_, sk-, glpat-, ...). A short keyword like
// "no-cache" trips this only if the rest of the token also looks token-shaped,
// which the entropy/length checks in looksLikeCredentialToken gate.
var credentialPrefix = regexp.MustCompile(`^(?i)(tok|key|sig|sess|session|secret|cred|auth|bearer|access|refresh|rt|at|sk|pk|rk|api)[_\-]`)

// looksLikeCredentialToken is the shared heuristic the entropy backstop and the
// header-value rule use to decide whether a lone token is a credential. It is
// the fail-CLOSED replacement for trusting the vendor allow-list alone:
//
//   - it ignores ordinary identifiers: emails, dotted hostnames, pure UUIDs,
//     ISO-8601 timestamps, and short words — these are the non-secret signal;
//   - it flags a token that is long AND high-entropy AND mixes character classes
//     (the shape of an opaque session/OAuth token), regardless of vendor prefix;
//   - it ALSO flags a moderately long token carrying a credential-ish prefix
//     (tok_, rt_, sk-, ...), catching short-but-named secrets the entropy rule
//     would miss.
func looksLikeCredentialToken(tok string) bool {
	t := strings.Trim(tok, "._-+/=~")
	n := len(t)
	if n < 12 {
		return false
	}
	// Routine structured identifiers are not credentials.
	if strings.Contains(t, "@") {
		return false // email / idempotency key like lead-x@acme.test-v1
	}
	if isUUID(t) {
		return false
	}
	if isDottedHostLike(t) {
		return false
	}
	if isISOTimestamp(t) {
		return false
	}
	hasPrefix := credentialPrefix.MatchString(t)

	// A credential-prefixed token of moderate length is a secret even if its
	// entropy is modest (e.g. "tok_clearvalue_noPattern", "rt_9f8e7d6c...").
	if hasPrefix && n >= 16 {
		return true
	}

	// Otherwise require the opaque-secret shape: long, mixed character classes,
	// and high Shannon entropy. This catches OAuth tokens and mixed-case base64
	// blobs with no recognised prefix while sparing words and plain ids.
	if n < 20 {
		return false
	}
	if !hasMixedClasses(t) {
		return false
	}
	return shannonEntropy(t) >= 3.2
}

// hasMixedClasses reports whether a token mixes at least two of {lowercase,
// uppercase, digit}. A pure-lowercase word or a pure-digit number is not the
// shape of a high-entropy credential and must be spared.
func hasMixedClasses(s string) bool {
	var lower, upper, digit bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	classes := 0
	for _, ok := range []bool{lower, upper, digit} {
		if ok {
			classes++
		}
	}
	return classes >= 2
}

// shannonEntropy returns the per-character Shannon entropy (bits/char) of s. A
// random high-entropy token approaches log2(alphabet); an English word or a
// repetitive id sits well below the 3.2 threshold the backstop uses.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	var freq [256]float64
	var counted float64
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
		counted++
	}
	var h float64
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := c / counted
		h -= p * math.Log2(p)
	}
	return h
}

// isUUID reports whether s is a canonical 8-4-4-4-12 UUID. UUIDs are record
// identifiers, not secrets, so the entropy backstop spares them (an id in a
// path is templated to {id} upstream; a stray one in prose is still not a leak).
func isUUID(s string) bool {
	return uuidID.MatchString(s)
}

// isDottedHostLike reports whether s looks like a dotted hostname/domain
// (label.label[.label...]) with no token-only segments long enough to be a
// credential. "api.crm.test" and "acme.test" return true; a base64 blob that
// merely contains a dot does not.
func isDottedHostLike(s string) bool {
	if !strings.Contains(s, ".") {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" {
			return false
		}
		if !hostLabel.MatchString(l) {
			return false
		}
	}
	return true
}

// hostLabel matches a DNS-label-shaped segment: alphanumeric with internal
// hyphens, no underscores, and not absurdly long.
var hostLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?$`)

// isISOTimestamp reports whether s is an ISO-8601 date or datetime. Timestamps
// are routine record values (signup_at, renewal_date) and must not be redacted.
func isISOTimestamp(s string) bool {
	return isoTimestamp.MatchString(s)
}

var isoTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?(?:Z|[+\-]\d{2}:?\d{2})?)?$`)

// isSecretKey reports whether a key/header/field name signals a secret value.
// Match is case-insensitive substring against secretKeyHints.
func isSecretKey(key string) bool {
	lk := strings.ToLower(strings.TrimSpace(key))
	if lk == "" {
		return false
	}
	for _, hint := range secretKeyHints {
		if strings.Contains(lk, hint) {
			return true
		}
	}
	return false
}

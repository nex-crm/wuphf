//go:build ignore

// verifier-reference.go — single-file Go reference for selected
// @wuphf/protocol wire contracts.
//
// Purpose: prove that an independent implementation in another language
// produces byte-identical hashes and canonical wire bytes from the same inputs
// as the TypeScript writer. If this program prints "all vectors match", the
// wire contract is genuinely cross-language portable. If any vector fails, the
// TS implementation has drifted from the spec — coordinate the bump with
// downstream consumers.
//
// Usage:
//   cd packages/protocol/testdata
//   go run verifier-reference.go
//
// Scope: this file ONLY supports the limited shapes used in the bundled
// vectors (string keys, string values, no numbers in the hashed projection,
// no escape edge cases beyond standard JSON). It is not a general RFC 8785
// JCS implementation; for production use the cyberphone/json-canonicalization
// library or equivalent. The bundled vectors stay within the limited shapes
// on purpose so that this file remains small and pasteable.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type auditPayloadInput struct {
	Kind      string      `json:"kind"`
	ReceiptID interface{} `json:"receiptId"`
	BodyB64   string      `json:"bodyB64"`
}

type auditEventInput struct {
	SeqNo     string            `json:"seqNo"`
	Timestamp string            `json:"timestamp"`
	PrevHash  string            `json:"prevHash"`
	Payload   auditPayloadInput `json:"payload"`
}

type auditEventExpected struct {
	CanonicalSerialization string `json:"canonicalSerialization"`
	EventHash              string `json:"eventHash"`
}

type auditEventVector struct {
	Name           string             `json:"name"`
	Input          auditEventInput    `json:"input"`
	Expected       auditEventExpected `json:"expected"`
	SerializedJSON string             `json:"serializedJson"`
	ExpectedHash   string             `json:"expectedHash"`
}

type merkleRootInput struct {
	SeqNo          string `json:"seqNo"`
	RootHash       string `json:"rootHash"`
	SignedAt       string `json:"signedAt"`
	EphemeralKeyID string `json:"ephemeralKeyId"`
	Signature      string `json:"signature"`
	CertChainPEM   string `json:"certChainPem"`
}

type merkleRootExpected struct {
	CanonicalJSON string `json:"canonicalJson"`
}

type merkleRootVector struct {
	Name     string             `json:"name"`
	Input    merkleRootInput    `json:"input"`
	Expected merkleRootExpected `json:"expected"`
}

type fixture struct {
	SchemaVersion     int                `json:"schemaVersion"`
	Comment           string             `json:"comment"`
	Vectors           []auditEventVector `json:"vectors"`
	MerkleRootVectors []merkleRootVector `json:"merkleRootVectors"`
}

type runnerVector struct {
	Name          string                 `json:"name"`
	Kind          string                 `json:"kind"`
	ErrorCategory string                 `json:"error_category,omitempty"`
	JSON          map[string]interface{} `json:"json"`
}

type runnerFixture struct {
	SchemaVersion int            `json:"schemaVersion"`
	Vectors       []runnerVector `json:"vectors"`
	RejectVectors []runnerVector `json:"rejectVectors"`
}

type agentProviderRoutingExpected struct {
	CanonicalSerialization string `json:"canonicalSerialization"`
}

type agentProviderRoutingAcceptedVector struct {
	Name     string                       `json:"name"`
	Input    json.RawMessage              `json:"input"`
	Expected agentProviderRoutingExpected `json:"expected"`
}

type agentProviderRoutingRejectedVector struct {
	Name          string          `json:"name"`
	Input         json.RawMessage `json:"input"`
	ExpectedError string          `json:"expectedError"`
}

type agentProviderRoutingFixture struct {
	SchemaVersion int                                  `json:"schemaVersion"`
	Comment       string                               `json:"comment"`
	Accepted      []agentProviderRoutingAcceptedVector `json:"accepted"`
	Rejected      []agentProviderRoutingRejectedVector `json:"rejected"`
}

type signedApprovalTokenExpected struct {
	CanonicalSerialization string `json:"canonicalSerialization"`
}

type signedApprovalTokenAcceptedVector struct {
	Name     string                      `json:"name"`
	Input    json.RawMessage             `json:"input"`
	Expected signedApprovalTokenExpected `json:"expected"`
}

type signedApprovalTokenRejectedVector struct {
	Name          string          `json:"name"`
	Input         json.RawMessage `json:"input"`
	ExpectedError string          `json:"expectedError"`
}

type signedApprovalTokenFixture struct {
	SchemaVersion int                                 `json:"schemaVersion"`
	Comment       string                              `json:"comment"`
	Accepted      []signedApprovalTokenAcceptedVector `json:"accepted"`
	Rejected      []signedApprovalTokenRejectedVector `json:"rejected"`
}

type agentProviderRoutingEnvelope struct {
	AgentID string                      `json:"agentId"`
	Routes  []agentProviderRoutingEntry `json:"routes"`
}

type agentProviderRoutingEntry struct {
	Kind            string `json:"kind"`
	CredentialScope string `json:"credentialScope"`
	ProviderKind    string `json:"providerKind"`
}

type agentProviderRoutingRawEnvelope struct {
	AgentID json.RawMessage `json:"agentId"`
	Routes  json.RawMessage `json:"routes"`
}

type agentProviderRoutingRawEntry struct {
	Kind            json.RawMessage `json:"kind"`
	CredentialScope json.RawMessage `json:"credentialScope"`
	ProviderKind    json.RawMessage `json:"providerKind"`
}

// canonicalize is a *minimal* JCS implementation sufficient for the bundled
// vectors. Sorts object keys lexicographically (Go's encoding/json does this
// for map[string]any by default), uses null for nil, emits no whitespace, and
// keeps '<', '>', and '&' unescaped to match ECMAScript JSON.stringify/JCS.
//
// LIMITATIONS vs full RFC 8785: no number normalization (vectors carry no
// numbers in the hashed projection), no special handling of -0 or NaN, no
// surrogate-pair sanitization. If you extend the vectors with numbers or
// adversarial Unicode, swap this for a real JCS library before trusting the
// match.
func canonicalize(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// computeEventHash mirrors packages/protocol/src/audit-event.ts:computeEventHash.
//
//	eventHash = sha256(asciiLowerHex(prevHash) || jcsBytes(record))
//
// The ASCII-hex form of prevHash (rather than 32 raw bytes) is intentional —
// it keeps the chain trivially readable in JSON dumps but is non-standard, so
// any cross-language verifier MUST mix the 64-byte ASCII string. This function
// is the spec.
func computeEventHash(prevHash string, recordBytes []byte) string {
	h := sha256.New()
	h.Write([]byte(prevHash)) // ASCII bytes of the 64-char hex
	h.Write(recordBytes)
	return hex.EncodeToString(h.Sum(nil))
}

// serializeAuditEventRecordForHash mirrors the same-named function in TS.
// Builds the canonical JSON projection over which the eventHash is computed.
func serializeAuditEventRecordForHash(rec auditEventInput) ([]byte, error) {
	receiptIDForJSON := rec.Payload.ReceiptID
	if receiptIDForJSON == nil {
		receiptIDForJSON = nil // emits null
	}
	projection := map[string]interface{}{
		"seqNo":     rec.SeqNo,
		"timestamp": rec.Timestamp,
		"prevHash":  rec.PrevHash,
		"payload": map[string]interface{}{
			"kind":      rec.Payload.Kind,
			"receiptId": receiptIDForJSON,
			"bodyB64":   rec.Payload.BodyB64,
		},
	}
	return canonicalize(projection)
}

func canonicalMerkleRoot(rec merkleRootInput) ([]byte, error) {
	projection := map[string]interface{}{
		"seqNo":          rec.SeqNo,
		"rootHash":       rec.RootHash,
		"signedAt":       rec.SignedAt,
		"ephemeralKeyId": rec.EphemeralKeyID,
		"signature":      rec.Signature,
		"certChainPem":   rec.CertChainPEM,
	}
	return canonicalize(projection)
}

// costPayloadKindSet identifies bodyB64 kinds whose payload itself is
// expected to be canonical JSON (RFC 8785 JCS). The TS writer guarantees
// `costAuditPayloadToBytes` emits canonical bytes; this verifier
// independently decodes, re-parses, and re-canonicalizes the body to
// confirm a non-TS implementation produces the same bytes. Other kinds
// (boot_marker, thread_*) carry kind-specific bodies — this verifier
// does not yet decode them.
var costPayloadKindSet = map[string]bool{
	"cost_event":               true,
	"budget_set":               true,
	"budget_threshold_crossed": true,
}

// canonicalizeBodyBytes decodes a base64 body, parses it as JSON, and
// re-canonicalizes via the minimal JCS implementation. Used to confirm
// the TS writer's canonical body bytes are reproducible from an
// independent canonicalizer.
func canonicalizeBodyBytes(bodyB64 string) ([]byte, []byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(bodyB64)
	if err != nil {
		return nil, nil, fmt.Errorf("base64 decode: %w", err)
	}
	var parsed interface{}
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		return decoded, nil, fmt.Errorf("body is not JSON: %w", err)
	}
	canonical, err := canonicalize(parsed)
	if err != nil {
		return decoded, nil, fmt.Errorf("canonicalize: %w", err)
	}
	return decoded, canonical, nil
}

var (
	agentIDRE          = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
	approvalClaimIDRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	base64URLRE        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	credentialHandleRE = regexp.MustCompile(`^cred_[A-Za-z0-9_-]{22,128}$`)
	costCeilingIDRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	runnerIDRE         = regexp.MustCompile(`^run_[A-Za-z0-9_-]{22,128}$`)
	sha256HexRE        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	ulidRE             = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	isoUtcRE           = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	writeIDRE          = regexp.MustCompile(`^write_[A-Za-z0-9_-]{1,122}$`)
)

var approvalClaimKindSet = map[string]bool{
	"cost_spike_acknowledgement":   true,
	"endpoint_allowlist_extension": true,
	"credential_grant_to_agent":    true,
	"receipt_co_sign":              true,
}

var approvalRoleSet = map[string]bool{
	"viewer":   true,
	"approver": true,
	"host":     true,
}

var riskClassSet = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

var runnerKindSet = map[string]bool{
	"claude-cli":    true,
	"codex-cli":     true,
	"openai-compat": true,
}

var runnerKindOrder = map[string]int{
	"claude-cli":    0,
	"codex-cli":     1,
	"openai-compat": 2,
}

var credentialScopeSet = map[string]bool{
	"anthropic":     true,
	"openai":        true,
	"openai-compat": true,
	"ollama":        true,
	"openclaw":      true,
	"hermes-agent":  true,
	"openclaw-http": true,
	"opencode":      true,
	"opencodego":    true,
	"github":        true,
}

var providerKindSet = map[string]bool{
	"anthropic":     true,
	"openai":        true,
	"openai-compat": true,
	"ollama":        true,
	"openclaw":      true,
	"hermes-agent":  true,
	"openclaw-http": true,
	"opencode":      true,
	"opencodego":    true,
}

var runnerFailureCodeSet = map[string]bool{
	"spawn_failed":                     true,
	"receipt_write_failed":             true,
	"event_log_write_failed":           true,
	"cost_ledger_write_failed":         true,
	"cost_ceiling_exceeded":            true,
	"credential_ownership_mismatch":    true,
	"subprocess_crashed":               true,
	"subprocess_timed_out":             true,
	"terminated_by_request":            true,
	"network_failed":                   true,
	"provider_returned_error":          true,
	"unrecognized_provider_response":   true,
	"subscriber_backpressure_exceeded": true,
	"runner_input_buffer_overflow":     true,
}

const (
	maxRunnerPromptBytes            = 64 * 1024
	maxRunnerModelBytes             = 128
	maxRunnerCwdBytes               = 4 * 1024
	maxRunnerStdioChunkBytes        = 64 * 1024
	maxRunnerErrorBytes             = 8 * 1024
	maxRunnerExtraArgs              = 64
	maxRunnerExtraArgBytes          = 1024
	maxRunnerProfileBytes           = 128
	maxRunnerEndpointBytes          = 2 * 1024
	maxRunnerOptionHeaders          = 64
	maxRunnerOptionHeaderNameBytes  = 256
	maxRunnerOptionHeaderValueBytes = 8 * 1024
	maxAgentIDBytes                 = 128
	maxCredentialHandleBytes        = 128
	maxCredentialHandleIDBytes      = len("cred_") + 128
	maxCredentialScopeBytes         = 128
	maxRunnerIDBytes                = 128
	maxCostModelBytes               = 128
	maxCostEventAmountMicroUsd      = 100_000_000
	maxBudgetLimitMicroUsd          = 1_000_000_000_000
	maxSafeInteger                  = 9_007_199_254_740_991
	maxAgentProviderRoutes          = 16
	maxApprovalTokenLifetimeMs      = 30 * 60 * 1000
	maxApprovalClaimCanonicalBytes  = 64 * 1024
	maxApprovalScopeCanonicalBytes  = 8 * 1024
	maxApprovalIdentifierBytes      = 256
	maxApprovalCostCeilingIDBytes   = 128
	maxApprovalEndpointOriginBytes  = 2 * 1024
	maxApprovalReasonBytes          = 8 * 1024
	maxWebAuthnAssertionFieldBytes  = 16 * 1024
	maxWebAuthnAssertionBytes       = 16 * 1024
	maxBudgetThresholdBps           = 10_000
)

func loadRunnerFixture() (runnerFixture, error) {
	fixtureBytes, err := os.ReadFile("runner-vectors.json")
	if err != nil {
		return runnerFixture{}, err
	}
	var fx runnerFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return runnerFixture{}, err
	}
	if fx.SchemaVersion != 1 {
		return runnerFixture{}, fmt.Errorf("unsupported runner fixture schemaVersion: %d", fx.SchemaVersion)
	}
	return fx, nil
}

func loadAgentProviderRoutingFixture() (agentProviderRoutingFixture, error) {
	fixtureBytes, err := os.ReadFile("agent-provider-routing-vectors.json")
	if err != nil {
		return agentProviderRoutingFixture{}, err
	}
	var fx agentProviderRoutingFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return agentProviderRoutingFixture{}, err
	}
	if fx.SchemaVersion != 1 {
		return agentProviderRoutingFixture{}, fmt.Errorf("unsupported agent-provider-routing fixture schemaVersion: %d", fx.SchemaVersion)
	}
	return fx, nil
}

func loadSignedApprovalTokenFixture() (signedApprovalTokenFixture, error) {
	fixtureBytes, err := os.ReadFile("signed-approval-token-vectors.json")
	if err != nil {
		return signedApprovalTokenFixture{}, err
	}
	var fx signedApprovalTokenFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return signedApprovalTokenFixture{}, err
	}
	if fx.SchemaVersion != 1 {
		return signedApprovalTokenFixture{}, fmt.Errorf("unsupported signed-approval-token fixture schemaVersion: %d", fx.SchemaVersion)
	}
	return fx, nil
}

func parseAgentProviderRouting(raw json.RawMessage) (agentProviderRoutingEnvelope, error) {
	var rawEnvelope agentProviderRoutingRawEnvelope
	if err := decodeStrictJSON(raw, "agentProviderRouting", &rawEnvelope); err != nil {
		return agentProviderRoutingEnvelope{}, err
	}
	agentID, err := requiredRawString(rawEnvelope.AgentID, "agentProviderRouting.agentId")
	if err != nil {
		return agentProviderRoutingEnvelope{}, err
	}
	if err := validateUtf8Budget(agentID, maxAgentIDBytes, "agentProviderRouting.agentId"); err != nil {
		return agentProviderRoutingEnvelope{}, err
	}
	if !agentIDRE.MatchString(agentID) {
		return agentProviderRoutingEnvelope{}, fmt.Errorf("agentProviderRouting.agentId: not an AgentId")
	}
	rawRoutes, err := requiredRawArray(rawEnvelope.Routes, "agentProviderRouting.routes")
	if err != nil {
		return agentProviderRoutingEnvelope{}, err
	}
	if len(rawRoutes) > maxAgentProviderRoutes {
		return agentProviderRoutingEnvelope{}, fmt.Errorf("agentProviderRouting.routes: exceeds %d entries (got %d)", maxAgentProviderRoutes, len(rawRoutes))
	}
	seenKinds := map[string]bool{}
	entries := make([]agentProviderRoutingEntry, 0, len(rawRoutes))
	for index, rawEntry := range rawRoutes {
		path := fmt.Sprintf("agentProviderRouting.routes/%d", index)
		entry, err := parseAgentProviderRoutingEntry(rawEntry, path)
		if err != nil {
			return agentProviderRoutingEnvelope{}, err
		}
		if seenKinds[entry.Kind] {
			return agentProviderRoutingEnvelope{}, fmt.Errorf("%s.kind: duplicate route for kind %q", path, entry.Kind)
		}
		seenKinds[entry.Kind] = true
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return runnerKindOrder[entries[i].Kind] < runnerKindOrder[entries[j].Kind]
	})
	return agentProviderRoutingEnvelope{
		AgentID: agentID,
		Routes:  entries,
	}, nil
}

func parseAgentProviderRoutingEntry(raw json.RawMessage, path string) (agentProviderRoutingEntry, error) {
	var rawEntry agentProviderRoutingRawEntry
	if err := decodeStrictJSON(raw, path, &rawEntry); err != nil {
		return agentProviderRoutingEntry{}, err
	}
	kind, err := requiredRawString(rawEntry.Kind, path+".kind")
	if err != nil {
		return agentProviderRoutingEntry{}, err
	}
	if !runnerKindSet[kind] {
		return agentProviderRoutingEntry{}, fmt.Errorf("%s.kind: not a supported RunnerKind", path)
	}
	credentialScope, err := requiredRawString(rawEntry.CredentialScope, path+".credentialScope")
	if err != nil {
		return agentProviderRoutingEntry{}, err
	}
	if err := validateUtf8Budget(credentialScope, maxCredentialScopeBytes, path+".credentialScope"); err != nil {
		return agentProviderRoutingEntry{}, err
	}
	if !credentialScopeSet[credentialScope] {
		return agentProviderRoutingEntry{}, fmt.Errorf("%s.credentialScope: not a supported CredentialScope", path)
	}
	providerKind, err := requiredRawString(rawEntry.ProviderKind, path+".providerKind")
	if err != nil {
		return agentProviderRoutingEntry{}, err
	}
	if err := validateUtf8Budget(providerKind, maxCredentialScopeBytes, path+".providerKind"); err != nil {
		return agentProviderRoutingEntry{}, err
	}
	if !providerKindSet[providerKind] {
		return agentProviderRoutingEntry{}, fmt.Errorf("%s.providerKind: not a supported ProviderKind", path)
	}
	return agentProviderRoutingEntry{
		Kind:            kind,
		CredentialScope: credentialScope,
		ProviderKind:    providerKind,
	}, nil
}

func validateAgentProviderRoutingAccepted(vec agentProviderRoutingAcceptedVector) error {
	parsed, err := parseAgentProviderRouting(vec.Input)
	if err != nil {
		return err
	}
	serialized, err := json.Marshal(parsed)
	if err != nil {
		return err
	}
	if string(serialized) != vec.Expected.CanonicalSerialization {
		return fmt.Errorf("canonicalSerialization mismatch: expected %s, got %s", vec.Expected.CanonicalSerialization, string(serialized))
	}
	return nil
}

func validateAgentProviderRoutingRejected(vec agentProviderRoutingRejectedVector) error {
	_, err := parseAgentProviderRouting(vec.Input)
	if err == nil {
		return fmt.Errorf("expected reject, got accept")
	}
	if vec.ExpectedError != "" && !strings.Contains(err.Error(), vec.ExpectedError) {
		return fmt.Errorf("expected error containing %q, got %q", vec.ExpectedError, err.Error())
	}
	return nil
}

func validateSignedApprovalTokenAccepted(vec signedApprovalTokenAcceptedVector) error {
	parsed, err := parseSignedApprovalToken(vec.Input)
	if err != nil {
		return err
	}
	serialized, err := canonicalize(parsed)
	if err != nil {
		return err
	}
	if string(serialized) != vec.Expected.CanonicalSerialization {
		return fmt.Errorf("canonicalSerialization mismatch: expected %s, got %s", vec.Expected.CanonicalSerialization, string(serialized))
	}
	return nil
}

func validateSignedApprovalTokenRejected(vec signedApprovalTokenRejectedVector) error {
	_, err := parseSignedApprovalToken(vec.Input)
	if err == nil {
		return fmt.Errorf("expected reject, got accept")
	}
	if vec.ExpectedError != "" && !strings.Contains(err.Error(), vec.ExpectedError) {
		return fmt.Errorf("expected error containing %q, got %q", vec.ExpectedError, err.Error())
	}
	return nil
}

func parseSignedApprovalToken(raw json.RawMessage) (map[string]interface{}, error) {
	var record map[string]interface{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("signedApprovalToken: must be an object: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("signedApprovalToken: must be an object")
	}
	if err := validateSignedApprovalTokenRecord(record, "signedApprovalToken"); err != nil {
		return nil, err
	}
	return record, nil
}

func validateSignedApprovalTokenRecord(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"tokenId":       true,
		"claim":         true,
		"scope":         true,
		"notBefore":     true,
		"expiresAt":     true,
		"issuedTo":      true,
		"signature":     true,
	}); err != nil {
		return err
	}
	if err := requiredExactNumber(record, "schemaVersion", path+"/schemaVersion", 1); err != nil {
		return err
	}
	tokenID, err := requiredStringValue(record, "tokenId", path+"/tokenId")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(tokenID, 26, path+"/tokenId"); err != nil {
		return err
	}
	if !ulidRE.MatchString(tokenID) {
		return fmt.Errorf("%s/tokenId: not an ApprovalTokenId", path)
	}
	claim, err := requiredObjectValue(record, "claim", path+"/claim")
	if err != nil {
		return err
	}
	claimKind, claimID, err := validateApprovalClaim(claim, path+"/claim")
	if err != nil {
		return err
	}
	scope, err := requiredObjectValue(record, "scope", path+"/scope")
	if err != nil {
		return err
	}
	scopeKind, scopeClaimID, err := validateApprovalScope(scope, path+"/scope")
	if err != nil {
		return err
	}
	if claimID != scopeClaimID {
		return fmt.Errorf("%s/scope/claimId: must match claim.claimId", path)
	}
	if claimKind != scopeKind {
		return fmt.Errorf("%s/scope/claimKind: must match claim.kind", path)
	}
	if err := validateApprovalClaimScopeBinding(claimKind, claim, scope, path+"/scope"); err != nil {
		return err
	}
	notBefore, err := requiredSafeInteger(record, "notBefore", path+"/notBefore")
	if err != nil {
		return err
	}
	expiresAt, err := requiredSafeInteger(record, "expiresAt", path+"/expiresAt")
	if err != nil {
		return err
	}
	if expiresAt <= notBefore {
		return fmt.Errorf("%s/expiresAt: must be strictly greater than notBefore", path)
	}
	if expiresAt-notBefore > maxApprovalTokenLifetimeMs {
		return fmt.Errorf("%s/expiresAt: lifetime exceeds %d ms", path, maxApprovalTokenLifetimeMs)
	}
	issuedTo, err := requiredStringValue(record, "issuedTo", path+"/issuedTo")
	if err != nil {
		return err
	}
	if err := validateAgentID(issuedTo, path+"/issuedTo"); err != nil {
		return err
	}
	signature, err := requiredObjectValue(record, "signature", path+"/signature")
	if err != nil {
		return err
	}
	return validateWebAuthnAssertion(signature, path+"/signature")
}

func validateApprovalClaim(record map[string]interface{}, path string) (string, string, error) {
	kind, err := requiredStringValue(record, "kind", path+"/kind")
	if err != nil {
		return "", "", err
	}
	if !approvalClaimKindSet[kind] {
		return "", "", fmt.Errorf("%s/kind: must be a valid approval claim kind", path)
	}
	var allowed map[string]bool
	switch kind {
	case "cost_spike_acknowledgement":
		allowed = map[string]bool{"schemaVersion": true, "claimId": true, "kind": true, "agentId": true, "costCeilingId": true, "thresholdBps": true, "currentMicroUsd": true, "ceilingMicroUsd": true}
	case "endpoint_allowlist_extension":
		allowed = map[string]bool{"schemaVersion": true, "claimId": true, "kind": true, "agentId": true, "providerKind": true, "endpointOrigin": true, "reason": true}
	case "credential_grant_to_agent":
		allowed = map[string]bool{"schemaVersion": true, "claimId": true, "kind": true, "granteeAgentId": true, "credentialHandleId": true, "credentialScope": true}
	default:
		allowed = map[string]bool{"schemaVersion": true, "claimId": true, "kind": true, "receiptId": true, "writeId": true, "frozenArgsHash": true, "riskClass": true}
	}
	if err := knownKeys(record, path, allowed); err != nil {
		return "", "", err
	}
	claimID, err := validateApprovalClaimBase(record, path)
	if err != nil {
		return "", "", err
	}
	switch kind {
	case "cost_spike_acknowledgement":
		if err := validateAgentIDField(record, "agentId", path+"/agentId"); err != nil {
			return "", "", err
		}
		if err := validateCostCeilingIDField(record, "costCeilingId", path+"/costCeilingId"); err != nil {
			return "", "", err
		}
		if err := requiredIntegerInRange(record, "thresholdBps", path+"/thresholdBps", 1, maxBudgetThresholdBps); err != nil {
			return "", "", err
		}
		if err := requiredNonNegativeInteger(record, "currentMicroUsd", path+"/currentMicroUsd", maxBudgetLimitMicroUsd); err != nil {
			return "", "", err
		}
		if err := requiredNonNegativeInteger(record, "ceilingMicroUsd", path+"/ceilingMicroUsd", maxBudgetLimitMicroUsd); err != nil {
			return "", "", err
		}
	case "endpoint_allowlist_extension":
		if err := validateAgentIDField(record, "agentId", path+"/agentId"); err != nil {
			return "", "", err
		}
		providerKind, err := requiredStringValue(record, "providerKind", path+"/providerKind")
		if err != nil {
			return "", "", err
		}
		if !providerKindSet[providerKind] {
			return "", "", fmt.Errorf("%s/providerKind: not a ProviderKind", path)
		}
		if err := validateEndpointOriginField(record, "endpointOrigin", path+"/endpointOrigin"); err != nil {
			return "", "", err
		}
		if err := validateReasonField(record, "reason", path+"/reason"); err != nil {
			return "", "", err
		}
	case "credential_grant_to_agent":
		if err := validateAgentIDField(record, "granteeAgentId", path+"/granteeAgentId"); err != nil {
			return "", "", err
		}
		if err := validateCredentialHandleIDField(record, "credentialHandleId", path+"/credentialHandleId"); err != nil {
			return "", "", err
		}
		if err := validateCredentialScopeField(record, "credentialScope", path+"/credentialScope"); err != nil {
			return "", "", err
		}
	case "receipt_co_sign":
		if err := validateReceiptIDField(record, "receiptId", path+"/receiptId"); err != nil {
			return "", "", err
		}
		if err := validateOptionalWriteIDField(record, "writeId", path+"/writeId"); err != nil {
			return "", "", err
		}
		if err := validateSha256HexField(record, "frozenArgsHash", path+"/frozenArgsHash"); err != nil {
			return "", "", err
		}
		riskClass, err := requiredStringValue(record, "riskClass", path+"/riskClass")
		if err != nil {
			return "", "", err
		}
		if !riskClassSet[riskClass] {
			return "", "", fmt.Errorf("%s/riskClass: must be a valid risk class", path)
		}
	}
	canonical, err := canonicalize(record)
	if err != nil {
		return "", "", err
	}
	if len(canonical) > maxApprovalClaimCanonicalBytes {
		return "", "", fmt.Errorf("%s: claim canonical JSON exceeds budget", path)
	}
	return kind, claimID, nil
}

func validateApprovalScope(record map[string]interface{}, path string) (string, string, error) {
	claimKind, err := requiredStringValue(record, "claimKind", path+"/claimKind")
	if err != nil {
		return "", "", err
	}
	if !approvalClaimKindSet[claimKind] {
		return "", "", fmt.Errorf("%s/claimKind: must be a valid approval claim kind", path)
	}
	var allowed map[string]bool
	switch claimKind {
	case "cost_spike_acknowledgement":
		allowed = map[string]bool{"mode": true, "claimId": true, "claimKind": true, "role": true, "maxUses": true, "agentId": true, "costCeilingId": true}
	case "endpoint_allowlist_extension":
		allowed = map[string]bool{"mode": true, "claimId": true, "claimKind": true, "role": true, "maxUses": true, "agentId": true, "providerKind": true, "endpointOrigin": true}
	case "credential_grant_to_agent":
		allowed = map[string]bool{"mode": true, "claimId": true, "claimKind": true, "role": true, "maxUses": true, "granteeAgentId": true, "credentialHandleId": true}
	default:
		allowed = map[string]bool{"mode": true, "claimId": true, "claimKind": true, "role": true, "maxUses": true, "receiptId": true, "writeId": true, "frozenArgsHash": true}
	}
	if err := knownKeys(record, path, allowed); err != nil {
		return "", "", err
	}
	claimID, err := validateApprovalScopeBase(record, path)
	if err != nil {
		return "", "", err
	}
	switch claimKind {
	case "cost_spike_acknowledgement":
		if err := validateAgentIDField(record, "agentId", path+"/agentId"); err != nil {
			return "", "", err
		}
		if err := validateCostCeilingIDField(record, "costCeilingId", path+"/costCeilingId"); err != nil {
			return "", "", err
		}
	case "endpoint_allowlist_extension":
		if err := validateAgentIDField(record, "agentId", path+"/agentId"); err != nil {
			return "", "", err
		}
		providerKind, err := requiredStringValue(record, "providerKind", path+"/providerKind")
		if err != nil {
			return "", "", err
		}
		if !providerKindSet[providerKind] {
			return "", "", fmt.Errorf("%s/providerKind: not a ProviderKind", path)
		}
		if err := validateEndpointOriginField(record, "endpointOrigin", path+"/endpointOrigin"); err != nil {
			return "", "", err
		}
	case "credential_grant_to_agent":
		if err := validateAgentIDField(record, "granteeAgentId", path+"/granteeAgentId"); err != nil {
			return "", "", err
		}
		if err := validateCredentialHandleIDField(record, "credentialHandleId", path+"/credentialHandleId"); err != nil {
			return "", "", err
		}
	case "receipt_co_sign":
		if err := validateReceiptIDField(record, "receiptId", path+"/receiptId"); err != nil {
			return "", "", err
		}
		if err := validateOptionalWriteIDField(record, "writeId", path+"/writeId"); err != nil {
			return "", "", err
		}
		if err := validateSha256HexField(record, "frozenArgsHash", path+"/frozenArgsHash"); err != nil {
			return "", "", err
		}
	}
	canonical, err := canonicalize(record)
	if err != nil {
		return "", "", err
	}
	if len(canonical) > maxApprovalScopeCanonicalBytes {
		return "", "", fmt.Errorf("%s: scope canonical JSON exceeds budget", path)
	}
	return claimKind, claimID, nil
}

func validateApprovalClaimBase(record map[string]interface{}, path string) (string, error) {
	if err := requiredExactNumber(record, "schemaVersion", path+"/schemaVersion", 1); err != nil {
		return "", err
	}
	claimID, err := requiredStringValue(record, "claimId", path+"/claimId")
	if err != nil {
		return "", err
	}
	if err := validateUtf8Budget(claimID, 128, path+"/claimId"); err != nil {
		return "", err
	}
	if !approvalClaimIDRE.MatchString(claimID) {
		return "", fmt.Errorf("%s/claimId: not an ApprovalClaimId", path)
	}
	return claimID, nil
}

func validateApprovalScopeBase(record map[string]interface{}, path string) (string, error) {
	mode, err := requiredStringValue(record, "mode", path+"/mode")
	if err != nil {
		return "", err
	}
	if mode != "single_use" {
		return "", fmt.Errorf("%s/mode: must be single_use", path)
	}
	claimID, err := requiredStringValue(record, "claimId", path+"/claimId")
	if err != nil {
		return "", err
	}
	if err := validateUtf8Budget(claimID, 128, path+"/claimId"); err != nil {
		return "", err
	}
	if !approvalClaimIDRE.MatchString(claimID) {
		return "", fmt.Errorf("%s/claimId: not an ApprovalClaimId", path)
	}
	role, err := requiredStringValue(record, "role", path+"/role")
	if err != nil {
		return "", err
	}
	if err := validateUtf8Budget(role, maxApprovalIdentifierBytes, path+"/role"); err != nil {
		return "", err
	}
	if !approvalRoleSet[role] {
		return "", fmt.Errorf("%s/role: must be a valid approval role", path)
	}
	if err := requiredExactNumber(record, "maxUses", path+"/maxUses", 1); err != nil {
		return "", err
	}
	return claimID, nil
}

func validateApprovalClaimScopeBinding(kind string, claim map[string]interface{}, scope map[string]interface{}, path string) error {
	switch kind {
	case "cost_spike_acknowledgement":
		return validateSameFields(claim, scope, path, []string{"agentId", "costCeilingId"})
	case "endpoint_allowlist_extension":
		return validateSameFields(claim, scope, path, []string{"agentId", "providerKind", "endpointOrigin"})
	case "credential_grant_to_agent":
		return validateSameFields(claim, scope, path, []string{"granteeAgentId", "credentialHandleId"})
	case "receipt_co_sign":
		return validateSameFields(claim, scope, path, []string{"receiptId", "writeId", "frozenArgsHash"})
	default:
		return nil
	}
}

func validateSameFields(claim map[string]interface{}, scope map[string]interface{}, path string, fields []string) error {
	for _, field := range fields {
		if claim[field] != scope[field] {
			return fmt.Errorf("%s/%s: must match claim.%s", path, field, field)
		}
	}
	return nil
}

func validateWebAuthnAssertion(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"credentialId":      true,
		"authenticatorData": true,
		"clientDataJson":    true,
		"signature":         true,
		"userHandle":        true,
	}); err != nil {
		return err
	}
	for _, field := range []string{"credentialId", "authenticatorData", "clientDataJson", "signature"} {
		if err := validateBase64URLField(record, field, path+"/"+field); err != nil {
			return err
		}
	}
	if _, ok := record["userHandle"]; ok {
		if err := validateBase64URLField(record, "userHandle", path+"/userHandle"); err != nil {
			return err
		}
	}
	canonical, err := canonicalize(record)
	if err != nil {
		return err
	}
	if len(canonical) > maxWebAuthnAssertionBytes {
		return fmt.Errorf("%s: WebAuthn assertion bytes exceed budget", path)
	}
	return nil
}

func validateAgentIDField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	return validateAgentID(value, path)
}

func validateAgentID(value string, path string) error {
	if err := validateUtf8Budget(value, maxAgentIDBytes, path); err != nil {
		return err
	}
	if !agentIDRE.MatchString(value) {
		return fmt.Errorf("%s: not an AgentId", path)
	}
	return nil
}

func validateCostCeilingIDField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxApprovalCostCeilingIDBytes, path); err != nil {
		return err
	}
	sanitized := sanitizeAllowlistText(value)
	if err := validateUtf8Budget(sanitized, maxApprovalCostCeilingIDBytes, path); err != nil {
		return err
	}
	if !costCeilingIDRE.MatchString(sanitized) {
		return fmt.Errorf("%s: must be a valid cost ceiling id", path)
	}
	record[key] = sanitized
	return nil
}

func validateEndpointOriginField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxApprovalEndpointOriginBytes, path); err != nil {
		return err
	}
	sanitized := sanitizeAllowlistText(value)
	if err := validateUtf8Budget(sanitized, maxApprovalEndpointOriginBytes, path); err != nil {
		return err
	}
	parsed, err := url.Parse(sanitized)
	if err != nil {
		return fmt.Errorf("%s: must be an http(s) URL origin", path)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" || parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return fmt.Errorf("%s: must be an http(s) URL origin", path)
	}
	if !isASCII(parsed.Hostname()) {
		return fmt.Errorf("%s: must be a canonical URL origin", path)
	}
	origin := canonicalURLOrigin(parsed)
	if origin != sanitized {
		return fmt.Errorf("%s: must be a canonical URL origin", path)
	}
	record[key] = origin
	return nil
}

func validateReasonField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxApprovalReasonBytes, path); err != nil {
		return err
	}
	sanitized := sanitizeAllowlistText(value)
	if err := validateUtf8Budget(sanitized, maxApprovalReasonBytes, path); err != nil {
		return err
	}
	record[key] = sanitized
	return nil
}

func canonicalURLOrigin(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		host += ":" + port
	}
	return scheme + "://" + host
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 0x7f {
			return false
		}
	}
	return true
}

func sanitizeAllowlistText(value string) string {
	// Frozen NFKC, NOT golang.org/x/text norm.NFKC: the moat normalises against
	// the version-pinned tables in nfkc-table.json so a signer and a verifier
	// on different Unicode versions agree on the sanitized bytes. See
	// frozenNFKC below and src/nfkc-core.ts (the production TS algorithm this
	// mirrors).
	//
	// Input domain: well-formed UTF-8 only. Production's TS sanitizer calls
	// `rejectLoneSurrogates` before normalising and THROWS on a lone surrogate;
	// the Go `range` below would instead substitute U+FFFD. Every input this
	// reference sees comes from a JSON fixture and is well-formed UTF-8, so the
	// two never diverge in practice — but this reference does not re-implement
	// the lone-surrogate rejection.
	normalized := frozenNfkcTables.frozenNFKC(value)
	var out strings.Builder
	for _, r := range normalized {
		// Strip against the FROZEN moat ranges (moat-disallowed-table.json),
		// not Go's live unicode.C* tables — otherwise the reference's sanitized
		// bytes would still be Unicode-version-coupled. tab/LF/CR are Cc and so
		// in the table; the moat carves them out as intentional whitespace.
		if r == '\t' || r == '\n' || r == '\r' ||
			!moatDisallowed(int(r), frozenMoatDisallowedRanges) {
			out.WriteRune(r)
		}
	}
	// Re-normalize after stripping — mirrors the production TS sanitizer's
	// second frozenNfkc pass. Stripping a composition-blocking code point can
	// leave neighbours that NFKC composes; without this pass the moat is not
	// idempotent and the Go reference would compute a different sanitized
	// value than production for joiner-between-marks inputs.
	return frozenNfkcTables.frozenNFKC(out.String())
}

func isAllowlistDisallowedCodePoint(r rune) bool {
	if r <= 0x1f && r != '\t' && r != '\n' && r != '\r' {
		return true
	}
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cn, unicode.Co, unicode.Cs) {
		return true
	}
	return unicode.Is(defaultIgnorableCodePointTable, r)
}

const defaultIgnorableCodePointUnicodeVersion = "15.1.0"

// Complete Default_Ignorable_Code_Point table pinned to Unicode 15.1.0,
// generated from the ECMAScript property escape
// /\p{Default_Ignorable_Code_Point}/u used by sanitized-string.ts.
// Go's stdlib Unicode tables expose Other_Default_Ignorable_Code_Point and
// Variation_Selector separately, but not the derived property as a single
// table, so the verifier keeps the exact generated ranges here.
var defaultIgnorableCodePointTable = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x00ad, Hi: 0x00ad, Stride: 1},
		{Lo: 0x034f, Hi: 0x034f, Stride: 1},
		{Lo: 0x061c, Hi: 0x061c, Stride: 1},
		{Lo: 0x115f, Hi: 0x1160, Stride: 1},
		{Lo: 0x17b4, Hi: 0x17b5, Stride: 1},
		{Lo: 0x180b, Hi: 0x180f, Stride: 1},
		{Lo: 0x200b, Hi: 0x200f, Stride: 1},
		{Lo: 0x202a, Hi: 0x202e, Stride: 1},
		{Lo: 0x2060, Hi: 0x206f, Stride: 1},
		{Lo: 0x3164, Hi: 0x3164, Stride: 1},
		{Lo: 0xfe00, Hi: 0xfe0f, Stride: 1},
		{Lo: 0xfeff, Hi: 0xfeff, Stride: 1},
		{Lo: 0xffa0, Hi: 0xffa0, Stride: 1},
		{Lo: 0xfff0, Hi: 0xfff8, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x1bca0, Hi: 0x1bca3, Stride: 1},
		{Lo: 0x1d173, Hi: 0x1d17a, Stride: 1},
		{Lo: 0xe0000, Hi: 0xe0fff, Stride: 1},
	},
}

// --- Frozen NFKC ----------------------------------------------------------
//
// The moat normalises text to NFKC before and after the strip. Using
// golang.org/x/text norm.NFKC would couple the result to whatever Unicode
// version that library ships — defeating the cross-runtime byte equality the
// moat exists to guarantee. So this reference re-implements NFKC against the
// version-pinned tables in nfkc-table.json, exactly mirroring the production
// TypeScript algorithm in src/nfkc-core.ts. Hangul is decomposed/composed
// algorithmically (its syllable layout is mathematical and stable), so the
// JSON table omits the 11,172 Hangul syllables.

type nfkcDecompEntry struct {
	CP int   `json:"cp"`
	To []int `json:"to"`
}

type nfkcVector struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Name     string `json:"name"`
}

type nfkcTableFixture struct {
	Description          string            `json:"description"`
	SchemaVersion        int               `json:"schemaVersion"`
	UnicodeVersion       string            `json:"unicodeVersion"`
	GeneratedBy          string            `json:"generatedBy"`
	Decomposition        []nfkcDecompEntry `json:"decomposition"`
	Composition          [][3]int          `json:"composition"`
	CombiningClass       [][2]int          `json:"combiningClass"`
	NormalizationVectors []nfkcVector      `json:"normalizationVectors"`
}

// nfkcTables holds the frozen NFKC data in lookup-ready form.
type nfkcTables struct {
	decomposition  map[rune][]rune
	composition    map[[2]rune]rune
	combiningClass map[rune]int
}

// frozenNfkcTables and frozenMoatDisallowedRanges are loaded once in main
// before any sanitizeAllowlistText call — it normalises via the former and
// strips against the latter, both version-pinned.
var frozenNfkcTables nfkcTables
var frozenMoatDisallowedRanges [][2]int

// Hangul algorithmic constants — Unicode §3.12 / UAX #15.
const (
	hangulSBase  = 0xac00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11a7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

func buildNfkcTables(fx nfkcTableFixture) nfkcTables {
	decomposition := make(map[rune][]rune, len(fx.Decomposition))
	for _, entry := range fx.Decomposition {
		runes := make([]rune, len(entry.To))
		for i, cp := range entry.To {
			runes[i] = rune(cp)
		}
		decomposition[rune(entry.CP)] = runes
	}
	composition := make(map[[2]rune]rune, len(fx.Composition))
	for _, pair := range fx.Composition {
		composition[[2]rune{rune(pair[0]), rune(pair[1])}] = rune(pair[2])
	}
	combiningClass := make(map[rune]int, len(fx.CombiningClass))
	for _, entry := range fx.CombiningClass {
		combiningClass[rune(entry[0])] = entry[1]
	}
	return nfkcTables{decomposition: decomposition, composition: composition, combiningClass: combiningClass}
}

func loadNfkcTableFixture() (nfkcTableFixture, error) {
	fixtureBytes, err := os.ReadFile("nfkc-table.json")
	if err != nil {
		return nfkcTableFixture{}, err
	}
	var fx nfkcTableFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return nfkcTableFixture{}, err
	}
	return fx, nil
}

// ccc returns the Canonical_Combining_Class — absent ⇒ 0 (a starter).
func (t nfkcTables) ccc(r rune) int {
	return t.combiningClass[r]
}

// isPureASCII reports whether every byte of s is < 0x80. A pure-ASCII string
// is already in NFKC in every Unicode version, so frozenNFKC can return it
// unchanged — matching the TS fast path in nfkc-core.ts.
func isPureASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// decomposeRune appends the full compatibility decomposition of r to out. The
// table stores fully-recursive NFKD (the TS generator's assertWellFormed
// proves it), so a single splice — no recursion here — is complete.
func (t nfkcTables) decomposeRune(r rune, out []rune) []rune {
	if r >= hangulSBase && r < hangulSBase+hangulSCount {
		syllableIndex := int(r) - hangulSBase
		out = append(out, rune(hangulLBase+syllableIndex/hangulNCount))
		out = append(out, rune(hangulVBase+(syllableIndex%hangulNCount)/hangulTCount))
		if trailingIndex := syllableIndex % hangulTCount; trailingIndex > 0 {
			out = append(out, rune(hangulTBase+trailingIndex))
		}
		return out
	}
	if mapped, ok := t.decomposition[r]; ok {
		return append(out, mapped...)
	}
	return append(out, r)
}

// canonicalOrder stably sorts each maximal non-starter run by combining class.
func (t nfkcTables) canonicalOrder(cps []rune) {
	index := 0
	for index < len(cps) {
		if t.ccc(cps[index]) == 0 {
			index++
			continue
		}
		runEnd := index
		for runEnd < len(cps) && t.ccc(cps[runEnd]) != 0 {
			runEnd++
		}
		for cursor := index + 1; cursor < runEnd; cursor++ {
			cp := cps[cursor]
			cpClass := t.ccc(cp)
			probe := cursor - 1
			for probe >= index && t.ccc(cps[probe]) > cpClass {
				cps[probe+1] = cps[probe]
				probe--
			}
			cps[probe+1] = cp
		}
		index = runEnd
	}
}

// composePair returns the Hangul-aware primary composite of an ordered pair.
func (t nfkcTables) composePair(starter, second rune) (rune, bool) {
	if starter >= hangulLBase && starter < hangulLBase+hangulLCount &&
		second >= hangulVBase && second < hangulVBase+hangulVCount {
		leadingIndex := int(starter) - hangulLBase
		vowelIndex := int(second) - hangulVBase
		return rune(hangulSBase + (leadingIndex*hangulVCount+vowelIndex)*hangulTCount), true
	}
	if starter >= hangulSBase && starter < hangulSBase+hangulSCount &&
		(int(starter)-hangulSBase)%hangulTCount == 0 &&
		second > hangulTBase && second < hangulTBase+hangulTCount {
		return starter + (second - hangulTBase), true
	}
	if composed, ok := t.composition[[2]rune{starter, second}]; ok {
		return composed, true
	}
	return 0, false
}

// compose performs canonical composition with the UAX #15 blocked rule.
func (t nfkcTables) compose(cps []rune) []rune {
	out := make([]rune, 0, len(cps))
	starterIndex := -1
	var starterCP rune
	lastKeptClass := -1
	for _, cp := range cps {
		cpClass := t.ccc(cp)
		notBlocked := lastKeptClass == -1 || lastKeptClass < cpClass
		if starterIndex >= 0 && notBlocked {
			if composed, ok := t.composePair(starterCP, cp); ok {
				out[starterIndex] = composed
				starterCP = composed
				continue
			}
		}
		out = append(out, cp)
		if cpClass == 0 {
			starterIndex = len(out) - 1
			starterCP = cp
			lastKeptClass = -1
		} else {
			lastKeptClass = cpClass
		}
	}
	return out
}

// frozenNFKC normalises input to NFKC against the frozen tables.
func (t nfkcTables) frozenNFKC(input string) string {
	if len(input) == 0 {
		return ""
	}
	if isPureASCII(input) {
		return input // already NFKC — matches the TS fast path
	}
	cps := make([]rune, 0, len(input))
	for _, r := range input {
		cps = t.decomposeRune(r, cps)
	}
	t.canonicalOrder(cps)
	return string(t.compose(cps))
}

func validateCredentialHandleIDField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxCredentialHandleIDBytes, path); err != nil {
		return err
	}
	if !credentialHandleRE.MatchString(value) {
		return fmt.Errorf("%s: not a CredentialHandleId", path)
	}
	return nil
}

func validateCredentialScopeField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxCredentialScopeBytes, path); err != nil {
		return err
	}
	if !credentialScopeSet[value] {
		return fmt.Errorf("%s: not a CredentialScope", path)
	}
	return nil
}

func validateReceiptIDField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !ulidRE.MatchString(value) {
		return fmt.Errorf("%s: not a ReceiptId", path)
	}
	return nil
}

func validateOptionalWriteIDField(record map[string]interface{}, key string, path string) error {
	value, ok, err := optionalString(record, key, path)
	if err != nil || !ok {
		return err
	}
	if !writeIDRE.MatchString(value) {
		return fmt.Errorf("%s: not a WriteId", path)
	}
	return nil
}

func validateSha256HexField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !sha256HexRE.MatchString(value) {
		return fmt.Errorf("%s: not a Sha256Hex", path)
	}
	return nil
}

func validateBase64URLField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(value, maxWebAuthnAssertionFieldBytes, path); err != nil {
		return err
	}
	if !base64URLRE.MatchString(value) {
		return fmt.Errorf("%s: must be a canonical non-empty unpadded base64url string", path)
	}
	if len(value)%4 == 1 {
		return fmt.Errorf("%s: must be a canonical non-empty unpadded base64url string", path)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return fmt.Errorf("%s: must be a canonical non-empty unpadded base64url string", path)
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != value {
		return fmt.Errorf("%s: must be a canonical non-empty unpadded base64url string", path)
	}
	return nil
}

func validateRunnerVector(vec runnerVector) error {
	switch vec.Kind {
	case "spawnRequest":
		return validateRunnerSpawnRequest(vec.JSON)
	case "event":
		return validateRunnerEvent(vec.JSON)
	default:
		return fmt.Errorf("unsupported runner vector kind %q", vec.Kind)
	}
}

func validateRunnerSpawnRequest(record map[string]interface{}) error {
	if err := knownKeys(record, "runnerSpawnRequest", map[string]bool{
		"schemaVersion":       true,
		"kind":                true,
		"agentId":             true,
		"credential":          true,
		"providerRoute":       true,
		"options":             true,
		"prompt":              true,
		"model":               true,
		"cwd":                 true,
		"taskId":              true,
		"costCeilingMicroUsd": true,
	}); err != nil {
		return err
	}
	if err := validateOptionalSchemaVersion(record, "runnerSpawnRequest.schemaVersion"); err != nil {
		return err
	}
	kind, err := requiredStringValue(record, "kind", "runnerSpawnRequest.kind")
	if err != nil {
		return err
	}
	if !runnerKindSet[kind] {
		return fmt.Errorf("runnerSpawnRequest.kind: unsupported RunnerKind")
	}
	agentID, err := requiredStringValue(record, "agentId", "runnerSpawnRequest.agentId")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(agentID, maxAgentIDBytes, "runnerSpawnRequest.agentId"); err != nil {
		return err
	}
	if !agentIDRE.MatchString(agentID) {
		return fmt.Errorf("runnerSpawnRequest.agentId: not an AgentId")
	}
	credential, err := requiredObjectValue(record, "credential", "runnerSpawnRequest.credential")
	if err != nil {
		return err
	}
	if err := validateCredentialHandle(credential); err != nil {
		return err
	}
	prompt, err := requiredStringValue(record, "prompt", "runnerSpawnRequest.prompt")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(prompt, maxRunnerPromptBytes, "runnerSpawnRequest.prompt"); err != nil {
		return err
	}
	if model, ok, err := optionalString(record, "model", "runnerSpawnRequest.model"); err != nil {
		return err
	} else if ok {
		if err := validateUtf8Budget(model, maxRunnerModelBytes, "runnerSpawnRequest.model"); err != nil {
			return err
		}
	}
	if cwd, ok, err := optionalString(record, "cwd", "runnerSpawnRequest.cwd"); err != nil {
		return err
	} else if ok {
		if err := validateUtf8Budget(cwd, maxRunnerCwdBytes, "runnerSpawnRequest.cwd"); err != nil {
			return err
		}
	}
	if err := optionalMicroUsd(record, "costCeilingMicroUsd", "runnerSpawnRequest.costCeilingMicroUsd", maxSafeInteger); err != nil {
		return err
	}
	if taskID, ok, err := optionalString(record, "taskId", "runnerSpawnRequest.taskId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(taskID) {
		return fmt.Errorf("runnerSpawnRequest.taskId: not a TaskId")
	}
	if routeValue, ok := record["providerRoute"]; ok {
		route, ok := routeValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("runnerSpawnRequest.providerRoute: must be an object")
		}
		if err := validateProviderRoute(route); err != nil {
			return err
		}
	}
	if optionsValue, ok := record["options"]; ok {
		options, ok := optionsValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("runnerSpawnRequest.options: must be an object")
		}
		if err := validateRunnerSpawnOptions(options, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateProviderRoute(record map[string]interface{}) error {
	if err := knownKeys(record, "runnerSpawnRequest.providerRoute", map[string]bool{
		"credentialScope": true,
		"providerKind":    true,
	}); err != nil {
		return err
	}
	scope, err := requiredStringValue(record, "credentialScope", "runnerSpawnRequest.providerRoute.credentialScope")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(scope, maxCredentialScopeBytes, "runnerSpawnRequest.providerRoute.credentialScope"); err != nil {
		return err
	}
	if !credentialScopeSet[scope] {
		return fmt.Errorf("runnerSpawnRequest.providerRoute.credentialScope: unsupported CredentialScope")
	}
	providerKind, err := requiredStringValue(record, "providerKind", "runnerSpawnRequest.providerRoute.providerKind")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(providerKind, maxCredentialScopeBytes, "runnerSpawnRequest.providerRoute.providerKind"); err != nil {
		return err
	}
	if !providerKindSet[providerKind] {
		return fmt.Errorf("runnerSpawnRequest.providerRoute.providerKind: unsupported ProviderKind")
	}
	return nil
}

func validateRunnerSpawnOptions(record map[string]interface{}, requestKind string) error {
	kind, err := requiredStringValue(record, "kind", "runnerSpawnRequest.options.kind")
	if err != nil {
		return err
	}
	if !runnerKindSet[kind] {
		return fmt.Errorf("runnerSpawnRequest.options.kind: unsupported RunnerKind")
	}
	if kind != requestKind {
		return fmt.Errorf("runnerSpawnRequest.options.kind: must match runnerSpawnRequest.kind")
	}
	switch kind {
	case "claude-cli":
		if err := knownKeys(record, "runnerSpawnRequest.options", map[string]bool{
			"kind":      true,
			"extraArgs": true,
		}); err != nil {
			return err
		}
		if extraArgs, ok := record["extraArgs"]; ok {
			items, ok := extraArgs.([]interface{})
			if !ok {
				return fmt.Errorf("runnerSpawnRequest.options.extraArgs: must be an array")
			}
			if len(items) > maxRunnerExtraArgs {
				return fmt.Errorf("runnerSpawnRequest.options.extraArgs: exceeds %d entries", maxRunnerExtraArgs)
			}
			for index, item := range items {
				arg, ok := item.(string)
				if !ok {
					return fmt.Errorf("runnerSpawnRequest.options.extraArgs/%d: must be a string", index)
				}
				if err := validateUtf8Budget(arg, maxRunnerExtraArgBytes, fmt.Sprintf("runnerSpawnRequest.options.extraArgs/%d", index)); err != nil {
					return err
				}
			}
		}
	case "codex-cli":
		if err := knownKeys(record, "runnerSpawnRequest.options", map[string]bool{
			"kind":    true,
			"sandbox": true,
			"profile": true,
		}); err != nil {
			return err
		}
		if sandbox, ok, err := optionalString(record, "sandbox", "runnerSpawnRequest.options.sandbox"); err != nil {
			return err
		} else if ok && sandbox != "read-only" && sandbox != "workspace-write" {
			return fmt.Errorf("runnerSpawnRequest.options.sandbox: unsupported codex sandbox")
		}
		if profile, ok, err := optionalString(record, "profile", "runnerSpawnRequest.options.profile"); err != nil {
			return err
		} else if ok {
			if err := validateUtf8Budget(profile, maxRunnerProfileBytes, "runnerSpawnRequest.options.profile"); err != nil {
				return err
			}
		}
	case "openai-compat":
		if err := knownKeys(record, "runnerSpawnRequest.options", map[string]bool{
			"kind":      true,
			"endpoint":  true,
			"headers":   true,
			"timeoutMs": true,
		}); err != nil {
			return err
		}
		endpoint, err := requiredStringValue(record, "endpoint", "runnerSpawnRequest.options.endpoint")
		if err != nil {
			return err
		}
		if err := validateUtf8Budget(endpoint, maxRunnerEndpointBytes, "runnerSpawnRequest.options.endpoint"); err != nil {
			return err
		}
		if headersValue, ok := record["headers"]; ok {
			headers, ok := headersValue.(map[string]interface{})
			if !ok {
				return fmt.Errorf("runnerSpawnRequest.options.headers: must be an object")
			}
			if len(headers) > maxRunnerOptionHeaders {
				return fmt.Errorf("runnerSpawnRequest.options.headers: exceeds %d headers", maxRunnerOptionHeaders)
			}
			for key, value := range headers {
				if err := validateUtf8Budget(key, maxRunnerOptionHeaderNameBytes, fmt.Sprintf("runnerSpawnRequest.options.headers/%s name", key)); err != nil {
					return err
				}
				headerValue, ok := value.(string)
				if !ok {
					return fmt.Errorf("runnerSpawnRequest.options.headers/%s: must be a string", key)
				}
				if err := validateUtf8Budget(headerValue, maxRunnerOptionHeaderValueBytes, fmt.Sprintf("runnerSpawnRequest.options.headers/%s value", key)); err != nil {
					return err
				}
			}
		}
		if timeoutMs, ok := record["timeoutMs"]; ok {
			number, ok := timeoutMs.(float64)
			if !ok || number != float64(int(number)) || number <= 0 {
				return fmt.Errorf("runnerSpawnRequest.options.timeoutMs: must be a positive safe integer")
			}
		}
	default:
		return fmt.Errorf("runnerSpawnRequest.options.kind: unsupported RunnerKind")
	}
	return nil
}

func validateRunnerEvent(record map[string]interface{}) error {
	kind, err := requiredStringValue(record, "kind", "runnerEvent.kind")
	if err != nil {
		return err
	}
	keys := map[string]bool{
		"schemaVersion": true,
		"kind":          true,
		"runnerId":      true,
		"at":            true,
	}
	switch kind {
	case "started":
	case "stdout", "stderr":
		keys["chunk"] = true
	case "cost":
		keys["entry"] = true
	case "receipt":
		keys["receiptId"] = true
	case "finished":
		keys["exitCode"] = true
	case "failed":
		keys["error"] = true
		keys["code"] = true
	default:
		return fmt.Errorf("runnerEvent.kind: unsupported RunnerEvent kind")
	}
	if err := knownKeys(record, "runnerEvent", keys); err != nil {
		return err
	}
	if err := validateOptionalSchemaVersion(record, "runnerEvent.schemaVersion"); err != nil {
		return err
	}
	runnerID, err := requiredStringValue(record, "runnerId", "runnerEvent.runnerId")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(runnerID, maxRunnerIDBytes, "runnerEvent.runnerId"); err != nil {
		return err
	}
	if !runnerIDRE.MatchString(runnerID) {
		return fmt.Errorf("runnerEvent.runnerId: not a RunnerId")
	}
	at, err := requiredStringValue(record, "at", "runnerEvent.at")
	if err != nil {
		return err
	}
	if err := validateCanonicalTimestamp(at, "runnerEvent.at"); err != nil {
		return err
	}
	switch kind {
	case "stdout", "stderr":
		chunk, err := requiredStringValue(record, "chunk", "runnerEvent.chunk")
		if err != nil {
			return err
		}
		return validateUtf8Budget(chunk, maxRunnerStdioChunkBytes, "runnerEvent.chunk")
	case "cost":
		entry, err := requiredObjectValue(record, "entry", "runnerEvent.entry")
		if err != nil {
			return err
		}
		return validateCostEventEntry(entry)
	case "receipt":
		receiptID, err := requiredStringValue(record, "receiptId", "runnerEvent.receiptId")
		if err != nil {
			return err
		}
		if !ulidRE.MatchString(receiptID) {
			return fmt.Errorf("runnerEvent.receiptId: not a ReceiptId")
		}
	case "finished":
		exitCode, ok := record["exitCode"].(float64)
		if !ok || exitCode != float64(int(exitCode)) || exitCode < 0 || exitCode > 255 {
			return fmt.Errorf("runnerEvent.exitCode: must be an integer from 0 to 255")
		}
	case "failed":
		errorMessage, err := requiredStringValue(record, "error", "runnerEvent.error")
		if err != nil {
			return err
		}
		if err := validateUtf8Budget(errorMessage, maxRunnerErrorBytes, "runnerEvent.error"); err != nil {
			return err
		}
		if code, ok, err := optionalString(record, "code", "runnerEvent.code"); err != nil {
			return err
		} else if ok && !runnerFailureCodeSet[code] {
			return fmt.Errorf("runnerEvent.code: unsupported RunnerFailureCode")
		}
	}
	return nil
}

func validateCredentialHandle(record map[string]interface{}) error {
	if err := knownKeys(record, "credentialHandle", map[string]bool{
		"version": true,
		"id":      true,
	}); err != nil {
		return err
	}
	version, ok := record["version"].(float64)
	if !ok || version != 1 {
		return fmt.Errorf("credentialHandle.version: must be 1")
	}
	id, err := requiredStringValue(record, "id", "credentialHandle.id")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(id, maxCredentialHandleBytes, "credentialHandle.id"); err != nil {
		return err
	}
	if !credentialHandleRE.MatchString(id) {
		return fmt.Errorf("credentialHandle.id: not a CredentialHandleId")
	}
	return nil
}

func validateCostEventEntry(record map[string]interface{}) error {
	if err := knownKeys(record, "runnerEvent.entry", map[string]bool{
		"receiptId":      true,
		"agentSlug":      true,
		"taskId":         true,
		"providerKind":   true,
		"model":          true,
		"amountMicroUsd": true,
		"units":          true,
		"occurredAt":     true,
	}); err != nil {
		return err
	}
	if receiptID, ok, err := optionalString(record, "receiptId", "runnerEvent.entry.receiptId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(receiptID) {
		return fmt.Errorf("runnerEvent.entry.receiptId: not a ReceiptId")
	}
	agentSlug, err := requiredStringValue(record, "agentSlug", "runnerEvent.entry.agentSlug")
	if err != nil {
		return err
	}
	if err := validateUtf8Budget(agentSlug, maxAgentIDBytes, "runnerEvent.entry.agentSlug"); err != nil {
		return err
	}
	if !agentIDRE.MatchString(agentSlug) {
		return fmt.Errorf("runnerEvent.entry.agentSlug: not an AgentSlug")
	}
	if taskID, ok, err := optionalString(record, "taskId", "runnerEvent.entry.taskId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(taskID) {
		return fmt.Errorf("runnerEvent.entry.taskId: not a TaskId")
	}
	providerKind, err := requiredStringValue(record, "providerKind", "runnerEvent.entry.providerKind")
	if err != nil {
		return err
	}
	if !providerKindSet[providerKind] {
		return fmt.Errorf("runnerEvent.entry.providerKind: unsupported ProviderKind")
	}
	model, err := requiredStringValue(record, "model", "runnerEvent.entry.model")
	if err != nil {
		return err
	}
	if len(model) == 0 {
		return fmt.Errorf("runnerEvent.entry.model: must be a non-empty string")
	}
	if err := validateUtf8Budget(model, maxCostModelBytes, "runnerEvent.entry.model"); err != nil {
		return err
	}
	if err := requiredNonNegativeInteger(record, "amountMicroUsd", "runnerEvent.entry.amountMicroUsd", maxCostEventAmountMicroUsd); err != nil {
		return err
	}
	units, err := requiredObjectValue(record, "units", "runnerEvent.entry.units")
	if err != nil {
		return err
	}
	if err := validateCostUnits(units); err != nil {
		return err
	}
	occurredAt, err := requiredStringValue(record, "occurredAt", "runnerEvent.entry.occurredAt")
	if err != nil {
		return err
	}
	return validateCanonicalTimestamp(occurredAt, "runnerEvent.entry.occurredAt")
}

func validateCostUnits(record map[string]interface{}) error {
	if err := knownKeys(record, "runnerEvent.entry.units", map[string]bool{
		"inputTokens":         true,
		"outputTokens":        true,
		"cacheReadTokens":     true,
		"cacheCreationTokens": true,
	}); err != nil {
		return err
	}
	for _, key := range []string{"inputTokens", "outputTokens", "cacheReadTokens", "cacheCreationTokens"} {
		if err := requiredNonNegativeInteger(record, key, "runnerEvent.entry.units."+key, maxSafeInteger); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalSchemaVersion(record map[string]interface{}, path string) error {
	value, ok := record["schemaVersion"]
	if !ok {
		return nil
	}
	version, ok := value.(float64)
	if !ok || version != float64(int(version)) {
		return fmt.Errorf("%s: must be an integer", path)
	}
	if version > 1 {
		return fmt.Errorf("%s: unsupported schemaVersion", path)
	}
	if version != 1 {
		return fmt.Errorf("%s: must be 1", path)
	}
	return nil
}

func knownKeys(record map[string]interface{}, path string, allowed map[string]bool) error {
	for key := range record {
		if !allowed[key] {
			return fmt.Errorf("%s/%s: is not allowed", path, key)
		}
	}
	return nil
}

func decodeStrictJSON(raw json.RawMessage, path string, out interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if field, ok := unknownJSONField(err); ok {
			return fmt.Errorf("%s/%s: is not allowed", path, field)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func unknownJSONField(err error) (string, bool) {
	message := err.Error()
	const prefix = "json: unknown field "
	if !strings.HasPrefix(message, prefix) {
		return "", false
	}
	field := strings.TrimPrefix(message, prefix)
	field = strings.Trim(field, `"`)
	if field == "" {
		return "", false
	}
	return field, true
}

func requiredRawString(raw json.RawMessage, path string) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("%s: is required", path)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s: must be a string", path)
	}
	return value, nil
}

func requiredRawArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s: is required", path)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	if value == nil {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	return value, nil
}

func requiredStringValue(record map[string]interface{}, key string, path string) (string, error) {
	value, ok := record[key]
	if !ok {
		return "", fmt.Errorf("%s: is required", path)
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s: must be a string", path)
	}
	return stringValue, nil
}

func optionalString(record map[string]interface{}, key string, path string) (string, bool, error) {
	value, ok := record[key]
	if !ok {
		return "", false, nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s: must be a string", path)
	}
	return stringValue, true, nil
}

func optionalStringValue(record map[string]interface{}, key string, path string) error {
	_, _, err := optionalString(record, key, path)
	return err
}

func optionalMicroUsd(record map[string]interface{}, key string, path string, max float64) error {
	if _, ok := record[key]; !ok {
		return nil
	}
	if err := requiredNonNegativeInteger(record, key, path, max); err != nil {
		return fmt.Errorf("%s: not a MicroUsd: %w", path, err)
	}
	return nil
}

func requiredExactNumber(record map[string]interface{}, key string, path string, expected int64) error {
	value, err := requiredSafeInteger(record, key, path)
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("%s: must be %d", path, expected)
	}
	return nil
}

func requiredIntegerInRange(record map[string]interface{}, key string, path string, min int64, max int64) error {
	value, err := requiredSafeInteger(record, key, path)
	if err != nil {
		return err
	}
	if value < min || value > max {
		return fmt.Errorf("%s: must be an integer in %d..%d", path, min, max)
	}
	return nil
}

func requiredSafeInteger(record map[string]interface{}, key string, path string) (int64, error) {
	value, ok := record[key]
	if !ok {
		return 0, fmt.Errorf("%s: is required", path)
	}
	numberValue, ok := value.(float64)
	if !ok || numberValue != float64(int64(numberValue)) || numberValue < 0 || numberValue > maxSafeInteger {
		return 0, fmt.Errorf("%s: must be a non-negative safe integer", path)
	}
	return int64(numberValue), nil
}

func requiredNonNegativeInteger(record map[string]interface{}, key string, path string, max float64) error {
	value, ok := record[key]
	if !ok {
		return fmt.Errorf("%s: is required", path)
	}
	numberValue, ok := value.(float64)
	if !ok || numberValue != float64(int64(numberValue)) || numberValue < 0 || numberValue > maxSafeInteger {
		return fmt.Errorf("%s: must be a non-negative safe integer", path)
	}
	if numberValue > max {
		return fmt.Errorf("%s: exceeds maximum %0.f", path, max)
	}
	return nil
}

func requiredObjectValue(record map[string]interface{}, key string, path string) (map[string]interface{}, error) {
	value, ok := record[key]
	if !ok {
		return nil, fmt.Errorf("%s: is required", path)
	}
	objectValue, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: must be an object", path)
	}
	return objectValue, nil
}

func validateUtf8Budget(value string, maxBytes int, path string) error {
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s: exceeds %d UTF-8 bytes", path, maxBytes)
	}
	return nil
}

func validateCanonicalTimestamp(value string, path string) error {
	if !isoUtcRE.MatchString(value) {
		return fmt.Errorf("%s: must be an ISO8601 UTC millisecond timestamp", path)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("%s: must be a valid ISO8601 UTC millisecond timestamp", path)
	}
	if parsed.UTC().Format("2006-01-02T15:04:05.000Z") != value {
		return fmt.Errorf("%s: must be a valid ISO8601 UTC millisecond timestamp", path)
	}
	return nil
}

func errorCategory(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "schemaVersion"):
		return "schema_version"
	case strings.Contains(message, "is not allowed"):
		return "unknown_key"
	case strings.Contains(message, "exceeds"):
		return "budget"
	case strings.Contains(message, "ISO8601"):
		return "timestamp"
	case strings.Contains(message, "costCeilingMicroUsd"):
		return "micro_usd"
	case strings.Contains(message, "RunnerFailureCode"):
		return "failure_code"
	case strings.Contains(message, "runnerEvent.entry"):
		return "cost_entry"
	default:
		return "validation"
	}
}

const (
	colorReset = "\x1b[0m"
	colorGreen = "\x1b[32m"
	colorRed   = "\x1b[31m"
	colorDim   = "\x1b[2m"
	colorBold  = "\x1b[1m"
)

// ─── moat (SanitizedString allowlist) frozen code-point table ───────────────
//
// moat-disallowed-table.json pins the Unicode \p{C} +
// \p{Default_Ignorable_Code_Point} union the `allowlist` policy rejects. The
// TypeScript sanitizer binary-searches an embedded copy of disallowedRanges;
// this Go reference loads the same JSON and binary-searches it independently.
// If both agree on every classificationVectors entry, the moat boundary is
// genuinely cross-language portable and not coupled to one runtime's Unicode
// data.

type moatVector struct {
	CodePoint  int    `json:"codePoint"`
	Name       string `json:"name"`
	Disallowed bool   `json:"disallowed"`
}

type moatTableFixture struct {
	Description           string       `json:"description"`
	UnicodeVersion        string       `json:"unicodeVersion"`
	GeneratedBy           string       `json:"generatedBy"`
	DisallowedRanges      [][2]int     `json:"disallowedRanges"`
	ClassificationVectors []moatVector `json:"classificationVectors"`
}

func loadMoatTableFixture() (moatTableFixture, error) {
	fixtureBytes, err := os.ReadFile("moat-disallowed-table.json")
	if err != nil {
		return moatTableFixture{}, err
	}
	var fx moatTableFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return moatTableFixture{}, err
	}
	return fx, nil
}

// moatDisallowed binary-searches the sorted, non-overlapping ranges — the same
// algorithm isMoatDisallowedCodePoint uses in sanitized-string.ts.
func moatDisallowed(codePoint int, ranges [][2]int) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		r := ranges[mid]
		switch {
		case codePoint < r[0]:
			hi = mid - 1
		case codePoint > r[1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

func verifyMoatTable(fx moatTableFixture) int {
	failed := 0
	if len(fx.ClassificationVectors) == 0 {
		// Fail closed — with no vectors the classification cross-check below is
		// vacuous. verifyNfkcTable does the same for its vectors.
		fmt.Printf("  %sFAIL%s moat-table: no classification vectors\n", colorRed, colorReset)
		return 1
	}

	// The ranges must be sorted, non-overlapping, and non-adjacent or the
	// binary search above is unsound.
	previousEnd := -2
	wellFormed := true
	for _, r := range fx.DisallowedRanges {
		if r[0] > r[1] || r[0] <= previousEnd+1 {
			fmt.Printf("  %sFAIL%s moat-table: malformed range [%d, %d] after %d\n",
				colorRed, colorReset, r[0], r[1], previousEnd)
			failed++
			wellFormed = false
			break
		}
		previousEnd = r[1]
	}
	if wellFormed {
		fmt.Printf("  %sPASS%s moat-table %d ranges sorted, non-overlapping, non-adjacent\n",
			colorGreen, colorReset, len(fx.DisallowedRanges))
	}

	for _, vec := range fx.ClassificationVectors {
		fromTable := moatDisallowed(vec.CodePoint, fx.DisallowedRanges)
		if fromTable != vec.Disallowed {
			fmt.Printf("  %sFAIL%s moat-table/%s: JSON range table disagrees on disallowed=%t\n",
				colorRed, colorReset, vec.Name, vec.Disallowed)
			failed++
			continue
		}
		// Independent cross-check: classify the same code point with Go's own
		// Unicode data (unicode.In + the pinned Default_Ignorable table) rather
		// than re-reading the JSON. The JSON table is raw \p{C}∪\p{DI}, so add
		// back tab/LF/CR, which isAllowlistDisallowedCodePoint carves out.
		//
		// Skip this for unassigned (Cn) vectors: a future Go release could
		// assign one, flipping its unicode.Cn membership and breaking the
		// cross-check even though the frozen JSON table is unchanged. The
		// JSON-range check above still covers them.
		if !strings.Contains(vec.Name, "unassigned") {
			r := rune(vec.CodePoint)
			native := isAllowlistDisallowedCodePoint(r) || r == '\t' || r == '\n' || r == '\r'
			if native != vec.Disallowed {
				fmt.Printf("  %sFAIL%s moat-table/%s: Go-native Unicode classification disagrees on disallowed=%t\n",
					colorRed, colorReset, vec.Name, vec.Disallowed)
				failed++
				continue
			}
		}
		fmt.Printf("  %sPASS%s moat-table/%s\n", colorGreen, colorReset, vec.Name)
	}
	return failed
}

// verifyNfkcTable runs the frozen NFKC normaliser over the curated corpus in
// nfkc-table.json. Each `expected` was frozen at table-generation time from
// the pinned Unicode 15.1 runtime; tests/nfkc.spec.ts checks the same corpus,
// so the TypeScript normaliser and this Go reference are pinned to one oracle.
func verifyNfkcTable(fx nfkcTableFixture, tables nfkcTables) int {
	failed := 0
	if len(fx.NormalizationVectors) == 0 {
		fmt.Printf("  %sFAIL%s nfkc-table: no normalization vectors\n", colorRed, colorReset)
		return 1
	}
	for _, vec := range fx.NormalizationVectors {
		got := tables.frozenNFKC(vec.Input)
		if got != vec.Expected {
			fmt.Printf("  %sFAIL%s nfkc/%s: frozen NFKC mismatch\n", colorRed, colorReset, vec.Name)
			fmt.Printf("    expected: %q\n", vec.Expected)
			fmt.Printf("    actual:   %q\n", got)
			failed++
			continue
		}
		// Idempotence — re-normalising the output must be a no-op.
		if again := tables.frozenNFKC(got); again != got {
			fmt.Printf("  %sFAIL%s nfkc/%s: not idempotent\n", colorRed, colorReset, vec.Name)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s nfkc/%s\n", colorGreen, colorReset, vec.Name)
	}
	return failed
}

func main() {
	fixtureBytes, err := os.ReadFile("audit-event-vectors.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read fixture: %v\n", err)
		fmt.Fprintln(os.Stderr, "run from packages/protocol/testdata/")
		os.Exit(2)
	}

	var fx fixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		fmt.Fprintf(os.Stderr, "could not parse fixture: %v\n", err)
		os.Exit(2)
	}
	if fx.SchemaVersion != 1 {
		fmt.Fprintf(os.Stderr, "unsupported fixture schemaVersion: %d\n", fx.SchemaVersion)
		os.Exit(2)
	}
	runnerFx, err := loadRunnerFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load runner fixture: %v\n", err)
		os.Exit(2)
	}
	agentProviderRoutingFx, err := loadAgentProviderRoutingFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load agent-provider-routing fixture: %v\n", err)
		os.Exit(2)
	}
	signedApprovalTokenFx, err := loadSignedApprovalTokenFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load signed-approval-token fixture: %v\n", err)
		os.Exit(2)
	}
	moatFx, err := loadMoatTableFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load moat-disallowed-table fixture: %v\n", err)
		os.Exit(2)
	}
	nfkcFx, err := loadNfkcTableFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load nfkc-table fixture: %v\n", err)
		os.Exit(2)
	}
	if nfkcFx.SchemaVersion != 1 {
		fmt.Fprintf(os.Stderr, "unsupported nfkc-table schemaVersion: %d\n", nfkcFx.SchemaVersion)
		os.Exit(2)
	}
	if nfkcFx.UnicodeVersion != "15.1" {
		fmt.Fprintf(os.Stderr, "unexpected nfkc-table unicodeVersion: %s\n", nfkcFx.UnicodeVersion)
		os.Exit(2)
	}
	// Loaded before any sanitizeAllowlistText call below — it normalises via
	// frozenNfkcTables and strips against frozenMoatDisallowedRanges.
	frozenNfkcTables = buildNfkcTables(nfkcFx)
	frozenMoatDisallowedRanges = moatFx.DisallowedRanges

	fmt.Printf("%s@wuphf/protocol — Go reference verifier%s\n", colorBold, colorReset)
	fmt.Printf("%sLoaded fixture schemaVersion=%d, %d audit-event vectors, %d merkle-root vectors, %d signed-approval-token accept vectors, %d signed-approval-token reject vectors, %d runner accept vectors, %d runner reject vectors, %d agent-provider-routing accept vectors, %d agent-provider-routing reject vectors, moat table Unicode %s (%d ranges, %d vectors), nfkc table Unicode %s (%d normalization vectors)%s\n\n",
		colorDim, fx.SchemaVersion, len(fx.Vectors), len(fx.MerkleRootVectors), len(signedApprovalTokenFx.Accepted), len(signedApprovalTokenFx.Rejected), len(runnerFx.Vectors), len(runnerFx.RejectVectors), len(agentProviderRoutingFx.Accepted), len(agentProviderRoutingFx.Rejected), moatFx.UnicodeVersion, len(moatFx.DisallowedRanges), len(moatFx.ClassificationVectors), nfkcFx.UnicodeVersion, len(nfkcFx.NormalizationVectors), colorReset)

	failed := 0

	for _, vec := range fx.Vectors {
		if vec.SerializedJSON != "" && vec.SerializedJSON != vec.Expected.CanonicalSerialization {
			fmt.Printf("  %sFAIL%s %s: serializedJson does not match expected.canonicalSerialization\n", colorRed, colorReset, vec.Name)
			failed++
			continue
		}
		if vec.ExpectedHash != "" && vec.ExpectedHash != vec.Expected.EventHash {
			fmt.Printf("  %sFAIL%s %s: expectedHash does not match expected.eventHash\n", colorRed, colorReset, vec.Name)
			failed++
			continue
		}
		serialized, err := serializeAuditEventRecordForHash(vec.Input)
		if err != nil {
			fmt.Printf("  %sFAIL%s %s: serialization error: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		if string(serialized) != vec.Expected.CanonicalSerialization {
			fmt.Printf("  %sFAIL%s %s: canonicalSerialization mismatch\n", colorRed, colorReset, vec.Name)
			fmt.Printf("    expected: %s\n", vec.Expected.CanonicalSerialization)
			fmt.Printf("    actual:   %s\n", string(serialized))
			failed++
			continue
		}
		actualHash := computeEventHash(vec.Input.PrevHash, serialized)
		if actualHash != vec.Expected.EventHash {
			fmt.Printf("  %sFAIL%s %s: eventHash mismatch\n", colorRed, colorReset, vec.Name)
			fmt.Printf("    expected: %s\n", vec.Expected.EventHash)
			fmt.Printf("    actual:   %s\n", actualHash)
			failed++
			continue
		}
		// For cost-payload kinds, also confirm the bodyB64 itself is canonical
		// JSON: an independent canonicalizer must reproduce identical bytes.
		// This locks the body-bytes contract that the audit chain relies on.
		if costPayloadKindSet[vec.Input.Payload.Kind] {
			decoded, canonical, err := canonicalizeBodyBytes(vec.Input.Payload.BodyB64)
			if err != nil {
				fmt.Printf("  %sFAIL%s %s: body canonicalize error: %v\n",
					colorRed, colorReset, vec.Name, err)
				failed++
				continue
			}
			if !bytes.Equal(decoded, canonical) {
				fmt.Printf("  %sFAIL%s %s: body bytes are not canonical JCS\n",
					colorRed, colorReset, vec.Name)
				fmt.Printf("    decoded:   %s\n", string(decoded))
				fmt.Printf("    canonical: %s\n", string(canonical))
				failed++
				continue
			}
		}
		fmt.Printf("  %sPASS%s audit-event/%s eventHash=%s%s%s\n",
			colorGreen, colorReset, vec.Name, colorDim, actualHash[:16]+"…", colorReset)
	}

	for _, vec := range fx.MerkleRootVectors {
		canonical, err := canonicalMerkleRoot(vec.Input)
		if err != nil {
			fmt.Printf("  %sFAIL%s merkle/%s: canonicalize error: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		if string(canonical) != vec.Expected.CanonicalJSON {
			fmt.Printf("  %sFAIL%s merkle/%s: canonicalJson mismatch\n", colorRed, colorReset, vec.Name)
			fmt.Printf("    expected: %s\n", vec.Expected.CanonicalJSON)
			fmt.Printf("    actual:   %s\n", string(canonical))
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s merkle/%s canonicalJson matches (%d bytes)%s\n",
			colorGreen, colorReset, vec.Name, len(canonical), colorReset)
	}

	for _, vec := range runnerFx.Vectors {
		if err := validateRunnerVector(vec); err != nil {
			fmt.Printf("  %sFAIL%s runner/%s: expected accept, got %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s runner/%s accepted\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range runnerFx.RejectVectors {
		if err := validateRunnerVector(vec); err == nil {
			fmt.Printf("  %sFAIL%s runner/%s: expected reject, got accept\n", colorRed, colorReset, vec.Name)
			failed++
			continue
		} else if vec.ErrorCategory != "" && errorCategory(err) != vec.ErrorCategory {
			fmt.Printf("  %sFAIL%s runner/%s: expected error_category=%s, got %s (%v)\n",
				colorRed, colorReset, vec.Name, vec.ErrorCategory, errorCategory(err), err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s runner/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range agentProviderRoutingFx.Accepted {
		if err := validateAgentProviderRoutingAccepted(vec); err != nil {
			fmt.Printf("  %sFAIL%s agent-provider-routing/%s: expected accept, got %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s agent-provider-routing/%s accepted\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range agentProviderRoutingFx.Rejected {
		if err := validateAgentProviderRoutingRejected(vec); err != nil {
			fmt.Printf("  %sFAIL%s agent-provider-routing/%s: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s agent-provider-routing/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range signedApprovalTokenFx.Accepted {
		if err := validateSignedApprovalTokenAccepted(vec); err != nil {
			fmt.Printf("  %sFAIL%s signed-approval-token/%s: expected accept, got %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s signed-approval-token/%s accepted\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range signedApprovalTokenFx.Rejected {
		if err := validateSignedApprovalTokenRejected(vec); err != nil {
			fmt.Printf("  %sFAIL%s signed-approval-token/%s: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s signed-approval-token/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	failed += verifyMoatTable(moatFx)
	failed += verifyNfkcTable(nfkcFx, frozenNfkcTables)

	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%sAll %d vectors match — wire contract is cross-language portable.%s\n",
			colorBold, colorGreen, len(fx.Vectors)+len(fx.MerkleRootVectors)+len(signedApprovalTokenFx.Accepted)+len(signedApprovalTokenFx.Rejected)+len(runnerFx.Vectors)+len(runnerFx.RejectVectors)+len(agentProviderRoutingFx.Accepted)+len(agentProviderRoutingFx.Rejected)+len(moatFx.ClassificationVectors)+len(nfkcFx.NormalizationVectors), colorReset)
		os.Exit(0)
	}
	fmt.Printf("%s%s%d vector(s) failed — TypeScript writer and Go reference disagree.%s\n",
		colorBold, colorRed, failed, colorReset)
	os.Exit(1)
}

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
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
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

type approvalRequestExpected struct {
	CanonicalSerialization string `json:"canonicalSerialization"`
}

type approvalRequestAcceptedVector struct {
	Name     string                  `json:"name"`
	Input    json.RawMessage         `json:"input"`
	Expected approvalRequestExpected `json:"expected"`
}

type approvalRequestRejectedVector struct {
	Name          string          `json:"name"`
	Input         json.RawMessage `json:"input"`
	ExpectedError string          `json:"expectedError"`
}

type approvalRequestFixture struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Comment       string                          `json:"comment"`
	Accepted      []approvalRequestAcceptedVector `json:"accepted"`
	Rejected      []approvalRequestRejectedVector `json:"rejected"`
}

type routeEnvelopeExpected struct {
	CanonicalSerialization string `json:"canonicalSerialization"`
}

type routeEnvelopeAcceptedVector struct {
	Name     string                `json:"name"`
	Codec    string                `json:"codec"`
	Input    json.RawMessage       `json:"input"`
	Expected routeEnvelopeExpected `json:"expected"`
}

type routeEnvelopeRejectedVector struct {
	Name          string          `json:"name"`
	Codec         string          `json:"codec"`
	Input         json.RawMessage `json:"input"`
	ExpectedError string          `json:"expectedError"`
}

type routeEnvelopeFixture struct {
	SchemaVersion int                           `json:"schemaVersion"`
	Comment       string                        `json:"comment"`
	Accepted      []routeEnvelopeAcceptedVector `json:"accepted"`
	Rejected      []routeEnvelopeRejectedVector `json:"rejected"`
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

// canonicalJsonPayloadKindSet identifies bodyB64 kinds whose payload itself is
// expected to be canonical JSON (RFC 8785 JCS). The TS writer guarantees the
// family-specific `*AuditPayloadToBytes` helpers emit canonical bytes; this
// verifier independently decodes, re-parses, and re-canonicalizes the body to
// confirm a non-TS implementation produces the same bytes.
var canonicalJsonPayloadKindSet = map[string]bool{
	"approval_requested":       true,
	"approval_decided":         true,
	"cost_event":               true,
	"budget_set":               true,
	"budget_threshold_crossed": true,
	"thread_created":           true,
	"thread_spec_edited":       true,
	"thread_status_changed":    true,
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
	idempotencyKeyRE   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
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

var approvalRequestStatusSet = map[string]bool{
	"pending":   true,
	"approved":  true,
	"rejected":  true,
	"abstained": true,
}

var approvalDecisionSet = map[string]bool{
	"approve": true,
	"reject":  true,
	"abstain": true,
}

var riskClassSet = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

var threadStatusSet = map[string]bool{
	"open":         true,
	"in_progress":  true,
	"needs_review": true,
	"merged":       true,
	"closed":       true,
}

var threadEffectiveStatusSet = map[string]bool{
	"open":            true,
	"in_progress":     true,
	"needs_review":    true,
	"needs_attention": true,
	"merged":          true,
	"closed":          true,
}

var threadAttentionReasonSet = map[string]bool{
	"pending_approval": true,
	"failed":           true,
	"stalled":          true,
}

var threadBoardColumnSet = map[string]bool{
	"running":  true,
	"review":   true,
	"needs_me": true,
	"done":     true,
}

var threadCurrentSeatSet = map[string]bool{
	"agent": true,
	"human": true,
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
	maxThreadTitleBytes             = 512
	maxThreadSpecContentBytes       = 64 * 1024
	maxThreadExternalRefs           = 32
	maxThreadExternalRefBytes       = 2 * 1024
	maxThreadTaskIDs                = 1024
	maxRouteThreadListItems         = 256
	maxRouteApprovalListItems       = 256
	maxRouteErrorCodeBytes          = 128
	maxRouteErrorMessageBytes       = 8 * 1024
	maxRouteCursorBytes             = 1024
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

func loadApprovalRequestFixture() (approvalRequestFixture, error) {
	fixtureBytes, err := os.ReadFile("approval-request-vectors.json")
	if err != nil {
		return approvalRequestFixture{}, err
	}
	var fx approvalRequestFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return approvalRequestFixture{}, err
	}
	if fx.SchemaVersion != 1 {
		return approvalRequestFixture{}, fmt.Errorf("unsupported approval-request fixture schemaVersion: %d", fx.SchemaVersion)
	}
	return fx, nil
}

func loadRouteEnvelopeFixture() (routeEnvelopeFixture, error) {
	fixtureBytes, err := os.ReadFile("route-envelope-vectors.json")
	if err != nil {
		return routeEnvelopeFixture{}, err
	}
	var fx routeEnvelopeFixture
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fx); err != nil {
		return routeEnvelopeFixture{}, err
	}
	if fx.SchemaVersion != 1 {
		return routeEnvelopeFixture{}, fmt.Errorf("unsupported route-envelope fixture schemaVersion: %d", fx.SchemaVersion)
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

func validateApprovalRequestAccepted(vec approvalRequestAcceptedVector) error {
	parsed, err := parseApprovalRequest(vec.Input)
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

func validateApprovalRequestRejected(vec approvalRequestRejectedVector) error {
	_, err := parseApprovalRequest(vec.Input)
	if err == nil {
		return fmt.Errorf("expected reject, got accept")
	}
	if vec.ExpectedError != "" && !strings.Contains(err.Error(), vec.ExpectedError) {
		return fmt.Errorf("expected error containing %q, got %q", vec.ExpectedError, err.Error())
	}
	return nil
}

func validateRouteEnvelopeAccepted(vec routeEnvelopeAcceptedVector) error {
	parsed, err := parseRouteEnvelope(vec.Codec, vec.Input)
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

func validateRouteEnvelopeRejected(vec routeEnvelopeRejectedVector) error {
	_, err := parseRouteEnvelope(vec.Codec, vec.Input)
	if err == nil {
		return fmt.Errorf("expected reject, got accept")
	}
	if vec.ExpectedError != "" && !strings.Contains(err.Error(), vec.ExpectedError) {
		return fmt.Errorf("expected error containing %q, got %q", vec.ExpectedError, err.Error())
	}
	return nil
}

func parseRouteEnvelope(codec string, raw json.RawMessage) (map[string]interface{}, error) {
	record, err := parseRouteRecord(raw, codec)
	if err != nil {
		return nil, err
	}
	switch codec {
	case "threadCreateRequest":
		return record, validateThreadCreateRequest(record)
	case "threadSpecEditRequest":
		return record, validateThreadSpecEditRequest(record)
	case "threadStatusChangeRequest":
		return record, validateThreadStatusChangeRequest(record)
	case "threadMutationResponse":
		return record, validateThreadMutationResponse(record)
	case "threadListResponse":
		return record, validateThreadListResponse(record)
	case "threadGetResponse":
		return record, validateThreadGetResponse(record)
	case "approvalRequestCreateRequest":
		return record, validateApprovalRequestCreateRequest(record)
	case "approvalDecisionRequest":
		return record, validateApprovalDecisionRequest(record)
	case "approvalRequestCreateResponse":
		return record, validateApprovalRequestEnvelopeResponse(record, "approvalRequestCreateResponse")
	case "approvalDecisionResponse":
		return record, validateApprovalRequestEnvelopeResponse(record, "approvalDecisionResponse")
	case "approvalView":
		return record, validateApprovalViewRecord(record, "approvalView")
	case "approvalListResponse":
		return record, validateApprovalListResponse(record)
	case "approvalGetResponse":
		return record, validateApprovalGetResponse(record)
	case "threadPinnedApprovalsResponse":
		return record, validateThreadPinnedApprovalsResponse(record)
	case "routeError":
		return record, validateRouteErrorEnvelope(record)
	default:
		return nil, fmt.Errorf("routeEnvelope: unsupported codec %q", codec)
	}
}

func parseRouteRecord(raw json.RawMessage, path string) (map[string]interface{}, error) {
	var record map[string]interface{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("%s: must be an object: %w", path, err)
	}
	if record == nil {
		return nil, fmt.Errorf("%s: must be an object", path)
	}
	return record, nil
}

func validateThreadCreateRequest(record map[string]interface{}) error {
	const path = "threadCreateRequest"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":  true,
		"title":          true,
		"specContent":    true,
		"externalRefs":   true,
		"idempotencyKey": true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	title, err := requiredStringValue(record, "title", path+".title")
	if err != nil {
		return err
	}
	if title == "" {
		return fmt.Errorf("%s.title: must be a non-empty string", path)
	}
	if err := validateUtf8Budget(title, maxThreadTitleBytes, "Thread.title bytes"); err != nil {
		return fmt.Errorf("%s.title: %w", path, err)
	}
	if content, ok := record["specContent"]; !ok {
		return fmt.Errorf("%s.specContent: is required", path)
	} else if err := validateThreadSpecContent(content, path+".specContent"); err != nil {
		return err
	}
	if externalRefs, ok := record["externalRefs"]; ok {
		refRecord, ok := externalRefs.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.externalRefs: must be an object", path)
		}
		if err := validateThreadExternalRefsRecord(refRecord, path+".externalRefs"); err != nil {
			return err
		}
	}
	return validateIdempotencyKeyField(record, "idempotencyKey", path+".idempotencyKey")
}

func validateThreadSpecEditRequest(record map[string]interface{}) error {
	const path = "threadSpecEditRequest"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":   true,
		"baseRevisionId":  true,
		"baseContentHash": true,
		"content":         true,
		"idempotencyKey":  true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	if err := validateULIDField(record, "baseRevisionId", path+".baseRevisionId", "ThreadSpecRevisionId"); err != nil {
		return err
	}
	if err := validateSha256HexField(record, "baseContentHash", path+".baseContentHash"); err != nil {
		return err
	}
	if content, ok := record["content"]; !ok {
		return fmt.Errorf("%s.content: is required", path)
	} else if err := validateThreadSpecContent(content, path+".content"); err != nil {
		return err
	}
	return validateIdempotencyKeyField(record, "idempotencyKey", path+".idempotencyKey")
}

func validateThreadStatusChangeRequest(record map[string]interface{}) error {
	const path = "threadStatusChangeRequest"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":  true,
		"fromStatus":     true,
		"toStatus":       true,
		"idempotencyKey": true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	if err := validateThreadStatusField(record, "fromStatus", path+".fromStatus"); err != nil {
		return err
	}
	if err := validateThreadStatusField(record, "toStatus", path+".toStatus"); err != nil {
		return err
	}
	return validateIdempotencyKeyField(record, "idempotencyKey", path+".idempotencyKey")
}

func validateThreadMutationResponse(record map[string]interface{}) error {
	const path = "threadMutationResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"threadId":      true,
		"headLsn":       true,
		"revisionId":    true,
		"contentHash":   true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	if err := validateULIDField(record, "threadId", path+".threadId", "ThreadId"); err != nil {
		return err
	}
	if err := validateEventLsnField(record, "headLsn", path+".headLsn"); err != nil {
		return err
	}
	if err := validateULIDField(record, "revisionId", path+".revisionId", "ThreadSpecRevisionId"); err != nil {
		return err
	}
	return validateSha256HexField(record, "contentHash", path+".contentHash")
}

func validateThreadListResponse(record map[string]interface{}) error {
	const path = "threadListResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"threads":       true,
		"nextCursor":    true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	threads, err := requiredArrayValue(record, "threads", path+".threads")
	if err != nil {
		return err
	}
	if len(threads) > maxRouteThreadListItems {
		return fmt.Errorf("%s.threads: length exceeds MAX_ROUTE_THREAD_LIST_ITEMS: %d > %d", path, len(threads), maxRouteThreadListItems)
	}
	for index, item := range threads {
		threadRecord, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.threads/%d: must be an object", path, index)
		}
		if err := validateThreadViewRecord(threadRecord, fmt.Sprintf("%s.threads/%d", path, index)); err != nil {
			return err
		}
	}
	if cursor, ok, err := optionalString(record, "nextCursor", path+".nextCursor"); err != nil {
		return err
	} else if ok {
		if cursor == "" {
			return fmt.Errorf("%s.nextCursor: must be non-empty when present", path)
		}
		if err := validateUtf8Budget(cursor, maxRouteCursorBytes, "RouteListResponse.nextCursor bytes"); err != nil {
			return fmt.Errorf("%s.nextCursor: %w", path, err)
		}
	}
	return nil
}

func validateThreadGetResponse(record map[string]interface{}) error {
	const path = "threadGetResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"thread":        true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	thread, err := requiredObjectValue(record, "thread", path+".thread")
	if err != nil {
		return err
	}
	return validateThreadViewRecord(thread, path+".thread")
}

func validateThreadPinnedApprovalsResponse(record map[string]interface{}) error {
	const path = "threadPinnedApprovalsResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"threadId":      true,
		"headLsn":       true,
		"approvals":     true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	if err := validateULIDField(record, "threadId", path+".threadId", "ThreadId"); err != nil {
		return err
	}
	if err := validateEventLsnField(record, "headLsn", path+".headLsn"); err != nil {
		return err
	}
	approvals, err := requiredArrayValue(record, "approvals", path+".approvals")
	if err != nil {
		return err
	}
	if len(approvals) > maxRouteApprovalListItems {
		return fmt.Errorf("%s.approvals: length exceeds MAX_ROUTE_APPROVAL_LIST_ITEMS: %d > %d", path, len(approvals), maxRouteApprovalListItems)
	}
	for index, item := range approvals {
		approval, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.approvals/%d: must be an object", path, index)
		}
		if err := validateApprovalViewRecord(approval, fmt.Sprintf("%s.approvals/%d", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func validateApprovalRequestCreateRequest(record map[string]interface{}) error {
	const path = "approvalRequestCreateRequest"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":  true,
		"claim":          true,
		"scope":          true,
		"riskClass":      true,
		"threadId":       true,
		"taskId":         true,
		"receiptId":      true,
		"idempotencyKey": true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	claim, err := requiredObjectValue(record, "claim", path+".claim")
	if err != nil {
		return err
	}
	claimKind, claimID, err := validateApprovalClaim(claim, path+".claim")
	if err != nil {
		return err
	}
	scope, err := requiredObjectValue(record, "scope", path+".scope")
	if err != nil {
		return err
	}
	scopeKind, scopeClaimID, err := validateApprovalScope(scope, path+".scope")
	if err != nil {
		return err
	}
	if claimID != scopeClaimID {
		return fmt.Errorf("%s.scope.claimId: must match claim.claimId", path)
	}
	if claimKind != scopeKind {
		return fmt.Errorf("%s.scope.claimKind: must match claim.kind", path)
	}
	if err := validateApprovalClaimScopeBinding(claimKind, claim, scope, path+".scope"); err != nil {
		return err
	}
	riskClass, err := requiredStringValue(record, "riskClass", path+".riskClass")
	if err != nil {
		return err
	}
	if !riskClassSet[riskClass] {
		return fmt.Errorf("%s.riskClass: must be a valid risk class", path)
	}
	if threadID, ok, err := optionalString(record, "threadId", path+".threadId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(threadID) {
		return fmt.Errorf("%s.threadId: not a ThreadId", path)
	}
	if taskID, ok, err := optionalString(record, "taskId", path+".taskId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(taskID) {
		return fmt.Errorf("%s.taskId: not a TaskId", path)
	}
	if receiptID, ok, err := optionalString(record, "receiptId", path+".receiptId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(receiptID) {
		return fmt.Errorf("%s.receiptId: not a ReceiptId", path)
	} else if claimKind == "receipt_co_sign" {
		if !ok || receiptID != claim["receiptId"] {
			return fmt.Errorf("%s.receiptId: must match claim.receiptId", path)
		}
		if riskClass != claim["riskClass"] {
			return fmt.Errorf("%s.riskClass: must match claim.riskClass", path)
		}
	}
	return validateIdempotencyKeyField(record, "idempotencyKey", path+".idempotencyKey")
}

func validateApprovalDecisionRequest(record map[string]interface{}) error {
	const path = "approvalDecisionRequest"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":  true,
		"decision":       true,
		"token":          true,
		"idempotencyKey": true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	decision, err := requiredStringValue(record, "decision", path+".decision")
	if err != nil {
		return err
	}
	if !approvalDecisionSet[decision] {
		return fmt.Errorf("%s.decision: must be a valid approval decision", path)
	}
	tokenValue, hasToken := record["token"]
	if decision == "approve" && !hasToken {
		return fmt.Errorf("%s.token: is required when decision is approve", path)
	}
	if hasToken {
		token, ok := tokenValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.token: must be an object", path)
		}
		if err := validateSignedApprovalTokenRecord(token, path+".token"); err != nil {
			return err
		}
	}
	return validateIdempotencyKeyField(record, "idempotencyKey", path+".idempotencyKey")
}

func validateApprovalRequestEnvelopeResponse(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion":   true,
		"approvalRequest": true,
		"headLsn":         true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	approvalRequest, err := requiredObjectValue(record, "approvalRequest", path+".approvalRequest")
	if err != nil {
		return err
	}
	if err := validateApprovalRequestRecord(approvalRequest); err != nil {
		return err
	}
	return validateEventLsnField(record, "headLsn", path+".headLsn")
}

func validateApprovalListResponse(record map[string]interface{}) error {
	const path = "approvalListResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"approvals":     true,
		"nextCursor":    true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	approvals, err := requiredArrayValue(record, "approvals", path+".approvals")
	if err != nil {
		return err
	}
	if len(approvals) > maxRouteApprovalListItems {
		return fmt.Errorf("%s.approvals: length exceeds MAX_ROUTE_APPROVAL_LIST_ITEMS: %d > %d", path, len(approvals), maxRouteApprovalListItems)
	}
	for index, item := range approvals {
		approval, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.approvals/%d: must be an object", path, index)
		}
		if err := validateApprovalViewRecord(approval, fmt.Sprintf("%s.approvals/%d", path, index)); err != nil {
			return err
		}
	}
	if cursor, ok, err := optionalString(record, "nextCursor", path+".nextCursor"); err != nil {
		return err
	} else if ok {
		if cursor == "" {
			return fmt.Errorf("%s.nextCursor: must be non-empty when present", path)
		}
		if err := validateUtf8Budget(cursor, maxRouteCursorBytes, "RouteListResponse.nextCursor bytes"); err != nil {
			return fmt.Errorf("%s.nextCursor: %w", path, err)
		}
	}
	return nil
}

func validateApprovalGetResponse(record map[string]interface{}) error {
	const path = "approvalGetResponse"
	if err := knownKeys(record, path, map[string]bool{
		"schemaVersion": true,
		"approval":      true,
	}); err != nil {
		return err
	}
	if err := validateOptionalRouteSchemaVersion(record, path); err != nil {
		return err
	}
	approval, err := requiredObjectValue(record, "approval", path+".approval")
	if err != nil {
		return err
	}
	return validateApprovalViewRecord(approval, path+".approval")
}

func validateApprovalViewRecord(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"id":              true,
		"claim":           true,
		"scope":           true,
		"riskClass":       true,
		"threadId":        true,
		"taskId":          true,
		"receiptId":       true,
		"requestedBy":     true,
		"requestedAt":     true,
		"status":          true,
		"decisionSummary": true,
		"schemaVersion":   true,
	}); err != nil {
		return err
	}
	if err := requiredExactNumber(record, "schemaVersion", path+"/schemaVersion", 1); err != nil {
		return err
	}
	if err := validateULIDField(record, "id", path+".id", "ApprovalRequestId"); err != nil {
		return err
	}
	claim, err := requiredObjectValue(record, "claim", path+".claim")
	if err != nil {
		return err
	}
	claimKind, claimID, err := validateApprovalClaim(claim, path+".claim")
	if err != nil {
		return err
	}
	scope, err := requiredObjectValue(record, "scope", path+".scope")
	if err != nil {
		return err
	}
	scopeKind, scopeClaimID, err := validateApprovalScope(scope, path+".scope")
	if err != nil {
		return err
	}
	if claimID != scopeClaimID {
		return fmt.Errorf("%s.scope.claimId: must match claim.claimId", path)
	}
	if claimKind != scopeKind {
		return fmt.Errorf("%s.scope.claimKind: must match claim.kind", path)
	}
	if err := validateApprovalClaimScopeBinding(claimKind, claim, scope, path+".scope"); err != nil {
		return err
	}
	riskClass, err := requiredStringValue(record, "riskClass", path+".riskClass")
	if err != nil {
		return err
	}
	if !riskClassSet[riskClass] {
		return fmt.Errorf("%s.riskClass: must be a valid risk class", path)
	}
	if threadID, ok, err := optionalString(record, "threadId", path+".threadId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(threadID) {
		return fmt.Errorf("%s.threadId: not a ThreadId", path)
	}
	if taskID, ok, err := optionalString(record, "taskId", path+".taskId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(taskID) {
		return fmt.Errorf("%s.taskId: not a TaskId", path)
	}
	if receiptID, ok, err := optionalString(record, "receiptId", path+".receiptId"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(receiptID) {
		return fmt.Errorf("%s.receiptId: not a ReceiptId", path)
	} else if claimKind == "receipt_co_sign" && (!ok || receiptID != claim["receiptId"]) {
		return fmt.Errorf("%s.receiptId: must match claim.receiptId", path)
	}
	if claimKind == "receipt_co_sign" && riskClass != claim["riskClass"] {
		return fmt.Errorf("%s.riskClass: must match claim.riskClass", path)
	}
	requestedBy, err := requiredStringValue(record, "requestedBy", path+".requestedBy")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(requestedBy, path+".requestedBy"); err != nil {
		return err
	}
	if _, err := requiredTimestampMillis(record, "requestedAt", path+".requestedAt"); err != nil {
		return err
	}
	status, err := requiredStringValue(record, "status", path+".status")
	if err != nil {
		return err
	}
	if !approvalRequestStatusSet[status] {
		return fmt.Errorf("%s.status: must be a valid approval request status", path)
	}
	summaryValue, hasSummary := record["decisionSummary"]
	if status == "pending" && hasSummary {
		return fmt.Errorf("%s/decisionSummary: must be absent when status is pending", path)
	}
	if status != "pending" && !hasSummary {
		return fmt.Errorf("%s/decisionSummary: is required when status is not pending", path)
	}
	if hasSummary {
		summary, ok := summaryValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s/decisionSummary: must be an object", path)
		}
		return validateApprovalDecisionSummaryRecord(summary, status, path+"/decisionSummary")
	}
	return nil
}

func validateApprovalDecisionSummaryRecord(summary map[string]interface{}, status string, path string) error {
	if err := knownKeys(summary, path, map[string]bool{
		"decision":  true,
		"decidedBy": true,
		"decidedAt": true,
	}); err != nil {
		return err
	}
	decision, err := requiredStringValue(summary, "decision", path+".decision")
	if err != nil {
		return err
	}
	if !approvalDecisionSet[decision] {
		return fmt.Errorf("%s.decision: must be a valid approval decision", path)
	}
	expectedStatus := map[string]string{"approve": "approved", "reject": "rejected", "abstain": "abstained"}[decision]
	if status != expectedStatus {
		return fmt.Errorf("%s/decision: must match status", path)
	}
	decidedBy, err := requiredStringValue(summary, "decidedBy", path+".decidedBy")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(decidedBy, path+".decidedBy"); err != nil {
		return err
	}
	_, err = requiredTimestampMillis(summary, "decidedAt", path+".decidedAt")
	return err
}

func validateRouteErrorEnvelope(record map[string]interface{}) error {
	const path = "routeError"
	if err := knownKeys(record, path, map[string]bool{
		"error":        true,
		"message":      true,
		"retryAfterMs": true,
	}); err != nil {
		return err
	}
	errorCode, err := requiredStringValue(record, "error", path+".error")
	if err != nil {
		return err
	}
	if errorCode == "" {
		return fmt.Errorf("%s.error: must be a non-empty string", path)
	}
	if err := validateUtf8Budget(errorCode, maxRouteErrorCodeBytes, "RouteError.error bytes"); err != nil {
		return fmt.Errorf("%s.error: %w", path, err)
	}
	if message, ok, err := optionalString(record, "message", path+".message"); err != nil {
		return err
	} else if ok {
		if err := validateUtf8Budget(message, maxRouteErrorMessageBytes, "RouteError.message bytes"); err != nil {
			return fmt.Errorf("%s.message: %w", path, err)
		}
	}
	if _, ok := record["retryAfterMs"]; ok {
		if err := requiredNonNegativeInteger(record, "retryAfterMs", path+".retryAfterMs", maxSafeInteger); err != nil {
			return err
		}
	}
	return nil
}

func validateThreadRecord(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"thread_id":     true,
		"title":         true,
		"status":        true,
		"spec":          true,
		"external_refs": true,
		"task_ids":      true,
		"created_by":    true,
		"created_at":    true,
		"updated_at":    true,
		"closed_at":     true,
	}); err != nil {
		return err
	}
	threadID, err := requiredStringValue(record, "thread_id", path+"/thread_id")
	if err != nil {
		return err
	}
	if !ulidRE.MatchString(threadID) {
		return fmt.Errorf("%s/thread_id: not a ThreadId", path)
	}
	title, err := requiredStringValue(record, "title", path+"/title")
	if err != nil {
		return err
	}
	if title == "" {
		return fmt.Errorf("%s/title: must be a non-empty string", path)
	}
	if err := validateUtf8Budget(title, maxThreadTitleBytes, path+"/title"); err != nil {
		return err
	}
	status, err := requiredStringValue(record, "status", path+"/status")
	if err != nil {
		return err
	}
	if !threadStatusSet[status] {
		return fmt.Errorf("%s/status: must be a valid thread status", path)
	}
	spec, err := requiredObjectValue(record, "spec", path+"/spec")
	if err != nil {
		return err
	}
	if err := validateThreadSpecRevisionRecord(spec, path+"/spec", threadID); err != nil {
		return err
	}
	externalRefs, err := requiredObjectValue(record, "external_refs", path+"/external_refs")
	if err != nil {
		return err
	}
	if err := validateThreadExternalRefsRecord(externalRefs, path+"/external_refs"); err != nil {
		return err
	}
	taskIDs, err := requiredArrayValue(record, "task_ids", path+"/task_ids")
	if err != nil {
		return err
	}
	if len(taskIDs) > maxThreadTaskIDs {
		return fmt.Errorf("%s/task_ids: length exceeds MAX_THREAD_TASK_IDS: %d > %d", path, len(taskIDs), maxThreadTaskIDs)
	}
	seenTaskIDs := map[string]bool{}
	for index, item := range taskIDs {
		taskID, ok := item.(string)
		if !ok {
			return fmt.Errorf("%s/task_ids/%d: must be a string", path, index)
		}
		if !ulidRE.MatchString(taskID) {
			return fmt.Errorf("%s/task_ids/%d: must be an uppercase ULID TaskId", path, index)
		}
		if seenTaskIDs[taskID] {
			return fmt.Errorf("%s/task_ids/%d: must be unique", path, index)
		}
		seenTaskIDs[taskID] = true
	}
	createdBy, err := requiredStringValue(record, "created_by", path+"/created_by")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(createdBy, path+"/created_by"); err != nil {
		return err
	}
	if _, err := requiredTimestampMillis(record, "created_at", path+"/created_at"); err != nil {
		return err
	}
	if _, err := requiredTimestampMillis(record, "updated_at", path+"/updated_at"); err != nil {
		return err
	}
	if closedAt, ok, err := optionalString(record, "closed_at", path+"/closed_at"); err != nil {
		return err
	} else if ok {
		if err := validateCanonicalTimestamp(closedAt, path+"/closed_at"); err != nil {
			return err
		}
		if status != "merged" && status != "closed" {
			return fmt.Errorf("%s/closed_at: must be absent unless status is terminal", path)
		}
	}
	return nil
}

func validateThreadViewRecord(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"thread_id":            true,
		"title":                true,
		"status":               true,
		"spec":                 true,
		"external_refs":        true,
		"task_ids":             true,
		"created_by":           true,
		"created_at":           true,
		"updated_at":           true,
		"closed_at":            true,
		"effectiveStatus":      true,
		"attentionReason":      true,
		"boardColumn":          true,
		"currentSeat":          true,
		"pendingApprovalCount": true,
	}); err != nil {
		return err
	}
	if err := validateThreadRecord(threadRecordSubset(record), path); err != nil {
		return err
	}
	effectiveStatus, err := requiredStringValue(record, "effectiveStatus", path+".effectiveStatus")
	if err != nil {
		return err
	}
	if !threadEffectiveStatusSet[effectiveStatus] {
		return fmt.Errorf("%s.effectiveStatus: must be a valid thread effective status", path)
	}
	attentionReason, hasAttentionReason, err := optionalString(record, "attentionReason", path+".attentionReason")
	if err != nil {
		return err
	}
	if hasAttentionReason && !threadAttentionReasonSet[attentionReason] {
		return fmt.Errorf("%s.attentionReason: must be a valid thread attention reason", path)
	}
	if effectiveStatus == "needs_attention" && !hasAttentionReason {
		return fmt.Errorf("%s.attentionReason: is required when effectiveStatus is needs_attention", path)
	}
	if effectiveStatus != "needs_attention" && hasAttentionReason {
		return fmt.Errorf("%s.attentionReason: must be absent unless effectiveStatus is needs_attention", path)
	}
	boardColumn, err := requiredStringValue(record, "boardColumn", path+".boardColumn")
	if err != nil {
		return err
	}
	if !threadBoardColumnSet[boardColumn] {
		return fmt.Errorf("%s.boardColumn: must be a valid thread board column", path)
	}
	if boardColumn != boardColumnForEffectiveStatus(effectiveStatus) {
		return fmt.Errorf("%s.boardColumn: must match effectiveStatus", path)
	}
	currentSeat, err := requiredStringValue(record, "currentSeat", path+".currentSeat")
	if err != nil {
		return err
	}
	if !threadCurrentSeatSet[currentSeat] {
		return fmt.Errorf("%s.currentSeat: must be a valid thread current seat", path)
	}
	status, err := requiredStringValue(record, "status", path+"/status")
	if err != nil {
		return err
	}
	expectedSeat := "agent"
	if effectiveStatus == "needs_attention" || status == "needs_review" {
		expectedSeat = "human"
	}
	if currentSeat != expectedSeat {
		return fmt.Errorf("%s.currentSeat: must match effectiveStatus and status", path)
	}
	return requiredNonNegativeInteger(record, "pendingApprovalCount", path+".pendingApprovalCount", maxSafeInteger)
}

func threadRecordSubset(record map[string]interface{}) map[string]interface{} {
	subset := map[string]interface{}{}
	for _, key := range []string{
		"thread_id",
		"title",
		"status",
		"spec",
		"external_refs",
		"task_ids",
		"created_by",
		"created_at",
		"updated_at",
		"closed_at",
	} {
		if value, ok := record[key]; ok {
			subset[key] = value
		}
	}
	return subset
}

func boardColumnForEffectiveStatus(status string) string {
	switch status {
	case "needs_attention":
		return "needs_me"
	case "needs_review":
		return "review"
	case "merged", "closed":
		return "done"
	default:
		return "running"
	}
}

func validateThreadSpecRevisionRecord(record map[string]interface{}, path string, expectedThreadID string) error {
	if err := knownKeys(record, path, map[string]bool{
		"revision_id":      true,
		"thread_id":        true,
		"base_revision_id": true,
		"content":          true,
		"content_hash":     true,
		"authored_by":      true,
		"authored_at":      true,
	}); err != nil {
		return err
	}
	if err := validateULIDField(record, "revision_id", path+"/revision_id", "ThreadSpecRevisionId"); err != nil {
		return err
	}
	threadID, err := requiredStringValue(record, "thread_id", path+"/thread_id")
	if err != nil {
		return err
	}
	if !ulidRE.MatchString(threadID) {
		return fmt.Errorf("%s/thread_id: not a ThreadId", path)
	}
	if threadID != expectedThreadID {
		return fmt.Errorf("%s/thread_id: must match parent thread_id", path)
	}
	if baseRevisionID, ok, err := optionalString(record, "base_revision_id", path+"/base_revision_id"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(baseRevisionID) {
		return fmt.Errorf("%s/base_revision_id: not a ThreadSpecRevisionId", path)
	}
	content, ok := record["content"]
	if !ok {
		return fmt.Errorf("%s/content: is required", path)
	}
	contentHash, err := requiredStringValue(record, "content_hash", path+"/content_hash")
	if err != nil {
		return err
	}
	if !sha256HexRE.MatchString(contentHash) {
		return fmt.Errorf("%s/content_hash: not a Sha256Hex", path)
	}
	derived, err := deriveThreadSpecContentHash(content, path+"/content")
	if err != nil {
		return err
	}
	if contentHash != derived {
		return fmt.Errorf("%s/content_hash: must match sha256(canonical(content))", path)
	}
	authoredBy, err := requiredStringValue(record, "authored_by", path+"/authored_by")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(authoredBy, path+"/authored_by"); err != nil {
		return err
	}
	_, err = requiredTimestampMillis(record, "authored_at", path+"/authored_at")
	return err
}

func validateThreadExternalRefsRecord(record map[string]interface{}, path string) error {
	if err := knownKeys(record, path, map[string]bool{
		"source_urls": true,
		"entity_ids":  true,
	}); err != nil {
		return err
	}
	if err := validateExternalRefArrayField(record, "source_urls", path+"/source_urls"); err != nil {
		return err
	}
	return validateExternalRefArrayField(record, "entity_ids", path+"/entity_ids")
}

func validateExternalRefArrayField(record map[string]interface{}, key string, path string) error {
	items, err := requiredArrayValue(record, key, path)
	if err != nil {
		return err
	}
	if len(items) > maxThreadExternalRefs {
		return fmt.Errorf("%s: length exceeds MAX_THREAD_EXTERNAL_REFS: %d > %d", path, len(items), maxThreadExternalRefs)
	}
	seen := map[string]bool{}
	for index, item := range items {
		value, ok := item.(string)
		if !ok {
			return fmt.Errorf("%s/%d: must be a string", path, index)
		}
		if value == "" {
			return fmt.Errorf("%s/%d: must be a non-empty string", path, index)
		}
		if err := validateUtf8Budget(value, maxThreadExternalRefBytes, fmt.Sprintf("%s/%d", path, index)); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%s/%d: must be unique", path, index)
		}
		seen[value] = true
	}
	return nil
}

func validateOptionalRouteSchemaVersion(record map[string]interface{}, path string) error {
	value, ok := record["schemaVersion"]
	if !ok {
		record["schemaVersion"] = float64(1)
		return nil
	}
	numberValue, ok := value.(float64)
	if !ok || numberValue != float64(int64(numberValue)) {
		return fmt.Errorf("%s.schemaVersion: must be an integer", path)
	}
	if numberValue > 1 {
		return fmt.Errorf("%s.schemaVersion: unsupported schemaVersion", path)
	}
	if numberValue != 1 {
		return fmt.Errorf("%s.schemaVersion: must be 1", path)
	}
	return nil
}

func validateIdempotencyKeyField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !idempotencyKeyRE.MatchString(value) {
		return fmt.Errorf("%s: not an IdempotencyKey", path)
	}
	return nil
}

func validateULIDField(record map[string]interface{}, key string, path string, brand string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !ulidRE.MatchString(value) {
		return fmt.Errorf("%s: not a %s", path, brand)
	}
	return nil
}

func validateThreadStatusField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !threadStatusSet[value] {
		return fmt.Errorf("%s: must be a valid thread status", path)
	}
	return nil
}

func validateEventLsnField(record map[string]interface{}, key string, path string) error {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(value, "v1:") {
		return fmt.Errorf("%s: must be an EventLsn", path)
	}
	seq := strings.TrimPrefix(value, "v1:")
	if seq == "" {
		return fmt.Errorf("%s: must be an EventLsn", path)
	}
	if len(seq) > 1 && strings.HasPrefix(seq, "0") {
		return fmt.Errorf("%s: must be an EventLsn", path)
	}
	for _, r := range seq {
		if r < '0' || r > '9' {
			return fmt.Errorf("%s: must be an EventLsn", path)
		}
	}
	parsed, err := strconv.ParseInt(seq, 10, 64)
	if err != nil || parsed < 0 || parsed > maxSafeInteger {
		return fmt.Errorf("%s: must be an EventLsn", path)
	}
	return nil
}

func validateThreadSpecContent(value interface{}, path string) error {
	_, err := deriveThreadSpecContentHash(value, path)
	return err
}

func deriveThreadSpecContentHash(value interface{}, path string) (string, error) {
	canonical, err := canonicalize(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if len(canonical) > maxThreadSpecContentBytes {
		return "", fmt.Errorf("%s: ThreadSpecRevision.content bytes: exceeds %d UTF-8 bytes", path, maxThreadSpecContentBytes)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func parseApprovalRequest(raw json.RawMessage) (map[string]interface{}, error) {
	var record map[string]interface{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("approvalRequest: must be an object: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("approvalRequest: must be an object")
	}
	if err := validateApprovalRequestRecord(record); err != nil {
		return nil, err
	}
	return record, nil
}

func validateApprovalRequestRecord(record map[string]interface{}) error {
	if err := knownKeys(record, "approvalRequest", map[string]bool{
		"request_id":     true,
		"claim":          true,
		"scope":          true,
		"risk_class":     true,
		"thread_id":      true,
		"task_id":        true,
		"receipt_id":     true,
		"requested_by":   true,
		"requested_at":   true,
		"status":         true,
		"decision":       true,
		"schema_version": true,
	}); err != nil {
		return err
	}
	if err := requiredExactNumber(record, "schema_version", "approvalRequest/schema_version", 1); err != nil {
		return err
	}
	requestID, err := requiredStringValue(record, "request_id", "approvalRequest/request_id")
	if err != nil {
		return err
	}
	if !ulidRE.MatchString(requestID) {
		return fmt.Errorf("approvalRequest/request_id: not an ApprovalRequestId")
	}
	claim, err := requiredObjectValue(record, "claim", "approvalRequest/claim")
	if err != nil {
		return err
	}
	claimKind, claimID, err := validateApprovalClaim(claim, "approvalRequest/claim")
	if err != nil {
		return err
	}
	scope, err := requiredObjectValue(record, "scope", "approvalRequest/scope")
	if err != nil {
		return err
	}
	scopeKind, scopeClaimID, err := validateApprovalScope(scope, "approvalRequest/scope")
	if err != nil {
		return err
	}
	if claimID != scopeClaimID {
		return fmt.Errorf("approvalRequest/scope/claimId: must match claim.claimId")
	}
	if claimKind != scopeKind {
		return fmt.Errorf("approvalRequest/scope/claimKind: must match claim.kind")
	}
	if err := validateApprovalClaimScopeBinding(claimKind, claim, scope, "approvalRequest/scope"); err != nil {
		return err
	}
	riskClass, err := requiredStringValue(record, "risk_class", "approvalRequest/risk_class")
	if err != nil {
		return err
	}
	if !riskClassSet[riskClass] {
		return fmt.Errorf("approvalRequest/risk_class: must be a valid risk class")
	}
	if threadID, ok, err := optionalString(record, "thread_id", "approvalRequest/thread_id"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(threadID) {
		return fmt.Errorf("approvalRequest/thread_id: not a ThreadId")
	}
	if taskID, ok, err := optionalString(record, "task_id", "approvalRequest/task_id"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(taskID) {
		return fmt.Errorf("approvalRequest/task_id: not a TaskId")
	}
	if receiptID, ok, err := optionalString(record, "receipt_id", "approvalRequest/receipt_id"); err != nil {
		return err
	} else if ok && !ulidRE.MatchString(receiptID) {
		return fmt.Errorf("approvalRequest/receipt_id: not a ReceiptId")
	}
	requestedBy, err := requiredStringValue(record, "requested_by", "approvalRequest/requested_by")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(requestedBy, "approvalRequest/requested_by"); err != nil {
		return err
	}
	if _, err := requiredTimestampMillis(record, "requested_at", "approvalRequest/requested_at"); err != nil {
		return err
	}
	status, err := requiredStringValue(record, "status", "approvalRequest/status")
	if err != nil {
		return err
	}
	if !approvalRequestStatusSet[status] {
		return fmt.Errorf("approvalRequest/status: must be a valid approval request status")
	}
	if claimKind == "receipt_co_sign" {
		receiptID, ok, err := optionalString(record, "receipt_id", "approvalRequest/receipt_id")
		if err != nil {
			return err
		}
		if !ok || receiptID != claim["receiptId"] {
			return fmt.Errorf("approvalRequest/receiptId: must match claim.receiptId")
		}
		if riskClass != claim["riskClass"] {
			return fmt.Errorf("approvalRequest/riskClass: must match claim.riskClass")
		}
	}
	decisionValue, hasDecision := record["decision"]
	if status == "pending" && hasDecision {
		return fmt.Errorf("approvalRequest/decision: must be absent when status is pending")
	}
	if status != "pending" && !hasDecision {
		return fmt.Errorf("approvalRequest/decision: is required when status is not pending")
	}
	if hasDecision {
		decision, ok := decisionValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("approvalRequest/decision: must be an object")
		}
		return validateApprovalDecisionRecord(decision, status, claim, scope)
	}
	return nil
}

func validateApprovalDecisionRecord(decision map[string]interface{}, status string, requestClaim map[string]interface{}, requestScope map[string]interface{}) error {
	if err := knownKeys(decision, "approvalRequest/decision", map[string]bool{
		"decision":   true,
		"decided_by": true,
		"decided_at": true,
		"token":      true,
	}); err != nil {
		return err
	}
	decisionValue, err := requiredStringValue(decision, "decision", "approvalRequest/decision/decision")
	if err != nil {
		return err
	}
	if !approvalDecisionSet[decisionValue] {
		return fmt.Errorf("approvalRequest/decision/decision: must be a valid approval decision")
	}
	expectedStatus := map[string]string{"approve": "approved", "reject": "rejected", "abstain": "abstained"}[decisionValue]
	if status != expectedStatus {
		return fmt.Errorf("approvalRequest/decision/decision: must match status")
	}
	decidedBy, err := requiredStringValue(decision, "decided_by", "approvalRequest/decision/decided_by")
	if err != nil {
		return err
	}
	if err := validateSignerIdentity(decidedBy, "approvalRequest/decision/decided_by"); err != nil {
		return err
	}
	decidedAtMs, err := requiredTimestampMillis(decision, "decided_at", "approvalRequest/decision/decided_at")
	if err != nil {
		return err
	}
	tokenValue, hasToken := decision["token"]
	if decisionValue == "approve" && !hasToken {
		return fmt.Errorf("approvalRequest/decision/token: is required when decision is approve")
	}
	if !hasToken {
		return nil
	}
	token, ok := tokenValue.(map[string]interface{})
	if !ok {
		return fmt.Errorf("approvalRequest/decision/token: must be an object")
	}
	if err := validateSignedApprovalTokenRecord(token, "approvalRequest/decision/token"); err != nil {
		return err
	}
	tokenClaim, err := requiredObjectValue(token, "claim", "approvalRequest/decision/token/claim")
	if err != nil {
		return err
	}
	tokenScope, err := requiredObjectValue(token, "scope", "approvalRequest/decision/token/scope")
	if err != nil {
		return err
	}
	if err := requireCanonicalEqual(tokenClaim, requestClaim, "approvalRequest/decision/token/claim", "must match request claim"); err != nil {
		return err
	}
	if err := requireCanonicalEqual(tokenScope, requestScope, "approvalRequest/decision/token/scope", "must match request scope"); err != nil {
		return err
	}
	notBefore, err := requiredSafeInteger(token, "notBefore", "approvalRequest/decision/token/notBefore")
	if err != nil {
		return err
	}
	expiresAt, err := requiredSafeInteger(token, "expiresAt", "approvalRequest/decision/token/expiresAt")
	if err != nil {
		return err
	}
	if decidedAtMs < notBefore || decidedAtMs >= expiresAt {
		return fmt.Errorf("approvalRequest/decision/decidedAt: must be within token validity window")
	}
	return nil
}

func requireCanonicalEqual(left interface{}, right interface{}, path string, message string) error {
	leftBytes, err := canonicalize(left)
	if err != nil {
		return err
	}
	rightBytes, err := canonicalize(right)
	if err != nil {
		return err
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		return fmt.Errorf("%s: %s", path, message)
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

func validateSignerIdentity(value string, path string) error {
	if value == "" {
		return fmt.Errorf("%s: must be a bounded non-empty SignerIdentity", path)
	}
	if err := validateUtf8Budget(value, 256, path); err != nil {
		return err
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
	normalized := norm.NFKC.String(value)
	var out strings.Builder
	for _, r := range normalized {
		if !isAllowlistDisallowedCodePoint(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
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

func requiredTimestampMillis(record map[string]interface{}, key string, path string) (int64, error) {
	value, err := requiredStringValue(record, key, path)
	if err != nil {
		return 0, err
	}
	if err := validateCanonicalTimestamp(value, path); err != nil {
		return 0, err
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0, fmt.Errorf("%s: must be a valid ISO8601 UTC millisecond timestamp", path)
	}
	return parsed.UnixNano() / int64(time.Millisecond), nil
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

func requiredArrayValue(record map[string]interface{}, key string, path string) ([]interface{}, error) {
	value, ok := record[key]
	if !ok {
		return nil, fmt.Errorf("%s: is required", path)
	}
	arrayValue, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	return arrayValue, nil
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
	approvalRequestFx, err := loadApprovalRequestFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load approval-request fixture: %v\n", err)
		os.Exit(2)
	}
	routeEnvelopeFx, err := loadRouteEnvelopeFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load route-envelope fixture: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("%s@wuphf/protocol — Go reference verifier%s\n", colorBold, colorReset)
	fmt.Printf("%sLoaded fixture schemaVersion=%d, %d audit-event vectors, %d merkle-root vectors, %d signed-approval-token accept vectors, %d signed-approval-token reject vectors, %d approval-request accept vectors, %d approval-request reject vectors, %d route-envelope accept vectors, %d route-envelope reject vectors, %d runner accept vectors, %d runner reject vectors, %d agent-provider-routing accept vectors, %d agent-provider-routing reject vectors%s\n\n",
		colorDim, fx.SchemaVersion, len(fx.Vectors), len(fx.MerkleRootVectors), len(signedApprovalTokenFx.Accepted), len(signedApprovalTokenFx.Rejected), len(approvalRequestFx.Accepted), len(approvalRequestFx.Rejected), len(routeEnvelopeFx.Accepted), len(routeEnvelopeFx.Rejected), len(runnerFx.Vectors), len(runnerFx.RejectVectors), len(agentProviderRoutingFx.Accepted), len(agentProviderRoutingFx.Rejected), colorReset)

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
		// For typed JSON payload kinds, also confirm the bodyB64 itself is canonical
		// JSON: an independent canonicalizer must reproduce identical bytes.
		// This locks the body-bytes contract that the audit chain relies on.
		if canonicalJsonPayloadKindSet[vec.Input.Payload.Kind] {
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

	for _, vec := range approvalRequestFx.Accepted {
		if err := validateApprovalRequestAccepted(vec); err != nil {
			fmt.Printf("  %sFAIL%s approval-request/%s: expected accept, got %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s approval-request/%s accepted\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range approvalRequestFx.Rejected {
		if err := validateApprovalRequestRejected(vec); err != nil {
			fmt.Printf("  %sFAIL%s approval-request/%s: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s approval-request/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range routeEnvelopeFx.Accepted {
		if err := validateRouteEnvelopeAccepted(vec); err != nil {
			fmt.Printf("  %sFAIL%s route-envelope/%s: expected accept, got %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s route-envelope/%s accepted\n", colorGreen, colorReset, vec.Name)
	}

	for _, vec := range routeEnvelopeFx.Rejected {
		if err := validateRouteEnvelopeRejected(vec); err != nil {
			fmt.Printf("  %sFAIL%s route-envelope/%s: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s route-envelope/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%sAll %d vectors match — wire contract is cross-language portable.%s\n",
			colorBold, colorGreen, len(fx.Vectors)+len(fx.MerkleRootVectors)+len(signedApprovalTokenFx.Accepted)+len(signedApprovalTokenFx.Rejected)+len(approvalRequestFx.Accepted)+len(approvalRequestFx.Rejected)+len(routeEnvelopeFx.Accepted)+len(routeEnvelopeFx.Rejected)+len(runnerFx.Vectors)+len(runnerFx.RejectVectors)+len(agentProviderRoutingFx.Accepted)+len(agentProviderRoutingFx.Rejected), colorReset)
		os.Exit(0)
	}
	fmt.Printf("%s%s%d vector(s) failed — TypeScript writer and Go reference disagree.%s\n",
		colorBold, colorRed, failed, colorReset)
	os.Exit(1)
}

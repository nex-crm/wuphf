//go:build ignore

// verifier-reference.go — single-file Go reference for selected
// @wuphf/protocol wire contracts.
//
// Purpose: prove that an independent implementation in another language
// produces byte-identical hashes/signing bytes from the same canonical inputs
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
// on purpose so that this file remains stdlib-only and pasteable.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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

type approvalClaimsInput struct {
	SignerIdentity    string  `json:"signerIdentity"`
	Role              string  `json:"role"`
	ReceiptID         string  `json:"receiptId"`
	WriteID           *string `json:"writeId"`
	FrozenArgsHash    string  `json:"frozenArgsHash"`
	RiskClass         string  `json:"riskClass"`
	IssuedAt          string  `json:"issuedAt"`
	ExpiresAt         string  `json:"expiresAt"`
	WebauthnAssertion *string `json:"webauthnAssertion"`
}

type approvalClaimsExpected struct {
	SigningBytes string `json:"signingBytes"`
}

type approvalClaimsVector struct {
	Name     string                 `json:"name"`
	Input    approvalClaimsInput    `json:"input"`
	Expected approvalClaimsExpected `json:"expected"`
}

type fixture struct {
	SchemaVersion         int                    `json:"schemaVersion"`
	Comment               string                 `json:"comment"`
	Vectors               []auditEventVector     `json:"vectors"`
	MerkleRootVectors     []merkleRootVector     `json:"merkleRootVectors"`
	ApprovalClaimsVectors []approvalClaimsVector `json:"approvalClaimsVectors"`
}

type runnerVector struct {
	Name string                 `json:"name"`
	Kind string                 `json:"kind"`
	JSON map[string]interface{} `json:"json"`
}

type runnerFixture struct {
	SchemaVersion int            `json:"schemaVersion"`
	Vectors       []runnerVector `json:"vectors"`
	RejectVectors []runnerVector `json:"rejectVectors"`
}

// canonicalize is a *minimal* JCS implementation sufficient for the bundled
// vectors. Sorts object keys lexicographically (Go's encoding/json does this
// for map[string]any by default), uses null for nil, and emits no whitespace.
// Strings get standard JSON escaping (sufficient because the vectors use
// base64 / ASCII / printable Unicode only).
//
// LIMITATIONS vs full RFC 8785: no number normalization (vectors carry no
// numbers in the hashed projection), no special handling of -0 or NaN, no
// surrogate-pair sanitization. If you extend the vectors with numbers or
// adversarial Unicode, swap this for a real JCS library before trusting the
// match.
func canonicalize(v interface{}) ([]byte, error) {
	return json.Marshal(v)
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

func canonicalApprovalClaims(rec approvalClaimsInput) ([]byte, error) {
	projection := map[string]interface{}{
		"signerIdentity": rec.SignerIdentity,
		"role":           rec.Role,
		"receiptId":      rec.ReceiptID,
		"frozenArgsHash": rec.FrozenArgsHash,
		"riskClass":      rec.RiskClass,
		"issuedAt":       rec.IssuedAt,
		"expiresAt":      rec.ExpiresAt,
	}
	if rec.WriteID != nil {
		projection["writeId"] = *rec.WriteID
	}
	if rec.WebauthnAssertion != nil {
		projection["webauthnAssertion"] = *rec.WebauthnAssertion
	}
	return canonicalize(projection)
}

var (
	agentIDRE          = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
	credentialHandleRE = regexp.MustCompile(`^cred_[A-Za-z0-9_-]{22,128}$`)
	runnerIDRE         = regexp.MustCompile(`^run_[A-Za-z0-9_-]{22,128}$`)
	ulidRE             = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	isoUtcRE           = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
)

var runnerKindSet = map[string]bool{
	"claude-cli":    true,
	"codex-cli":     true,
	"openai-compat": true,
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
	if _, err := requiredStringValue(record, "prompt", "runnerSpawnRequest.prompt"); err != nil {
		return err
	}
	if err := optionalStringValue(record, "model", "runnerSpawnRequest.model"); err != nil {
		return err
	}
	if err := optionalStringValue(record, "cwd", "runnerSpawnRequest.cwd"); err != nil {
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
	if !credentialScopeSet[scope] {
		return fmt.Errorf("runnerSpawnRequest.providerRoute.credentialScope: unsupported CredentialScope")
	}
	providerKind, err := requiredStringValue(record, "providerKind", "runnerSpawnRequest.providerRoute.providerKind")
	if err != nil {
		return err
	}
	if !providerKindSet[providerKind] {
		return fmt.Errorf("runnerSpawnRequest.providerRoute.providerKind: unsupported ProviderKind")
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
	if !runnerIDRE.MatchString(runnerID) {
		return fmt.Errorf("runnerEvent.runnerId: not a RunnerId")
	}
	at, err := requiredStringValue(record, "at", "runnerEvent.at")
	if err != nil {
		return err
	}
	if !isoUtcRE.MatchString(at) {
		return fmt.Errorf("runnerEvent.at: must be an ISO8601 UTC millisecond timestamp")
	}
	switch kind {
	case "stdout", "stderr":
		_, err = requiredStringValue(record, "chunk", "runnerEvent.chunk")
		return err
	case "cost":
		_, err = requiredObjectValue(record, "entry", "runnerEvent.entry")
		return err
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
		_, err = requiredStringValue(record, "error", "runnerEvent.error")
		return err
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
	if !credentialHandleRE.MatchString(id) {
		return fmt.Errorf("credentialHandle.id: not a CredentialHandleId")
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

func optionalStringValue(record map[string]interface{}, key string, path string) error {
	_, _, err := optionalString(record, key, path)
	return err
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

	fmt.Printf("%s@wuphf/protocol — Go reference verifier%s\n", colorBold, colorReset)
	fmt.Printf("%sLoaded fixture schemaVersion=%d, %d audit-event vectors, %d merkle-root vectors, %d approval-claims vectors, %d runner accept vectors, %d runner reject vectors%s\n\n",
		colorDim, fx.SchemaVersion, len(fx.Vectors), len(fx.MerkleRootVectors), len(fx.ApprovalClaimsVectors), len(runnerFx.Vectors), len(runnerFx.RejectVectors), colorReset)

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

	for _, vec := range fx.ApprovalClaimsVectors {
		canonical, err := canonicalApprovalClaims(vec.Input)
		if err != nil {
			fmt.Printf("  %sFAIL%s approval-claims/%s: canonicalize error: %v\n", colorRed, colorReset, vec.Name, err)
			failed++
			continue
		}
		if string(canonical) != vec.Expected.SigningBytes {
			fmt.Printf("  %sFAIL%s approval-claims/%s: signingBytes mismatch\n", colorRed, colorReset, vec.Name)
			fmt.Printf("    expected: %s\n", vec.Expected.SigningBytes)
			fmt.Printf("    actual:   %s\n", string(canonical))
			failed++
			continue
		}
		fmt.Printf("  %sPASS%s approval-claims/%s signingBytes match (%d bytes)%s\n",
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
		}
		fmt.Printf("  %sPASS%s runner/%s rejected\n", colorGreen, colorReset, vec.Name)
	}

	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%sAll %d vectors match — wire contract is cross-language portable.%s\n",
			colorBold, colorGreen, len(fx.Vectors)+len(fx.MerkleRootVectors)+len(fx.ApprovalClaimsVectors)+len(runnerFx.Vectors)+len(runnerFx.RejectVectors), colorReset)
		os.Exit(0)
	}
	fmt.Printf("%s%s%d vector(s) failed — TypeScript writer and Go reference disagree.%s\n",
		colorBold, colorRed, failed, colorReset)
	os.Exit(1)
}

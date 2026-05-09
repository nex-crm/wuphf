// verifier-reference.go — single-file Go reference for the @wuphf/protocol
// audit-chain wire contract.
//
// Purpose: prove that an independent implementation in another language
// produces byte-identical hashes from the same canonical inputs as the
// TypeScript writer. If this program prints "all vectors match", the wire
// contract is genuinely cross-language portable. If any vector fails, the
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	Name     string             `json:"name"`
	Input    auditEventInput    `json:"input"`
	Expected auditEventExpected `json:"expected"`
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

const (
	colorReset = "\x1b[0m"
	colorGreen = "\x1b[32m"
	colorRed   = "\x1b[31m"
	colorDim   = "\x1b[2m"
	colorBold  = "\x1b[1m"
)

func main() {
	bytes, err := os.ReadFile("audit-event-vectors.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read fixture: %v\n", err)
		fmt.Fprintln(os.Stderr, "run from packages/protocol/testdata/")
		os.Exit(2)
	}

	var fx fixture
	if err := json.Unmarshal(bytes, &fx); err != nil {
		fmt.Fprintf(os.Stderr, "could not parse fixture: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("%s@wuphf/protocol — Go reference verifier%s\n", colorBold, colorReset)
	fmt.Printf("%sLoaded fixture schemaVersion=%d, %d audit-event vectors, %d merkle-root vectors%s\n\n",
		colorDim, fx.SchemaVersion, len(fx.Vectors), len(fx.MerkleRootVectors), colorReset)

	failed := 0

	for _, vec := range fx.Vectors {
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

	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%sAll %d vectors match — wire contract is cross-language portable.%s\n",
			colorBold, colorGreen, len(fx.Vectors)+len(fx.MerkleRootVectors), colorReset)
		os.Exit(0)
	}
	fmt.Printf("%s%s%d vector(s) failed — TypeScript writer and Go reference disagree.%s\n",
		colorBold, colorRed, failed, colorReset)
	os.Exit(1)
}

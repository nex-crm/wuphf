package openclaw

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DeviceIdentity is a persistent Ed25519 keypair used for OpenClaw device auth.
//
// OpenClaw requires clients to pair as a device: present a public key + a signature
// over a payload that includes the server-sent nonce. On loopback the gateway
// auto-approves pairing silently. Token auth alone grants ZERO scopes.
type DeviceIdentity struct {
	deviceID string
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
}

type storedIdentity struct {
	Version       int    `json:"version"`
	DeviceID      string `json:"deviceId"`
	PrivateKeyB64 string `json:"privateKeyB64url"`
	CreatedAtMs   int64  `json:"createdAtMs"`
}

// LoadOrCreateDeviceIdentity returns the keypair at path, creating one if missing.
// The file is written mode 0600 so it stays private.
func LoadOrCreateDeviceIdentity(path string) (*DeviceIdentity, error) {
	if path == "" {
		return nil, errors.New("openclaw: identity path required")
	}
	if data, err := os.ReadFile(path); err == nil {
		id, err := parseIdentity(data)
		if err == nil {
			return id, nil
		}
		// Corrupt file — regenerate rather than fail startup.
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("openclaw: generate identity: %w", err)
	}
	id := &DeviceIdentity{
		deviceID: computeDeviceID(pub),
		priv:     priv,
		pub:      pub,
	}
	stored := storedIdentity{
		Version:       1,
		DeviceID:      id.deviceID,
		PrivateKeyB64: base64URLNoPad(priv),
		CreatedAtMs:   nowMs(),
	}
	body, err := json.MarshalIndent(&stored, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("openclaw: marshal identity: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("openclaw: mkdir identity: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return nil, fmt.Errorf("openclaw: write identity: %w", err)
	}
	return id, nil
}

func parseIdentity(raw []byte) (*DeviceIdentity, error) {
	var s storedIdentity
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Version != 1 || s.PrivateKeyB64 == "" {
		return nil, errors.New("identity: unsupported version")
	}
	priv, err := base64URLDecodeNoPad(s.PrivateKeyB64)
	if err != nil {
		return nil, err
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("identity: wrong key size")
	}
	privKey := ed25519.PrivateKey(priv)
	pub := privKey.Public().(ed25519.PublicKey)
	return &DeviceIdentity{
		deviceID: computeDeviceID(pub),
		priv:     privKey,
		pub:      pub,
	}, nil
}

// DeviceID is hex(sha256(raw 32-byte ed25519 public key)), matching OpenClaw's
// deriveDeviceIdFromPublicKey at infra/device-identity.ts.
func (d *DeviceIdentity) DeviceID() string { return d.deviceID }

// PublicKeyB64URL returns the raw 32-byte public key as base64url (no padding).
// This is what OpenClaw expects in connect.params.device.publicKey.
func (d *DeviceIdentity) PublicKeyB64URL() string { return base64URLNoPad(d.pub) }

// Sign returns base64url(no-pad) Ed25519 signature over payload.
func (d *DeviceIdentity) Sign(payload []byte) string {
	return base64URLNoPad(ed25519.Sign(d.priv, payload))
}

// BuildDeviceAuthPayloadV3 produces the exact pipe-delimited string OpenClaw
// signs/verifies in buildDeviceAuthPayloadV3 (gateway/device-auth.ts).
//
// Format: v3|deviceId|clientId|clientMode|role|scopes|signedAtMs|token|nonce|platform|deviceFamily
// platform and deviceFamily are lowercase-trimmed.
func BuildDeviceAuthPayloadV3(p DeviceAuthPayloadV3) []byte {
	scopes := joinCSV(p.Scopes)
	return []byte(
		"v3|" +
			p.DeviceID + "|" +
			p.ClientID + "|" +
			p.ClientMode + "|" +
			p.Role + "|" +
			scopes + "|" +
			fmtInt64(p.SignedAtMs) + "|" +
			p.Token + "|" +
			p.Nonce + "|" +
			normalizeMetadata(p.Platform) + "|" +
			normalizeMetadata(p.DeviceFamily),
	)
}

type DeviceAuthPayloadV3 struct {
	DeviceID     string
	ClientID     string
	ClientMode   string
	Role         string
	Scopes       []string
	SignedAtMs   int64
	Token        string
	Nonce        string
	Platform     string
	DeviceFamily string
}

func computeDeviceID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

func base64URLNoPad(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func base64URLDecodeNoPad(s string) ([]byte, error) {
	if out, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return out, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func normalizeMetadata(s string) string {
	out := make([]byte, 0, len(s))
	start, end := 0, len(s)
	for start < end && isASCIISpace(s[start]) {
		start++
	}
	for end > start && isASCIISpace(s[end-1]) {
		end--
	}
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		out = append(out, c)
	}
	return string(out)
}

func isASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func joinCSV(items []string) string {
	if len(items) == 0 {
		return ""
	}
	total := 0
	for _, s := range items {
		total += len(s) + 1
	}
	out := make([]byte, 0, total)
	for i, s := range items {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, s...)
	}
	return string(out)
}

func fmtInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

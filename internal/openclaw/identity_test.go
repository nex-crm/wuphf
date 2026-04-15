package openclaw

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateDeviceIdentityPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")

	a, err := LoadOrCreateDeviceIdentity(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(a.DeviceID()) != 64 { // sha256 hex = 64 chars
		t.Fatalf("deviceId hex length: %d", len(a.DeviceID()))
	}
	if _, err := hex.DecodeString(a.DeviceID()); err != nil {
		t.Fatalf("deviceId not hex: %v", err)
	}

	// Reload and confirm the same identity materializes.
	b, err := LoadOrCreateDeviceIdentity(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if a.DeviceID() != b.DeviceID() {
		t.Fatalf("deviceId changed on reload: %q vs %q", a.DeviceID(), b.DeviceID())
	}
	if a.PublicKeyB64URL() != b.PublicKeyB64URL() {
		t.Fatalf("publicKey changed on reload")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity file should be 0600; got %v", info.Mode().Perm())
	}
}

func TestSignVerifiesAgainstPublicKey(t *testing.T) {
	id, err := LoadOrCreateDeviceIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatalf("LoadOrCreateDeviceIdentity: %v", err)
	}
	payload := []byte("v3|abc|gateway-client|backend|operator|operator.admin|123|tok|nonce|darwin|wuphf")
	sigB64 := id.Sign(payload)
	sig, err := base64URLDecodeNoPad(sigB64)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if !ed25519.Verify(id.pub, payload, sig) {
		t.Fatal("signature did not verify with the same identity's public key")
	}
}

func TestBuildDeviceAuthPayloadV3WireFormat(t *testing.T) {
	got := string(BuildDeviceAuthPayloadV3(DeviceAuthPayloadV3{
		DeviceID:     "abc123",
		ClientID:     "gateway-client",
		ClientMode:   "backend",
		Role:         "operator",
		Scopes:       []string{"operator.admin", "operator.read"},
		SignedAtMs:   1776254522461,
		Token:        "tok",
		Nonce:        "n1",
		Platform:     " DARWIN ",
		DeviceFamily: "WUPHF",
	}))
	want := "v3|abc123|gateway-client|backend|operator|operator.admin,operator.read|1776254522461|tok|n1|darwin|wuphf"
	if got != want {
		t.Fatalf("payload wire format\n got:  %q\n want: %q", got, want)
	}
}

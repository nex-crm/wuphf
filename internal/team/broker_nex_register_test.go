package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeNexCLIForBroker drops a shell script at dir/name that mimics
// nex-cli for broker-level tests. Kept here (vs. reusing the nex package
// helper) because that helper is in an internal test file and not exported.
func writeFakeNexCLIForBroker(t *testing.T, dir, name, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake nex-cli: %v", err)
	}
}

// TestNexRegisterEndpoint_Success exercises POST /nex/register against a
// fake nex-cli that returns a canned success output. Asserts the handler
// passes the email through, returns 200 with status=ok, and echoes the
// CLI's stdout back to the wizard.
func TestNexRegisterEndpoint_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	// Isolate PATH so we pick up only the fake nex-cli for this test, and
	// keep the user's real one out of the picture.
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("WUPHF_NO_NEX", "")
	writeFakeNexCLIForBroker(t, dir, "nex-cli", `printf 'api_key=fake-nex-123'`)

	// Isolate config state to a temp HOME so config writes from register
	// (nex-cli would normally emit) don't collide with the user's disk.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".wuphf"), 0o700); err != nil {
		t.Fatal(err)
	}

	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	body := bytes.NewBufferString(`{"email":"founder@example.com"}`)
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.addr+"/nex/register", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nex/register: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /nex/register status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, string(raw))
	}
	if got, _ := out["status"].(string); got != "ok" {
		t.Fatalf("expected status=ok, got %q (body=%s)", got, string(raw))
	}
	if got, _ := out["email"].(string); got != "founder@example.com" {
		t.Fatalf("expected echoed email, got %q", got)
	}
	if got, _ := out["output"].(string); got == "" {
		t.Fatalf("expected non-empty output forwarded from nex-cli, got empty")
	}
}

// TestNexRegisterEndpoint_MissingEmail makes sure we don't shell out when
// the payload lacks an email — the broker must reject with 400 before
// spending an exec on nex-cli.
func TestNexRegisterEndpoint_MissingEmail(t *testing.T) {
	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	body := bytes.NewBufferString(`{"email":""}`)
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.addr+"/nex/register", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nex/register: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing email, got %d", resp.StatusCode)
	}
}

// TestNexRegisterEndpoint_CLIMissing covers the fallback path: when
// nex-cli is not on PATH, nex.Register returns ErrNotInstalled, which the
// handler surfaces as 502. The wizard uses this to flip the signup
// affordance to the external-link mode.
func TestNexRegisterEndpoint_CLIMissing(t *testing.T) {
	// Empty PATH — nex-cli resolution will fail.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "")

	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	body := bytes.NewBufferString(`{"email":"founder@example.com"}`)
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.addr+"/nex/register", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nex/register: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when nex-cli missing, got %d body=%s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, string(raw))
	}
	if got, _ := out["status"].(string); got != "error" {
		t.Fatalf("expected status=error, got %q", got)
	}
	if got, _ := out["error"].(string); got == "" {
		t.Fatalf("expected non-empty error message when nex-cli missing")
	}
}

// TestNexRegisterEndpoint_RejectsGET ensures we only accept POST — a GET
// would indicate the caller wiring the wrong verb, worth failing fast.
func TestNexRegisterEndpoint_RejectsGET(t *testing.T) {
	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.addr+"/nex/register", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /nex/register: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

// TestConfigEndpoint_NexAPIKeyPersists exercises the wizard's new Nex API
// key input: POST /config with api_key should persist to disk and be
// reflected on subsequent GET /config as api_key_set=true (the broker
// returns secrets as booleans on read to avoid leakage).
func TestConfigEndpoint_NexAPIKeyPersists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".wuphf"), 0o700); err != nil {
		t.Fatal(err)
	}

	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	// POST /config with api_key — mimics the wizard's new Nex API key input.
	payload := `{"api_key":"nex-test-key-xyz","openai_api_key":"sk-test-openai","anthropic_api_key":"sk-ant-test"}`
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.addr+"/config", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /config: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /config status=%d", resp.StatusCode)
	}

	// GET /config — api_key_set should be true, openai_key_set true,
	// anthropic_key_set true. The plaintext values are NOT returned.
	req, _ = http.NewRequest(http.MethodGet, "http://"+b.addr+"/config", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /config: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, string(raw))
	}
	if set, _ := got["api_key_set"].(bool); !set {
		t.Fatalf("expected api_key_set=true after POST, got %v (body=%s)", got["api_key_set"], string(raw))
	}
	if set, _ := got["openai_key_set"].(bool); !set {
		t.Fatalf("expected openai_key_set=true, got %v", got["openai_key_set"])
	}
	if set, _ := got["anthropic_key_set"].(bool); !set {
		t.Fatalf("expected anthropic_key_set=true, got %v", got["anthropic_key_set"])
	}
}

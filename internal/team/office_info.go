package team

// office_info.go writes and reads the office.json sidecar that sits next to
// office.pid. It records the running broker's web UI URL so any front-end — the
// desktop shell, the CLI, a browser — can DISCOVER and ATTACH to a broker
// already serving this workspace instead of booting a second one. One broker
// per workspace is the invariant (killStaleBroker would otherwise kill-9 a peer
// on the shared port); this file is how front-ends honor it.
//
// This is an INTERNAL, single-machine, unstable contract. The only stable part
// is the {schemaVersion, webURL} envelope: every future version MUST preserve
// those two fields so an older binary never mistakes a newer office for "no
// office" (which would make it killStaleBroker a live peer and clobber its file).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// officeInfoSchemaVersion is the current writer version. Readers attach on any
// version >= 1 (see the envelope note above); the value is informational + lets
// future readers branch on optional fields.
const officeInfoSchemaVersion = 1

// officeInfo is the office.json payload. Unexported: the public surface is
// RunningOfficeURL's (string, bool), not this struct.
type officeInfo struct {
	SchemaVersion int    `json:"schemaVersion"`
	PID           int    `json:"pid"`
	WebURL        string `json:"webURL"`
	BrokerURL     string `json:"brokerURL"`
	StartedAt     string `json:"startedAt"`
}

func officeInfoPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "office.json")
	}
	return filepath.Join(home, ".wuphf", "team", "office.json")
}

// writeOfficeInfo records the running broker's URLs for the active workspace.
// It writes to a temp file then renames into place so a concurrent reader sees
// either the old or the new file, never a torn one — os.Rename replaces the
// destination on POSIX (atomic) and via MoveFileEx on Windows (replaces, and is
// atomic on NTFS for our single-writer case). On the rare Windows-locked-target
// failure the write errors and is treated as best-effort; the sidecar then
// self-heals (RunningOfficeURL re-probes, a future boot rewrites it).
func writeOfficeInfo(webURL, brokerURL string) error {
	path := officeInfoPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(officeInfo{
		SchemaVersion: officeInfoSchemaVersion,
		PID:           os.Getpid(),
		WebURL:        strings.TrimRight(strings.TrimSpace(webURL), "/"),
		BrokerURL:     strings.TrimRight(strings.TrimSpace(brokerURL), "/"),
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "office-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func clearOfficeInfo() error {
	if err := os.Remove(officeInfoPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ClearRunningOffice removes the office.json sidecar for the active workspace.
// A front-end that OWNED the in-process broker (the desktop shell when it booted
// rather than attached) calls this on shutdown so the next launch doesn't pay a
// probe to a dead URL. Best-effort; a leftover sidecar self-corrects because a
// future boot overwrites it and readers re-probe.
func ClearRunningOffice() { _ = clearOfficeInfo() }

func readOfficeInfo() (officeInfo, error) {
	raw, err := os.ReadFile(officeInfoPath())
	if err != nil {
		return officeInfo{}, err
	}
	var info officeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return officeInfo{}, err
	}
	return info, nil
}

// RunningOfficeURL reports the web UI URL of a broker already serving the active
// workspace, if one is alive, on loopback, and identifiably WUPHF. Front-ends
// call it BEFORE booting: a non-empty URL means "attach here", ("", false) means
// "no peer — boot your own".
//
// It does NOT clear a stale sidecar: a reader whose probe times out must not
// delete a file a live peer may have just written. Stale files self-correct —
// the next boot overwrites office.json, and clean shutdown clears it.
func RunningOfficeURL() (string, bool) {
	info, err := readOfficeInfo()
	if err != nil {
		return "", false
	}
	// Frozen envelope: attach on any current-or-newer schema. Never treat a
	// newer office as "absent" — that would make this binary killStaleBroker a
	// live peer it simply doesn't understand.
	if info.SchemaVersion < officeInfoSchemaVersion || info.WebURL == "" {
		return "", false
	}
	if info.PID == os.Getpid() {
		return "", false // our own sidecar
	}
	// webURL drives where the WebView / browser navigates; only ever trust a
	// loopback origin so a planted office.json can't point us at a remote host.
	if !isLoopbackURL(info.WebURL) {
		return "", false
	}
	if !officeIsWuphf(info.WebURL) {
		return "", false
	}
	return info.WebURL, true
}

// isLoopbackURL reports whether the URL's host is a loopback address.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// officeIsWuphf returns true if a live WUPHF web UI answers at webURL. It is the
// liveness signal (cross-platform, no OS process syscalls) AND an identity check
// — a non-WUPHF service that grabbed a recycled loopback port must not be
// attached to. Redirects are not followed (a real broker serves its UI at "/").
func officeIsWuphf(webURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(webURL, "/")+"/", nil)
	if err != nil {
		return false
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return strings.Contains(strings.ToLower(string(body)), "wuphf")
}

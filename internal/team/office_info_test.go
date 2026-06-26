package team

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// wuphfServer is an httptest server that looks like a WUPHF web UI (loopback +
// "wuphf" in the body) so officeIsWuphf accepts it.
func wuphfServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<title>WUPHF - Slack for AI employees</title>"))
	}))
}

func TestWriteReadOfficeInfoRoundtrip(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	if err := writeOfficeInfo("http://127.0.0.1:1234/", "http://127.0.0.1:5678/"); err != nil {
		t.Fatalf("writeOfficeInfo: %v", err)
	}
	got, err := readOfficeInfo()
	if err != nil {
		t.Fatalf("readOfficeInfo: %v", err)
	}
	if got.SchemaVersion != officeInfoSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", got.SchemaVersion, officeInfoSchemaVersion)
	}
	if got.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", got.PID, os.Getpid())
	}
	if got.WebURL != "http://127.0.0.1:1234" {
		t.Errorf("webURL = %q, want http://127.0.0.1:1234 (trailing slash trimmed)", got.WebURL)
	}
}

func TestRunningOfficeURL(t *testing.T) {
	t.Run("no sidecar means no running office", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		if url, ok := RunningOfficeURL(); ok {
			t.Errorf("RunningOfficeURL() = %q, true; want \"\", false", url)
		}
	})

	t.Run("our own sidecar is not a peer to attach to", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		if err := writeOfficeInfo("http://127.0.0.1:9", "http://127.0.0.1:9"); err != nil {
			t.Fatalf("writeOfficeInfo: %v", err)
		}
		if url, ok := RunningOfficeURL(); ok {
			t.Errorf("RunningOfficeURL() = %q, true; want \"\", false (self pid)", url)
		}
	})

	t.Run("live reachable WUPHF peer on loopback is attachable", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		srv := wuphfServer(t)
		defer srv.Close()
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: officeInfoSchemaVersion,
			PID:           foreignPID(),
			WebURL:        srv.URL, // httptest binds 127.0.0.1 (loopback)
		})
		url, ok := RunningOfficeURL()
		if !ok || url != srv.URL {
			t.Errorf("RunningOfficeURL() = %q, %v; want %q, true", url, ok, srv.URL)
		}
	})

	t.Run("newer schema still attaches (frozen envelope, forward-compat)", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		srv := wuphfServer(t)
		defer srv.Close()
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: officeInfoSchemaVersion + 99, // a future writer
			PID:           foreignPID(),
			WebURL:        srv.URL,
		})
		if _, ok := RunningOfficeURL(); !ok {
			t.Error("a newer-schema reachable office must still be attachable, not treated as absent")
		}
	})

	t.Run("non-loopback webURL is rejected without probing", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: officeInfoSchemaVersion,
			PID:           foreignPID(),
			WebURL:        "http://10.1.2.3:8080", // a planted remote host
		})
		if url, ok := RunningOfficeURL(); ok {
			t.Errorf("RunningOfficeURL() = %q, true; want \"\", false (non-loopback must never be attached)", url)
		}
	})

	t.Run("a non-WUPHF service on the recorded port is not attached", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello from some other dev server"))
		}))
		defer foreign.Close()
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: officeInfoSchemaVersion,
			PID:           foreignPID(),
			WebURL:        foreign.URL,
		})
		if url, ok := RunningOfficeURL(); ok {
			t.Errorf("RunningOfficeURL() = %q, true; want \"\", false (foreign service is not a WUPHF office)", url)
		}
	})

	t.Run("dead peer is not attachable", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: officeInfoSchemaVersion,
			PID:           foreignPID(),
			WebURL:        "http://127.0.0.1:" + freeClosedPort(t),
		})
		if url, ok := RunningOfficeURL(); ok {
			t.Errorf("RunningOfficeURL() = %q, true; want \"\", false (dead peer)", url)
		}
	})

	t.Run("schema below floor is ignored", func(t *testing.T) {
		t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
		srv := wuphfServer(t)
		defer srv.Close()
		writeRawOfficeInfo(t, officeInfo{
			SchemaVersion: 0,
			PID:           foreignPID(),
			WebURL:        srv.URL,
		})
		if _, ok := RunningOfficeURL(); ok {
			t.Error("schema 0 (below floor) must be ignored")
		}
	})
}

// foreignPID returns a PID that is not this process, so the self-skip in
// RunningOfficeURL does not fire (liveness is decided by the HTTP probe).
func foreignPID() int { return os.Getpid() + 1 }

func writeRawOfficeInfo(t *testing.T, info officeInfo) {
	t.Helper()
	path := officeInfoPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// freeClosedPort binds an ephemeral port then closes it, returning a port that
// is (almost certainly) not listening — a deterministic "dead peer" target.
func freeClosedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return strconv.Itoa(port)
}

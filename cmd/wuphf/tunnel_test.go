package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCloudflaredURLPattern locks the regex against both the boxed banner
// cloudflared currently emits and a representative log line shape, so a
// future log-format tweak that breaks parsing fails here loudly instead of
// silently making the "Start public tunnel" button hang.
func TestCloudflaredURLPattern(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "boxed banner",
			line: "2024-01-15T10:30:45Z INF |  https://winter-soft-banana-42.trycloudflare.com  |",
			want: "https://winter-soft-banana-42.trycloudflare.com",
		},
		{
			name: "single-word host",
			line: "INF Your tunnel is ready: https://abc.trycloudflare.com",
			want: "https://abc.trycloudflare.com",
		},
		{
			name: "no match (bare domain)",
			line: "INF Cloudflare quick tunnels at trycloudflare.com",
			want: "",
		},
		{
			name: "no match (different host)",
			line: "INF https://example.com",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cloudflaredURLPattern.FindString(tc.line)
			if got != tc.want {
				t.Fatalf("FindString(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestTunnelJoinURLTrimsTrailingSlash(t *testing.T) {
	got := tunnelJoinURL("https://abc.trycloudflare.com/", "tok")
	want := "https://abc.trycloudflare.com/join/tok"
	if got != want {
		t.Fatalf("tunnelJoinURL = %q, want %q", got, want)
	}
}

func TestScanCloudflaredOutputFindsURLAndKeepsTail(t *testing.T) {
	urlCh := make(chan string, 1)
	tailCh := make(chan []string, 1)
	input := strings.NewReader(strings.Join([]string{
		"line 1",
		"line 2",
		"INF Your tunnel is ready: https://abc.trycloudflare.com",
		"line 4",
	}, "\n"))

	scanCloudflaredOutput(input, urlCh, tailCh)

	select {
	case got := <-urlCh:
		if got != "https://abc.trycloudflare.com" {
			t.Fatalf("url = %q, want cloudflared URL", got)
		}
	default:
		t.Fatal("expected cloudflared URL to be sent")
	}

	tail := <-tailCh
	if len(tail) != 4 || tail[0] != "line 1" || tail[3] != "line 4" {
		t.Fatalf("tail = %#v, want all scanned lines", tail)
	}
}

func TestWaitForTunnelURLReturnsTailWhenProcessExits(t *testing.T) {
	urlCh := make(chan string)
	close(urlCh)
	tailCh := make(chan []string, 1)
	tailCh <- []string{"last cloudflared line"}

	gotURL, tail, err := waitForTunnelURL(context.Background(), urlCh, tailCh, time.Second)
	if err == nil {
		t.Fatal("expected error when cloudflared exits without URL")
	}
	if gotURL != "" {
		t.Fatalf("url = %q, want empty", gotURL)
	}
	if len(tail) != 1 || tail[0] != "last cloudflared line" {
		t.Fatalf("tail = %#v, want last cloudflared line", tail)
	}
}

// TestIsTransientQuickTunnelFailure locks in which cloudflared bring-up
// failure modes are eligible for an automatic respawn. The 1101 / 500
// signature is the real-world hit that motivated the retry path: when
// trycloudflare.com's QuickTunnel API returns a Cloudflare HTML error
// page, cloudflared logs an unmarshal error and exits in <1s. Retrying
// after a brief backoff routinely succeeds.
func TestIsTransientQuickTunnelFailure(t *testing.T) {
	cases := []struct {
		name string
		tail []string
		want bool
	}{
		{
			name: "QuickTunnel response unmarshal error (1101)",
			tail: []string{
				"INF Requesting new quick Tunnel on trycloudflare.com...",
				`ERR Error unmarshaling QuickTunnel response: error code: 1101 error="invalid character 'e' looking for beginning of value" status_code="500 Internal Server Error"`,
				"failed to unmarshal quick Tunnel: invalid character 'e' looking for beginning of value",
			},
			want: true,
		},
		{
			name: "Cloudflare 502",
			tail: []string{
				"INF Requesting new quick Tunnel on trycloudflare.com...",
				"ERR upstream returned 502 Bad Gateway",
			},
			want: true,
		},
		{
			name: "permanent: missing binary banner only",
			tail: []string{
				"2024-01-15 INF Thank you for trying Cloudflare Tunnel.",
			},
			want: false,
		},
		{
			name: "permanent: connection refused",
			tail: []string{
				"INF Connecting to edge",
				"ERR dial tcp: connection refused",
			},
			want: false,
		},
		{
			name: "empty tail",
			tail: nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTransientQuickTunnelFailure(tc.tail)
			if got != tc.want {
				t.Fatalf("isTransientQuickTunnelFailure(%v) = %v, want %v", tc.tail, got, tc.want)
			}
		})
	}
}

// TestStopClosesStartCancel proves that stop() wakes an in-flight start()'s
// retry-backoff select. Without this wiring, a click on Stop during the 3s
// inter-attempt wait would block the user-perceived state until the timer
// fires. The retry loop selects on startCancel; this test guards the only
// thing that closes it.
func TestStopClosesStartCancel(t *testing.T) {
	c := newWebTunnelController()
	c.mu.Lock()
	startCancel := make(chan struct{})
	c.startCancel = startCancel
	c.starting = true
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		<-startCancel
		close(done)
	}()

	_ = c.stop()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop() did not close startCancel within 1s — backoff would not be cancellable")
	}

	c.mu.Lock()
	if c.startCancel != nil {
		t.Fatalf("stop() left c.startCancel non-nil; subsequent stop() would panic on close()")
	}
	if c.starting {
		t.Fatalf("stop() left c.starting=true; next Start would refuse with \"already in progress\"")
	}
	c.mu.Unlock()
}

// TestCloudflaredMissingMessageMentionsInstall guards the user-facing string
// — the tunnel button is the entry point for non-technical hosts, so the
// failure mode they hit first must include an install command rather than a
// stack trace.
func TestCloudflaredMissingMessageMentionsInstall(t *testing.T) {
	msg := cloudflaredMissingMessage()
	if !strings.Contains(msg, "cloudflared") {
		t.Fatalf("missing message does not mention cloudflared: %q", msg)
	}
	if !strings.Contains(msg, "install") && !strings.Contains(msg, "Install") {
		t.Fatalf("missing message has no install hint: %q", msg)
	}
}

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

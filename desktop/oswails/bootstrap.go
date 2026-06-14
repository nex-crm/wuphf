//go:build desktop

package main

import (
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// targetPlaceholder is replaced with the resolved office origin (the attached
// peer's URL, or the freshly-booted local URL) in the embedded bootstrap page
// at startup. Keep it in sync with frontend/dist/index.html.
const targetPlaceholder = "__WUPHF_TARGET__"

// bootstrapTargetMiddleware serves the embedded bootstrap page (with the office
// origin templated in) for the document root, and lets every other request fall
// through to the default asset server. The bootstrap polls the origin and
// redirects the WebView to it once it answers.
func bootstrapTargetMiddleware(targetURL string) assetserver.Middleware {
	page := renderBootstrap(targetURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(page)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func renderBootstrap(targetURL string) []byte {
	target := strings.TrimRight(strings.TrimSpace(targetURL), "/") + "/"
	raw, err := assets.ReadFile("frontend/dist/index.html")
	if err != nil {
		// Minimal fallback so a missing embed still reaches the office.
		raw = []byte(
			`<!doctype html><meta charset="utf-8"><title>WUPHF</title>` +
				`<script>location.replace("` + targetPlaceholder + `")</script>`,
		)
	}
	return []byte(strings.ReplaceAll(string(raw), targetPlaceholder, target))
}

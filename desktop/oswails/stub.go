//go:build !desktop

// This file keeps `go build ./...`, `go vet ./...`, and the default test build
// working without the `desktop` build tag (and its Wails/CGO webview deps).
// The real desktop shell lives in the //go:build desktop files and is built
// with `wails build -tags desktop` (see README.md "Build").
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr,
		"wuphf desktop shell: build with `wails build -tags desktop` (see desktop/oswails/README.md)")
	os.Exit(1)
}

//go:build desktop

// Command oswails is the WUPHF desktop shell: a single Go binary that attaches a
// native Wails window to the user's office. If a broker is already serving the
// active workspace (a running CLI `wuphf web` or another front-end), the window
// ATTACHES to it; otherwise the shell boots the existing broker + web UI
// in-process (no sidecar). One broker per workspace is the invariant.
//
// This is the only package permitted to import Wails (see README.md and
// scripts/check-wails-boundary.sh).
package main

import (
	"context"
	"embed"
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/nex-crm/wuphf/internal/team"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func init() {
	// Cocoa requires the NSApplication run loop on the process's main thread.
	// Pin the main goroutine before any broker boot (which spawns goroutines)
	// so wails.Run lands on the main thread and the window actually appears.
	runtime.LockOSThread()
}

//go:embed all:frontend/dist
var assets embed.FS

// freePort reserves an ephemeral loopback port. ServeWebUI binds a literal
// 127.0.0.1:<port> (it does not accept :0), so the shell picks the port itself
// and templates it into the bootstrap page.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func main() {
	// Resolve the office to show. The shell shares the user's active workspace
	// (the same office an unqualified `wuphf` CLI uses) — WUPHF_RUNTIME_HOME is
	// intentionally NOT overridden here.
	target, attached := team.RunningOfficeURL()
	if attached {
		// A broker is already serving this workspace — attach the window to it
		// instead of booting a second broker (which killStaleBroker would race).
		fmt.Fprintln(os.Stderr, "attaching to running office at", target)
	} else {
		// No peer — boot the existing broker + web UI in-process on a free port,
		// off the main thread so the window opens immediately. The bootstrap
		// page polls the loopback origin and redirects once it answers.
		port, err := freePort()
		if err != nil {
			fmt.Fprintln(os.Stderr, "allocate port:", err)
			os.Exit(1)
		}
		target = fmt.Sprintf("http://127.0.0.1:%d", port)
		go func() {
			launcher, err := team.NewLauncher("")
			if err != nil {
				fmt.Fprintln(os.Stderr, "launcher:", err)
				os.Exit(1)
			}
			launcher.SetNoOpen(true)
			if err := launcher.PreflightWeb(); err != nil {
				fmt.Fprintln(os.Stderr, "preflight:", err)
				os.Exit(1)
			}
			if err := launcher.LaunchWeb(port); err != nil {
				fmt.Fprintln(os.Stderr, "launch:", err)
				os.Exit(1)
			}
		}()
	}

	var appCtx context.Context

	if err := wails.Run(&options.App{
		Title:            "WUPHF",
		Width:            1400,
		Height:           900,
		BackgroundColour: &options.RGBA{R: 11, G: 11, B: 13, A: 1},
		OnStartup:        func(ctx context.Context) { appCtx = ctx },
		OnShutdown: func(context.Context) {
			// If we booted the broker in-process (rather than attaching to a
			// running peer), clear the office.json sidecar so the next launch
			// doesn't probe a now-dead URL. The broker dies with the process.
			if !attached {
				team.ClearRunningOffice()
			}
		},
		// One desktop instance per machine: a second launch focuses the running
		// window instead of spawning a competing process.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "ai.nex.wuphf.desktop",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				if appCtx != nil {
					wailsruntime.WindowUnminimise(appCtx)
					wailsruntime.WindowShow(appCtx)
				}
			},
		},
		// Hand the WebView a real http origin: SSE / WebSocket / WebAuthn all
		// need one, and the wails:// custom scheme cannot carry a WebSocket. The
		// middleware templates the resolved office origin into the bootstrap.
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: bootstrapTargetMiddleware(target),
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, "wails:", err)
		os.Exit(1)
	}
}

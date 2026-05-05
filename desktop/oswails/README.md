# Wails OS Boundary

This is the only Go package allowed to import
`github.com/wailsapp/wails/v2/...` or `github.com/wailsapp/wails/v3/...`.

All app data must stay on the existing HTTP/SSE/WebSocket loopback transport in
`internal/team/broker_web_proxy.go`.

Wails events are reserved for OS verbs only:

- native notifications
- tray
- dock badge
- deep-link
- autostart
- file pickers
- single-instance lock

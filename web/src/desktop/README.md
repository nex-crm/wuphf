# Wails Web Boundary

This is the only TS-side web directory allowed to import Wails runtime modules
or generated bindings such as `@wails/runtime`, `@wailsapp/runtime`,
`wails-bindings`, and `wailsjs/*`.

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

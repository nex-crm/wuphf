// Package channelui hosts the Bubble Tea TUI for the wuphf "channel"
// surface — channel feed, sidebar, thread panel, composer, splash, and
// the broker-backed model that drives them.
//
// The package was extracted from the cmd/wuphf binary's main package to
// give the channel layer a clearly defined boundary. Code lives under
// cmd/wuphf/ rather than internal/ because it is binary-private; the
// broker-side internal/channel package owns the cross-process channel
// store types and is intentionally distinct from this UI layer.
//
// Extraction is staged across multiple PRs. As of this scaffolding PR
// only the composer history primitives live here; subsequent PRs will
// move the renderers, workspace cluster, sidebar, broker integrations,
// and the channelModel itself in dependency order.
package channelui

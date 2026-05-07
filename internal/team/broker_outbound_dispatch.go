package team

// broker_outbound_dispatch.go owns the per-transport outbound dispatcher loop
// that the launcher runs alongside Run for adapters that publish to an
// external surface (Telegram today, future channel-bound adapters tomorrow).
//
// This honors the transport.Transport contract intent — "The Host calls Send
// from a per-transport worker goroutine (not the broker's hot path)" — by
// moving the polling lifecycle out of each adapter and into a shared loop the
// Host owns. Adapters supply two thin functions:
//
//   formatter(channelMessage) (transport.Outbound, bool)
//       Adapter-specific conversion. Returns ok=false to skip a message
//       (e.g. the channel slug has no chat mapped). No side effects.
//
//   sender(ctx, transport.Outbound) error
//       The adapter's Transport.Send method (passed by value as a method
//       expression: t.Send) — actually delivers to the wire.
//
// The dispatcher polls broker.ExternalQueue(name) every interval, formats
// each message, and calls sender. Send errors are logged and the loop
// continues to the next message — at-least-once delivery semantics already
// live in the broker's ExternalQueue (which marks messages as delivered on
// dequeue), so a transient send failure drops the message rather than
// retrying. This matches the prior in-adapter behavior.

import (
	"context"
	"log"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// outboundDispatchInterval is the polling cadence. Matches the prior
// in-adapter loop (2s) so behavior is byte-identical at the SLO level for
// the existing telegram path. If we ever need pushed delivery (lower
// latency), the broker grows a per-provider notify channel and the
// dispatcher selects on it instead of a ticker.
const outboundDispatchInterval = 2 * time.Second

// runOutboundDispatcher polls the broker's outbound queue for `name` and
// hands each message to the adapter via formatter+sender. Returns when ctx
// is cancelled. The caller is responsible for goroutine lifecycle (typically
// launcher_transports.go starts this in a goroutine alongside Transport.Run).
//
// formatter must be safe to call from a single goroutine (no caller locking
// is provided around it). sender is the adapter's Send method; the
// dispatcher passes ctx through so a Send blocking past adapter timeout can
// be unwound on shutdown.
func runOutboundDispatcher(
	ctx context.Context,
	broker *Broker,
	name string,
	formatter func(channelMessage) (transport.Outbound, bool),
	sender func(context.Context, transport.Outbound) error,
) error {
	if broker == nil {
		return nil
	}
	ticker := time.NewTicker(outboundDispatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		msgs := broker.ExternalQueue(name)
		if len(msgs) > 0 {
			log.Printf("[transport] %s: outbound queue: %d message(s)", name, len(msgs))
		}
		for _, msg := range msgs {
			out, ok := formatter(msg)
			if !ok {
				continue
			}
			if err := sender(ctx, out); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("[transport] %s: outbound send error for %q: %v", name, out.Binding.ChannelSlug, err)
				continue
			}
		}
	}
}

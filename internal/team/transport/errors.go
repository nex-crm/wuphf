package transport

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by [Host] methods. Adapters should use [errors.Is]
// to test for these; the Host wraps them with context via fmt.Errorf("%w").
//
// See docs/ADD-A-TRANSPORT.md §Error contract for remediation guidance.
var (
	// ErrParticipantUnknown is returned by [Host.ReceiveMessage] when the
	// adapter did not call [Host.UpsertParticipant] before sending the message.
	// Remediation: call UpsertParticipant(ctx, p, b) immediately when a new
	// external identity first contacts the adapter, then retry ReceiveMessage.
	ErrParticipantUnknown = errors.New("transport: participant unknown: call UpsertParticipant first")

	// ErrBindingChannelMissing is returned by [Host.ReceiveMessage] or at
	// adapter registration time when the channel declared in [Binding.ChannelSlug]
	// does not exist in the broker. Remediation: verify the channel slug in
	// the adapter's configuration and reconnect the Telegram channel via the
	// web UI.
	ErrBindingChannelMissing = errors.New("transport: binding channel not found")

	// ErrAdapterCrashed is returned by the Host when a broker panic was
	// recovered during message dispatch. The adapter should log the error and
	// attempt reconnection via its normal backoff path; the broker has recovered
	// and is still running.
	ErrAdapterCrashed = errors.New("transport: adapter dispatch panicked and was recovered")

	// ErrSendTimeout is returned by the Host worker when [Transport.Send] did
	// not return within the configured timeout. The message has been dropped.
	// Remediation: check the adapter's upstream connectivity and reduce message
	// payload size if the upstream API has a latency limit.
	ErrSendTimeout = errors.New("transport: send timed out: message dropped")

	// ErrHealthDegraded is returned by [Host.ReceiveMessage] when the broker
	// has marked the adapter as degraded (e.g. repeated send failures) and is
	// refusing further inbound messages until the adapter self-heals. The
	// adapter should pause polling, run its reconnect logic, and retry.
	ErrHealthDegraded = errors.New("transport: adapter health degraded: pause and reconnect")

	// ErrRegistrationConflict is returned by [Host.UpsertParticipant] when
	// the (AdapterName, Key) pair maps to a different member slug than already
	// registered. This typically indicates the adapter restarted with a new key
	// assignment while the old mapping still exists. Remediation: use stable,
	// content-addressed keys (e.g. upstream session ID) rather than ephemeral
	// identifiers.
	ErrRegistrationConflict = errors.New("transport: participant key conflicts with existing member slug")
)

// ParticipantUnknownError wraps [ErrParticipantUnknown] with the adapter name
// and participant key so the log message points directly at the cause.
type ParticipantUnknownError struct {
	AdapterName string
	Key         string
}

func (e *ParticipantUnknownError) Error() string {
	return fmt.Sprintf("transport: participant unknown for adapter %q key %q: call UpsertParticipant first", e.AdapterName, e.Key)
}

func (e *ParticipantUnknownError) Is(target error) bool {
	return target == ErrParticipantUnknown
}

// BindingChannelMissingError wraps [ErrBindingChannelMissing] with the channel
// slug that was not found.
type BindingChannelMissingError struct {
	ChannelSlug string
}

func (e *BindingChannelMissingError) Error() string {
	return fmt.Sprintf("transport: binding channel %q not found in broker", e.ChannelSlug)
}

func (e *BindingChannelMissingError) Is(target error) bool {
	return target == ErrBindingChannelMissing
}

// RegistrationConflictError wraps [ErrRegistrationConflict] with the
// conflicting slugs.
type RegistrationConflictError struct {
	AdapterName     string
	Key             string
	ExistingSlug    string
	ConflictingSlug string
}

func (e *RegistrationConflictError) Error() string {
	return fmt.Sprintf(
		"transport: adapter %q key %q already mapped to member %q; cannot remap to %q",
		e.AdapterName, e.Key, e.ExistingSlug, e.ConflictingSlug,
	)
}

func (e *RegistrationConflictError) Is(target error) bool {
	return target == ErrRegistrationConflict
}

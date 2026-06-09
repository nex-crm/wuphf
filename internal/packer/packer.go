package packer

import "fmt"

// Pack runs the read-side pipeline: Gather -> Classify -> Budget -> Render. It
// returns a PackedDelegation ready for Deliver, plus the egress audit, or
// ErrEnvelopeHeld if a critical envelope field could not be sanitized.
// audienceTier is the trust tier the content is classified against — pass the
// LEAST-trusted reader present for a shared thread post, or the target's own
// tier for an ephemeral/DM delivery (see Audience).
func Pack(
	brain BrainHandle,
	policy EgressPolicy,
	sc SecretScanner,
	req ContextRequest,
	opts GatherOptions,
	audienceTier BotTrust,
) (PackedDelegation, []ItemAudit, error) {
	raw, err := Gather(brain, req, opts)
	if err != nil {
		return PackedDelegation{}, nil, fmt.Errorf("pack gather: %w", err)
	}
	bundle, audit, err := Classify(raw, audienceTier, policy, sc)
	if err != nil {
		return PackedDelegation{}, audit, err
	}
	bundle = Budget(bundle, req.Target)
	packed := Render(bundle, req, audit)
	return packed, audit, nil
}

// Audience computes the trust tier to classify a delivery against. For a shared
// channel thread post the content is visible to the least-trusted reader present
// (and to unknown future joiners, so a public thread should be treated as
// untrusted unless its history is provably closed). For an ephemeral/DM delivery
// the audience is the target bot alone. BotTrust is ordered least-to-most
// trusted, so the audience is the minimum of target and least-trusted-present.
func Audience(target, leastTrustedPresent BotTrust, targetOnly bool) BotTrust {
	if targetOnly {
		return target
	}
	if leastTrustedPresent < target {
		return leastTrustedPresent
	}
	return target
}

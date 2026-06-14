package packer

import "fmt"

// DeliveryAudience describes who will be able to read the delegation, so Pack can
// compute the classification tier itself rather than trusting the caller to pass
// the right one. For a shared-channel thread post the content is visible to the
// least-trusted reader present (and to unknown future joiners). For an
// ephemeral/DM delivery only the target reads it.
type DeliveryAudience struct {
	// TargetOnly is true for ephemeral/DM delivery (audience == the target bot).
	TargetOnly bool
	// LeastTrustedPresent is the least-trusted reader in the channel. Ignored
	// when TargetOnly. A public thread whose history is not provably closed
	// should pass BotUntrusted here.
	LeastTrustedPresent BotTrust
}

// Pack runs the read-side pipeline: Gather -> Classify -> budget -> render. It
// computes the classification audience INTERNALLY from aud + the target tier
// (the minimum), so a caller cannot accidentally classify a shared-thread post
// against the target's own higher tier. It returns a sealed PackedDelegation
// ready for Deliver, plus the egress audit, or ErrEnvelopeHeld if a critical
// envelope field could not be sanitized.
func Pack(
	brain BrainHandle,
	policy EgressPolicy,
	sc SecretScanner,
	req ContextRequest,
	opts GatherOptions,
	aud DeliveryAudience,
) (PackedDelegation, []ItemAudit, error) {
	audienceTier := Audience(req.Target.Trust, aud.LeastTrustedPresent, aud.TargetOnly)

	// Retrieve against the AUDIENCE tier, not the target tier: a downgraded
	// audience must not even pull first-party content into the pipeline.
	raw, err := Gather(brain, req, opts, audienceTier)
	if err != nil {
		return PackedDelegation{}, nil, fmt.Errorf("pack gather: %w", err)
	}
	bundle, audit, err := Classify(raw, audienceTier, policy, sc)
	if err != nil {
		return PackedDelegation{}, audit, err
	}
	bundle = budget(bundle, req.Target)
	packed := render(bundle, req, audit, audienceTier)
	return packed, audit, nil
}

// Audience computes the trust tier to classify a delivery against. BotTrust is
// ordered least-to-most trusted, so for a shared thread the audience is the
// minimum of target and least-trusted-present; for a target-only (DM/ephemeral)
// delivery it is the target alone.
func Audience(target, leastTrustedPresent BotTrust, targetOnly bool) BotTrust {
	if targetOnly {
		return target
	}
	if leastTrustedPresent < target {
		return leastTrustedPresent
	}
	return target
}

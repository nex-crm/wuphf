package packer

// EgressPolicy decides, per candidate kind and audience tier, whether content
// may leave the brain. It is intentionally conservative and label-free for v1;
// per-item sensitivity labels are a later refinement that only WIDENS first-party
// reach. The audience tier passed in is the LEAST-trusted reader the content
// will reach, not necessarily the target bot (a shared-thread post is visible to
// everyone in the channel and to future joiners).
type EgressPolicy interface {
	Version() int
	Classify(kind ItemKind, audience BotTrust) ExportClass
}

// DefaultEgressPolicy encodes the spec's per-tier table:
//
//	BotUntrusted  -> ask/returnpact/guard/plan REDACTED; everything else DENIED.
//	                 No free wiki/learning. Raw task body never exported wholesale.
//	BotFirstParty -> the above + task-scoped learnings + task-linked wiki refs +
//	                 roster + skill hints, all REDACTED. Raw task body still DENIED.
//	BotHosted     -> everything ALLOWED (it already gets the full push-side
//	                 injection; the packer is permissive if ever called for it).
type DefaultEgressPolicy struct{ version int }

// NewDefaultEgressPolicy returns the default policy stamped with a version that
// every InjectionRecord references for audit.
func NewDefaultEgressPolicy(version int) DefaultEgressPolicy {
	return DefaultEgressPolicy{version: version}
}

// Version reports the policy version.
func (p DefaultEgressPolicy) Version() int { return p.version }

// Classify returns the export class for a candidate kind at an audience tier.
func (p DefaultEgressPolicy) Classify(kind ItemKind, audience BotTrust) ExportClass {
	switch audience {
	case BotHosted:
		return ExportAllowed
	case BotFirstParty:
		switch kind {
		case KindAsk, KindReturnPact, KindGuard, KindPlan,
			KindLearning, KindWiki, KindRoster, KindSkill:
			return ExportRedacted
		default:
			// KindTask (raw free-form body) and anything unknown: never exported
			// wholesale. Redaction catches credential tokens, not confidential
			// prose / source / customer data.
			return ExportDenied
		}
	default: // BotUntrusted — the default for anything externally originated.
		switch kind {
		case KindAsk, KindReturnPact, KindGuard, KindPlan:
			return ExportRedacted
		default:
			// No free wiki/learning retrieval; raw task body denied.
			return ExportDenied
		}
	}
}

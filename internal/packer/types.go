// Package packer implements the Slack inbound context-packer: the CEO-membrane
// component that does task-scoped retrieval against the office brain and injects
// egress-classified, redacted context into the Slack @-mention a foreign bot
// reads. It is the one place internal brain content crosses into a foreign LLM
// we do not control, so the egress boundary (Classify + EgressPolicy +
// redaction) is the security core. See docs/specs/slack-context-packer.md.
//
// The package depends only on internal/scanner and the standard library. The
// brain, the delivery transport, the snapshot validator, and the audit sink are
// all interfaces (the cloud-portability seams), so the same core runs in the OSS
// self-hosted broker and in the Nex cloud multi-tenant host without a fork.
package packer

// --- Trust, identity, data handling ---

// BotTrust is the trust tier a bot is granted. It is DERIVED partly from data
// handling, not just origin: a first-party bot that forwards prompts to a
// third-party LLM is not automatically trusted. BotUntrusted is the default for
// anything externally originated.
type BotTrust int

const (
	BotUntrusted  BotTrust = iota // default for anything externally originated
	BotFirstParty                 // in-house, company-owned workspace, known data handling
	BotHosted                     // WUPHF-hosted agent (also gets push-side injection)
)

// ReadScope is how much of the conversation a bot reads. Upgraded to ReadThread
// only via an explicit nonce probe, never inferred from ordinary replies.
type ReadScope int

const (
	ReadMentionOnly ReadScope = iota // default: the bot reads only its @-mention
	ReadThread                       // verified to read thread history
)

// Invocation is how the bot is triggered.
type Invocation int

const (
	InvokeMention Invocation = iota
	InvokeSlash
	InvokeKeyword
)

// BotIdentity is the full provenance anchor. A bare Slack user id is not enough
// across workspaces / enterprise grids, and display names are spoofable, so
// trust and the first-egress gate bind to this whole tuple (plus InstallID),
// never to DisplayName.
type BotIdentity struct {
	SlackTeamID     string // workspace id
	SlackEnterprise string // enterprise-grid id, if any
	AppUserID       string // the bot's app/bot user id
	InstallID       string // verified app-install id; part of the first-egress gate key
	DisplayName     string // advisory only — never a trust input
	VerifiedVia     string // how identity was confirmed (install OAuth, admin add)
}

// BotDataHandling describes where this bot's prompts actually go. Trust is about
// data handling, not just "we built it".
type BotDataHandling struct {
	ModelProvider      string // "anthropic" | "openai" | "self-hosted" | "unknown"
	RetainsLogs        bool
	WorkspaceOwned     bool   // company-owned workspace vs a personal one
	NetworkEgress      string // "none" | "vendor-llm" | "open" | "unknown"
	ReadsThreadHistory bool
}

// BotProfile is the per-bot delivery profile. Version is bumped on every change
// and referenced by every InjectionRecord.
type BotProfile struct {
	Version      int
	Slug         string
	Identity     BotIdentity
	Trust        BotTrust
	DataHandling BotDataHandling
	ReadScope    ReadScope
	Invoke       Invocation
	Trigger      string   // slash command or keyword when Invoke != InvokeMention
	Specialties  []string // observed task history + human confirmation, never the display name
	Notes        string
}

// --- Intent + taint ---

// Taint marks whether intent was influenced by foreign-bot output. It is DERIVED
// from SourceRefs, not caller-declared: tainted intent cannot drive free
// retrieval. v1 retrieval is task-scoped (keyed by task id, never by intent
// text), so taint cannot reach retrieval here; the Ask field still carries the
// text and is classified + redacted like any other export.
type Taint int

const (
	TaintClean Taint = iota
	TaintForeign
)

// StepIntent is the precise ask for one delegation, CEO-restated.
type StepIntent struct {
	Text       string
	Taint      Taint
	SourceRefs []string // message ids the intent derives from, for audit
}

// --- Request ---

// ThreadRef locates the Slack destination.
type ThreadRef struct {
	WorkspaceID string
	ChannelID   string
	ThreadTS    string
}

// ContextRequest is the packer's input for one delegation. It carries snapshot +
// version guards so Gather / Classify / Deliver cannot race a task edit or a
// trust downgrade.
type ContextRequest struct {
	TaskID          string
	TaskUpdatedAt   string // re-checked before Deliver; stale aborts
	PlanID          string
	PlanVersion     int
	Target          BotProfile // carries Target.Version
	Intent          StepIntent
	Thread          ThreadRef
	EgressPolicyVer int
	Approver        string // who approved the plan / this egress, when a gate applies
	IdempotencyKey  string // dedupes both delivery and the InjectionRecord
}

// --- Export classes + items ---

// ExportClass is decided per item by the egress policy + secret scan.
type ExportClass int

const (
	ExportDenied   ExportClass = iota // never leaves the brain
	ExportRedacted                    // leaves only after redaction
	ExportAllowed                     // leaves as-is
)

func (c ExportClass) String() string {
	switch c {
	case ExportRedacted:
		return "redacted"
	case ExportAllowed:
		return "allowed"
	default:
		return "denied"
	}
}

// ItemKind enumerates the provenance of a candidate. The egress policy keys on
// it. The envelope kinds (ask/returnpact/guard) are export data too — a tainted
// task can plant a secret in the ask — so they are classified, not waved through.
type ItemKind string

const (
	KindAsk        ItemKind = "ask"
	KindReturnPact ItemKind = "returnpact"
	KindGuard      ItemKind = "guard"
	KindPlan       ItemKind = "plan"     // human-approved plan step / IssueDraftSpec
	KindTask       ItemKind = "task"     // raw teamTask.Details — free-form, untrusted body
	KindLearning   ItemKind = "learning" // task-scoped, AND-scoped to the task id
	KindWiki       ItemKind = "wiki"     // explicitly task-linked article, never free search
	KindRoster     ItemKind = "roster"
	KindSkill      ItemKind = "skill"
)

// RawItem is a pre-classification candidate produced by Gather.
type RawItem struct {
	Ref  string
	Kind ItemKind
	Body string
}

// RawBundle is what Gather produces: the envelope fields plus candidate items,
// before any classification or redaction.
type RawBundle struct {
	Ask        string
	ReturnPact string
	Guards     []string
	Items      []RawItem
}

// ContextItem is a classified, post-redaction item.
type ContextItem struct {
	Ref        string
	Kind       ItemKind
	Body       string // POST-redaction text
	Class      ExportClass
	Redactions int
}

// ContextBundle is classified + redacted, pre-budget. Only ExportAllowed and
// ExportRedacted items survive Classify into here.
type ContextBundle struct {
	Ask        string
	ReturnPact string
	Guards     []string
	Items      []ContextItem
}

// --- Output + audit ---

// DeliveryStatus tracks an injection through delivery.
type DeliveryStatus int

const (
	DeliveryPending DeliveryStatus = iota
	DeliverySent
	DeliveryFailed
)

func (s DeliveryStatus) String() string {
	switch s {
	case DeliverySent:
		return "sent"
	case DeliveryFailed:
		return "failed"
	default:
		return "pending"
	}
}

// ItemAudit is the per-item record of what was classified, for the egress audit.
type ItemAudit struct {
	Ref        string
	Kind       ItemKind
	Class      ExportClass
	Redactions int
}

// PackedDelegation is the output the bridge posts to Slack.
type PackedDelegation struct {
	MentionText   string // ALWAYS carries the essentials
	ThreadContext string // CHANNEL-VISIBLE: classified against the least-trusted reader
	Injection     InjectionRecord
}

// InjectionRecord is the append-only egress audit row. It is strong enough for
// incident response: it proves exactly what was sent, where, under which policy,
// and whether it landed.
type InjectionRecord struct {
	IdempotencyKey string
	TaskID         string
	PlanID         string
	PlanVersion    int
	Identity       BotIdentity // full tuple, not a bare id
	BotTrust       BotTrust
	ProfileVersion int
	PolicyVersion  int
	WorkspaceID    string
	ChannelID      string
	ThreadTS       string
	MessageTS      string // filled on DeliverySent
	Items          []ItemAudit
	RenderedHash   string // hash of exactly what was sent
	TokenCount     int
	Status         DeliveryStatus
	FailureReason  string
	Timestamp      string
}

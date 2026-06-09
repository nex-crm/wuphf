package team

// broker_approval_action.go defines the structured action-approval payload
// (deterministic-integrations slice 4b) carried on an external-action approval
// request. It replaces the old "parse a string with regexes" path with typed
// fields the card renders directly, and — the headline of 4b — carries the real
// masked HTTP envelope so the approval card's raw toggle shows exactly what will
// go over the wire, not a reconstruction.

type approvalActionAccount struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

type approvalActionEnvelope struct {
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type approvalActionPayload struct {
	Platform    string                  `json:"platform,omitempty"`
	ActionID    string                  `json:"action_id,omitempty"`
	Verb        string                  `json:"verb,omitempty"`
	Name        string                  `json:"name,omitempty"`
	LogoURL     string                  `json:"logo_url,omitempty"`
	Account     *approvalActionAccount  `json:"account,omitempty"`
	RawEnvelope *approvalActionEnvelope `json:"raw_envelope,omitempty"`
}

// sanitizeApprovalActionPayload returns a copy with the raw envelope re-masked
// and the internal connection key stripped. The teammcp gate already masks the
// envelope via the resolver, but the broker re-masks on store so a secret can
// never be persisted on an approval card even if an upstream caller sent one
// unmasked — defense in depth on the surface a human reads.
func sanitizeApprovalActionPayload(p *approvalActionPayload) *approvalActionPayload {
	if p == nil {
		return nil
	}
	out := *p
	if p.RawEnvelope != nil {
		env := *p.RawEnvelope
		env.Headers = maskSensitivePayload(p.RawEnvelope.Headers)
		env.Data = maskSensitivePayload(p.RawEnvelope.Data)
		out.RawEnvelope = &env
	}
	if p.Account != nil {
		// The card shows the friendly account name; the connection key is an
		// internal routing id with no meaning for the approver, so it never
		// leaves the broker.
		out.Account = &approvalActionAccount{Name: p.Account.Name}
	}
	return &out
}

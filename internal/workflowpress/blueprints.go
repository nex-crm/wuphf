package workflowpress

// blueprints.go holds the three registered SynthesisBlueprint skeletons, one per
// ground-truth RevOps workflow. Each is the deterministic state-machine shape
// Synthesize fuses with evidence-derived signals to produce a draft that
// structurally matches the hand-authored contract under testdata/examples.
//
// A blueprint is the operator domain knowledge a generic inference pass cannot
// read out of HTTP traces: the named states a record passes through, the events
// that drive them, the guards that gate them, the action graph, the SLAs and the
// verification scenarios. Provenance on each element is the blueprint's claim;
// Synthesize keeps operator-stated tiers (the operator explicitly told us) and
// treats everything else as inferred. The evidence supplies entities,
// exceptions and improvement signals on top.
//
// These blueprints are intentionally hand-written, not generated: they stand in
// for the model's structural proposal so synthesis is deterministic and
// testable. The freeze gate + human review is what turns any of these drafts
// into a trusted contract.

// obs builds an observed-tier provenance with the given confidence and optional
// evidence pointers; observed means the element was seen in captured evidence.
func obs(conf float64, evidence ...string) Provenance {
	return Provenance{TrustTier: TrustObserved, Confidence: conf, Evidence: evidence}
}

// stated builds an operator-stated provenance; this is the only tier that may
// relax a write-action's approval requirement.
func stated(conf float64) Provenance {
	return Provenance{TrustTier: TrustOperatorStated, Confidence: conf}
}

// inferred builds an inferred-tier provenance; the lowest trust, an inferred
// write always requires approval.
func inferred(conf float64, evidence ...string) Provenance {
	return Provenance{TrustTier: TrustInferred, Confidence: conf, Evidence: evidence}
}

// trialToAERoutingBlueprint is the skeleton for example 1: a new trial signup is
// enriched, scored against the ICP, routed to an AE, and posted to the deal
// channel. Multi-entity, guarded, with an external WRITE (post to channel) that
// must require approval.
func trialToAERoutingBlueprint() SynthesisBlueprint {
	return SynthesisBlueprint{
		Goal:        "Route a new trial signup to the right account executive after enriching and scoring it against the ICP, then post the routing decision to the deal channel.",
		Operator:    defaultOperator,
		EntityOrder: []string{"TrialSignup", "Company", "AccountExecutive"},
		EntityDescriptions: map[string]string{
			"TrialSignup":      "The inbound trial signup event with a work email and company domain.",
			"Company":          "The enriched company record (size, industry) the signup belongs to.",
			"AccountExecutive": "The AE who owns the territory/segment the company maps to.",
		},
		EntityProvenance: map[string]Provenance{
			// The AE roster is something the operator named, not something we
			// scraped — raise it to operator-stated.
			"AccountExecutive": stated(1.0),
		},
		States: []State{
			{Name: "received", Description: "A trial signup has arrived but is not yet enriched.", Initial: true, Provenance: obs(0.95)},
			{Name: "enriched", Description: "Company size and industry have been resolved.", Provenance: obs(0.9)},
			{Name: "scored", Description: "The lead has an ICP fit score from the weighted rubric.", Provenance: stated(1.0)},
			{Name: "routed", Description: "The signup has been assigned to an AE and posted to the deal channel.", Terminal: true, Provenance: stated(1.0)},
		},
		Events: []Event{
			{Name: "trial_signed_up", Trigger: TriggerExternal, From: "received", To: "enriched", Provenance: obs(0.95, "trace:signup-webhook-001")},
			{Name: "enrichment_completed", Trigger: TriggerInternal, From: "enriched", To: "scored", Provenance: obs(0.85)},
			{Name: "scoring_completed", Trigger: TriggerInternal, From: "scored", To: "routed", Guard: "score_meets_icp", Provenance: stated(1.0)},
		},
		Guards: []Guard{
			{Name: "score_meets_icp", Expr: "icp_score >= icp_threshold", Provenance: stated(1.0)},
		},
		Actions: []Action{
			{Name: "enrich_company", Kind: ActionRead, On: "trial_signed_up", Target: "enrichment-provider", RequiresApproval: false, Provenance: obs(0.9, "trace:enrich-call-002")},
			{Name: "score_lead", Kind: ActionRead, On: "enrichment_completed", Target: "icp-rubric", RequiresApproval: false, Provenance: stated(1.0)},
			// route_to_ae is an internal write the operator did by hand; we inferred
			// it from the POST .../owner trace. Inferred write => approval forced by
			// synthActions. Idempotent so a re-run does not double-assign.
			{Name: "route_to_ae", Kind: ActionInternalWrite, On: "scoring_completed", Target: "crm", Idempotent: true, Provenance: inferred(0.7)},
			// post_to_deal_channel is an external write (a chat post). Inferred =>
			// approval forced. The most cautious tier.
			{Name: "post_to_deal_channel", Kind: ActionExternalWrite, On: "scoring_completed", Target: "deal-channel", Provenance: inferred(0.65, "note:operator-posts-by-hand")},
		},
		SLAs: []SLA{
			{Name: "route_freshness", Metric: "time from signup to routed", Threshold: "5m", Provenance: stated(0.9)},
		},
		VerificationScenarios: []VerificationScenario{
			{
				Name:  "icp_fit_routes_and_posts",
				Given: map[string]string{"company_size": "200", "industry": "saas", "icp_score": "82"},
				When:  "trial_signed_up",
				ExpectTransitions: []Transition{
					{From: "received", To: "enriched"},
					{From: "enriched", To: "scored"},
					{From: "scored", To: "routed"},
				},
				ExpectApproval: true,
			},
			{
				Name:              "below_threshold_does_not_route",
				Given:             map[string]string{"company_size": "3", "industry": "hobbyist", "icp_score": "20"},
				When:              "scoring_completed",
				ExpectTransitions: nil,
			},
		},
	}
}

// renewalRiskSweepBlueprint is the skeleton for example 2: a weekly scheduled
// sweep selects accounts renewing within 60 days, pulls the usage trend, flags
// any account down more than 20 percent, creates a CS task and drafts outreach.
// Scheduled trigger, SLAs, exception handling.
func renewalRiskSweepBlueprint() SynthesisBlueprint {
	return SynthesisBlueprint{
		Goal:        "Weekly, for each account with a renewal within 60 days, pull the usage trend; if usage is down more than 20 percent, flag the account at-risk, create a CS task, and draft outreach for the CSM.",
		Operator:    defaultOperator,
		EntityOrder: []string{"Account", "UsageTrend", "CSTask"},
		EntityDescriptions: map[string]string{
			"Account":    "A customer account with a renewal date and a usage history.",
			"UsageTrend": "The week-over-week usage trend pulled from the product analytics warehouse.",
			"CSTask":     "A customer-success task created for an at-risk account.",
		},
		EntityProvenance: map[string]Provenance{
			// The CS task is the operator's own output object, named by the operator.
			"CSTask": stated(1.0),
		},
		States: []State{
			{Name: "idle", Description: "Waiting for the weekly sweep trigger.", Initial: true, Provenance: stated(1.0)},
			{Name: "selecting", Description: "Selecting accounts with a renewal within 60 days.", Provenance: obs(0.9)},
			{Name: "analyzing", Description: "Pulling the usage trend for each selected account.", Provenance: obs(0.85)},
			{Name: "flagged", Description: "Accounts whose usage dropped more than 20 percent are flagged at-risk; CS task created and outreach drafted.", Terminal: true, Provenance: stated(1.0)},
		},
		Events: []Event{
			{Name: "weekly_sweep", Trigger: TriggerScheduled, Schedule: "0 9 * * MON", From: "idle", To: "selecting", Provenance: stated(1.0)},
			{Name: "accounts_selected", Trigger: TriggerInternal, From: "selecting", To: "analyzing", Guard: "renewal_within_60d", Provenance: obs(0.9)},
			{Name: "usage_analyzed", Trigger: TriggerInternal, From: "analyzing", To: "flagged", Guard: "usage_down_over_20pct", Provenance: stated(0.95)},
		},
		Guards: []Guard{
			{Name: "renewal_within_60d", Expr: "renewal_date - now <= 60d", Provenance: stated(1.0)},
			{Name: "usage_down_over_20pct", Expr: "usage_trend.delta_pct < -0.20", Provenance: stated(0.95)},
		},
		Actions: []Action{
			{Name: "select_renewing_accounts", Kind: ActionRead, On: "weekly_sweep", Target: "crm", RequiresApproval: false, Provenance: obs(0.9)},
			{Name: "pull_usage_trend", Kind: ActionRead, On: "accounts_selected", Target: "analytics-warehouse", RequiresApproval: false, Provenance: obs(0.85)},
			// create_cs_task is operator-stated (the operator told us they create a
			// CS task); even so the blueprint gates it for safety.
			{Name: "create_cs_task", Kind: ActionInternalWrite, On: "usage_analyzed", Target: "crm", RequiresApproval: true, Idempotent: true, Provenance: stated(1.0)},
			// draft_outreach is an external write inferred from the POST to the mail
			// draft endpoint; inferred => approval forced.
			{Name: "draft_outreach", Kind: ActionExternalWrite, On: "usage_analyzed", Target: "email-draft", Provenance: inferred(0.6, "note:csm-personalizes-outreach")},
		},
		SLAs: []SLA{
			{Name: "usage_freshness", Metric: "age of the usage data used for the trend", Threshold: "24h", Provenance: stated(0.9)},
			{Name: "sweep_completion", Metric: "time to complete the weekly sweep after trigger", Threshold: "2h", Provenance: stated(0.85)},
		},
		VerificationScenarios: []VerificationScenario{
			{
				Name:  "at_risk_account_is_flagged",
				Given: map[string]string{"renewal_in_days": "30", "delta_pct": "-0.35"},
				When:  "weekly_sweep",
				ExpectTransitions: []Transition{
					{From: "idle", To: "selecting"},
					{From: "selecting", To: "analyzing"},
					{From: "analyzing", To: "flagged"},
				},
				ExpectApproval: true,
			},
			{
				Name:              "stable_usage_is_not_flagged",
				Given:             map[string]string{"renewal_in_days": "30", "delta_pct": "-0.05"},
				When:              "usage_analyzed",
				ExpectTransitions: nil,
			},
			{
				Name:              "renewal_too_far_out_is_skipped",
				Given:             map[string]string{"renewal_in_days": "120", "delta_pct": "-0.40"},
				When:              "accounts_selected",
				ExpectTransitions: nil,
			},
		},
	}
}

// inboundLeadDedupeMergeBlueprint is the skeleton for example 3: a new inbound
// lead is matched against existing contacts/companies; a confident match merges
// idempotently into the existing record, otherwise a new record is created.
// Exercises idempotency and duplicate handling.
func inboundLeadDedupeMergeBlueprint() SynthesisBlueprint {
	return SynthesisBlueprint{
		Goal:        "For each new inbound lead, find a matching contact or company; if a match is found, merge idempotently into the existing record; otherwise create a new one.",
		Operator:    defaultOperator,
		EntityOrder: []string{"InboundLead", "Contact", "MatchCandidate"},
		EntityDescriptions: map[string]string{
			"InboundLead":    "A new inbound lead from a form fill or list import.",
			"Contact":        "The canonical contact record the lead may already correspond to.",
			"MatchCandidate": "A scored candidate match between the inbound lead and an existing contact/company.",
		},
		EntityProvenance: map[string]Provenance{
			// The match candidate is a synthesised scoring object, not a record we
			// observed directly — it is inferred.
			"MatchCandidate": inferred(0.7),
		},
		States: []State{
			{Name: "received", Description: "A new inbound lead has arrived.", Initial: true, Provenance: obs(0.95)},
			{Name: "matched", Description: "Candidate matches have been resolved against existing records.", Provenance: obs(0.85)},
			{Name: "merged", Description: "A match was found and the lead was merged idempotently into the existing record.", Terminal: true, Provenance: stated(1.0)},
			{Name: "created", Description: "No match was found and a new record was created.", Terminal: true, Provenance: stated(1.0)},
		},
		Events: []Event{
			{Name: "lead_received", Trigger: TriggerExternal, From: "received", To: "matched", Provenance: obs(0.95, "trace:form-submit-021")},
			{Name: "match_found", Trigger: TriggerInternal, From: "matched", To: "merged", Guard: "has_confident_match", Provenance: stated(1.0)},
			{Name: "no_match", Trigger: TriggerInternal, From: "matched", To: "created", Guard: "no_confident_match", Provenance: stated(1.0)},
		},
		Guards: []Guard{
			{Name: "has_confident_match", Expr: "best_candidate.match_score >= match_threshold", Provenance: stated(0.95)},
			{Name: "no_confident_match", Expr: "best_candidate.match_score < match_threshold", Provenance: stated(0.95)},
		},
		Actions: []Action{
			{Name: "find_matches", Kind: ActionRead, On: "lead_received", Target: "crm", RequiresApproval: false, Provenance: obs(0.9)},
			// merge_into_existing and create_new_record are internal writes inferred
			// from the POST .../merge and POST /contacts traces; inferred => approval
			// forced. Both idempotent so duplicate delivery does not double-apply.
			{Name: "merge_into_existing", Kind: ActionInternalWrite, On: "match_found", Target: "crm", Idempotent: true, Provenance: inferred(0.75, "note:merge-must-be-replayable")},
			{Name: "create_new_record", Kind: ActionInternalWrite, On: "no_match", Target: "crm", Idempotent: true, Provenance: inferred(0.7)},
		},
		SLAs: []SLA{
			{Name: "dedupe_latency", Metric: "time from lead received to merged/created", Threshold: "2m", Provenance: stated(0.85)},
		},
		VerificationScenarios: []VerificationScenario{
			{
				Name:  "matching_lead_merges_idempotently",
				Given: map[string]string{"existing_contact": "true", "match_score": "0.96"},
				When:  "lead_received",
				ExpectTransitions: []Transition{
					{From: "received", To: "matched"},
					{From: "matched", To: "merged"},
				},
				ExpectApproval: true,
			},
			{
				Name:  "new_lead_creates_record",
				Given: map[string]string{"existing_contact": "false", "match_score": "0.10"},
				When:  "lead_received",
				ExpectTransitions: []Transition{
					{From: "received", To: "matched"},
					{From: "matched", To: "created"},
				},
				ExpectApproval: true,
			},
			{
				Name:  "duplicate_event_does_not_double_apply",
				Given: map[string]string{"existing_contact": "true", "match_score": "0.96", "redelivered": "true"},
				When:  "match_found",
				ExpectTransitions: []Transition{
					{From: "matched", To: "merged"},
				},
				ExpectApproval: true,
			},
		},
	}
}

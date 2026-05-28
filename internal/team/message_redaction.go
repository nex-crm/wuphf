package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/scanner"
)

func redactSecretsInText(text string) string {
	res := scanner.RedactSecretsForDisplay(text)
	return res.Content
}

func sanitizeChannelMessageSecrets(msg channelMessage) channelMessage {
	redactionCount := msg.RedactionCount
	reasons := append([]string(nil), msg.RedactionReasons...)

	content := scanner.RedactSecretsForDisplay(msg.Content)
	if content.Matches() > 0 {
		msg.Content = content.Content
		redactionCount += content.Matches()
		reasons = appendRedactionReasons(reasons, content.ReasonLabels())
	}

	title := scanner.RedactSecretsForDisplay(msg.Title)
	if title.Matches() > 0 {
		msg.Title = title.Content
		redactionCount += title.Matches()
		reasons = appendRedactionReasons(reasons, title.ReasonLabels())
	}

	if redactionCount > 0 || len(reasons) > 0 {
		msg.Redacted = true
		msg.RedactionCount = redactionCount
		msg.RedactionReasons = reasons
	}
	return msg
}

func sanitizeHumanInterview(req humanInterview) humanInterview {
	redactionCount := req.RedactionCount
	reasons := append([]string(nil), req.RedactionReasons...)

	// Approval cards contain structured technical identifiers (action_id,
	// connection key, workflow handle) that the human MUST see to know
	// what they're approving. Run only the known-secret pattern pass on
	// these short identifier-bearing fields — the entropy heuristic was
	// redacting high-entropy action IDs (e.g. conn_mod_def::HASH) as
	// "Action: [REDACTED]" which defeated the entire approval surface.
	// Free-form fields (Context, answer text, option descriptions) still
	// get the stronger display redaction so a pasted token in the body
	// does not leak through. Real secrets (API keys, AWS access keys,
	// OAuth tokens, etc.) get caught by the pattern pass in either mode.
	applyKnownSecretsOnly := func(s string) string {
		res := scanner.RedactKnownSecretsOnly(s)
		if res.Matches() > 0 {
			redactionCount += res.Matches()
			reasons = appendRedactionReasons(reasons, res.ReasonLabels())
			return res.Content
		}
		return s
	}
	applyDisplayRedaction := func(s string) string {
		res := scanner.RedactSecretsForDisplay(s)
		if res.Matches() > 0 {
			redactionCount += res.Matches()
			reasons = appendRedactionReasons(reasons, res.ReasonLabels())
			return res.Content
		}
		return s
	}

	req.Title = applyKnownSecretsOnly(req.Title)
	req.Question = applyKnownSecretsOnly(req.Question)
	req.Context = applyDisplayRedaction(req.Context)
	if len(req.Options) > 0 {
		req.Options = append([]interviewOption(nil), req.Options...)
		for i := range req.Options {
			req.Options[i].Label = applyKnownSecretsOnly(req.Options[i].Label)
			req.Options[i].Description = applyDisplayRedaction(req.Options[i].Description)
			req.Options[i].TextHint = applyDisplayRedaction(req.Options[i].TextHint)
		}
	}
	if req.Answered != nil {
		answer := *req.Answered
		answer.ChoiceText = applyDisplayRedaction(answer.ChoiceText)
		answer.CustomText = applyDisplayRedaction(answer.CustomText)
		req.Answered = &answer
	}
	if redactionCount > 0 || len(reasons) > 0 {
		req.Redacted = true
		req.RedactionCount = redactionCount
		req.RedactionReasons = reasons
	}
	return req
}

func sanitizeOfficeSignalRecord(sig officeSignalRecord) officeSignalRecord {
	sig.Title = redactSecretsInText(sig.Title)
	sig.Content = redactSecretsInText(sig.Content)
	return sig
}

func sanitizeOfficeDecisionRecord(dec officeDecisionRecord) officeDecisionRecord {
	dec.Summary = redactSecretsInText(dec.Summary)
	dec.Reason = redactSecretsInText(dec.Reason)
	return dec
}

func sanitizeWatchdogAlert(alert watchdogAlert) watchdogAlert {
	alert.Summary = redactSecretsInText(alert.Summary)
	return alert
}

func sanitizeTeamTask(task teamTask) teamTask {
	task.Title = redactSecretsInText(task.Title)
	task.Details = redactSecretsInText(task.Details)
	return task
}

func sanitizeSchedulerJob(job schedulerJob) schedulerJob {
	job.Label = redactSecretsInText(job.Label)
	// Payload is JSON parsed at runtime; redacting it would corrupt the structure.
	return job
}

func appendRedactionReasons(existing []string, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, reason := range existing {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	for _, reason := range incoming {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	return out
}

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
	req.Title = redactSecretsInText(req.Title)
	req.Question = redactSecretsInText(req.Question)
	req.Context = redactSecretsInText(req.Context)
	if len(req.Options) > 0 {
		req.Options = append([]interviewOption(nil), req.Options...)
		for i := range req.Options {
			req.Options[i].Label = redactSecretsInText(req.Options[i].Label)
			req.Options[i].Description = redactSecretsInText(req.Options[i].Description)
			req.Options[i].TextHint = redactSecretsInText(req.Options[i].TextHint)
		}
	}
	if req.Answered != nil {
		answer := *req.Answered
		answer.ChoiceText = redactSecretsInText(answer.ChoiceText)
		answer.CustomText = redactSecretsInText(answer.CustomText)
		req.Answered = &answer
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

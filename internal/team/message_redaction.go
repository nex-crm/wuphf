package team

import (
	"encoding/json"
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

	applyField := func(s string) string {
		res := scanner.RedactSecretsForDisplay(s)
		if res.Matches() > 0 {
			redactionCount += res.Matches()
			reasons = appendRedactionReasons(reasons, res.ReasonLabels())
			return res.Content
		}
		return s
	}

	req.Title = applyField(req.Title)
	req.Question = applyField(req.Question)
	req.Context = applyField(req.Context)
	if len(req.Options) > 0 {
		req.Options = append([]interviewOption(nil), req.Options...)
		for i := range req.Options {
			req.Options[i].Label = applyField(req.Options[i].Label)
			req.Options[i].Description = applyField(req.Options[i].Description)
			req.Options[i].TextHint = applyField(req.Options[i].TextHint)
		}
	}
	if req.Answered != nil {
		answer := *req.Answered
		answer.ChoiceText = applyField(answer.ChoiceText)
		answer.CustomText = applyField(answer.CustomText)
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
	job.ScheduleExpr = redactSecretsInText(job.ScheduleExpr)
	job.WorkflowKey = redactSecretsInText(job.WorkflowKey)
	job.SkillName = redactSecretsInText(job.SkillName)
	job.Payload = sanitizeSchedulerPayload(job.Payload)
	job.LastRunStatus = redactSecretsInText(job.LastRunStatus)
	return job
}

func sanitizeSchedulerPayload(payload string) string {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return payload
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return redactSecretsInText(payload)
	}
	value = sanitizeJSONStrings(value)
	raw, err := json.Marshal(value)
	if err != nil {
		return redactSecretsInText(payload)
	}
	return string(raw)
}

func sanitizeJSONStrings(value any) any {
	switch v := value.(type) {
	case string:
		return redactSecretsInText(v)
	case []any:
		for i := range v {
			v[i] = sanitizeJSONStrings(v[i])
		}
		return v
	case map[string]any:
		for key, item := range v {
			v[key] = sanitizeJSONStrings(item)
		}
		return v
	default:
		return value
	}
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

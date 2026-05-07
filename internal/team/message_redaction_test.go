package team

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeHumanInterviewSetsRedactionMetadata(t *testing.T) {
	secret := "sk-" + strings.Repeat("H", 24)
	req := sanitizeHumanInterview(humanInterview{
		ID:       "req-1",
		From:     "ceo",
		Title:    "Approve key " + secret,
		Question: "Use " + secret + "?",
		Context:  "context " + secret,
		Options: []interviewOption{{
			ID:          "approve",
			Label:       "Approve " + secret,
			Description: "Ship with " + secret,
			TextHint:    "Mention " + secret,
		}},
		Answered: &interviewAnswer{
			ChoiceText: "Approve " + secret,
			CustomText: "Custom " + secret,
		},
	})

	combined := strings.Join([]string{
		req.Title,
		req.Question,
		req.Context,
		req.Options[0].Label,
		req.Options[0].Description,
		req.Options[0].TextHint,
		req.Answered.ChoiceText,
		req.Answered.CustomText,
	}, "\n")
	if strings.Contains(combined, secret) {
		t.Fatalf("human interview leaked secret: %q", combined)
	}
	if !req.Redacted || req.RedactionCount == 0 || len(req.RedactionReasons) == 0 {
		t.Fatalf("human interview missing redaction metadata: %+v", req)
	}

	again := sanitizeHumanInterview(req)
	if again.RedactionCount != req.RedactionCount {
		t.Fatalf("sanitizing twice changed count: before %d after %d", req.RedactionCount, again.RedactionCount)
	}
}

func TestSanitizeQueuedRecordsRedactsFreeText(t *testing.T) {
	secret := "sk-" + strings.Repeat("Q", 24)

	task := sanitizeTeamTask(teamTask{
		Title:   "Build with " + secret,
		Details: "Details " + secret,
	})
	if strings.Contains(task.Title+task.Details, secret) {
		t.Fatalf("task leaked secret: %+v", task)
	}

	alert := sanitizeWatchdogAlert(watchdogAlert{
		Summary: "Alert " + secret,
	})
	if strings.Contains(alert.Summary, secret) {
		t.Fatalf("watchdog leaked secret: %+v", alert)
	}

	job := sanitizeSchedulerJob(schedulerJob{
		Label:         "Follow up " + secret,
		ScheduleExpr:  "expr " + secret,
		WorkflowKey:   "workflow " + secret,
		SkillName:     "skill " + secret,
		Payload:       "payload " + secret,
		LastRunStatus: "status " + secret,
	})
	if strings.Contains(strings.Join([]string{job.Label, job.ScheduleExpr, job.WorkflowKey, job.SkillName, job.Payload, job.LastRunStatus}, "\n"), secret) {
		t.Fatalf("scheduler job leaked secret: %+v", job)
	}
}

func TestTruncateMemberLastMessagePreviewIsRuneSafeAndKeepsMarkersWhole(t *testing.T) {
	content := strings.Repeat("é", 79) + "[REDACTED:openai] tail"
	got := truncateMemberLastMessagePreview(content, 80)

	if !utf8.ValidString(got) {
		t.Fatalf("preview is invalid UTF-8: %q", got)
	}
	if strings.Contains(got, "[") {
		t.Fatalf("preview cut inside redaction marker: %q", got)
	}
	if utf8.RuneCountInString(got) != 79 {
		t.Fatalf("preview length = %d runes, want 79", utf8.RuneCountInString(got))
	}

	completeMarker := strings.Repeat("é", 63) + "[REDACTED:openai] tail"
	got = truncateMemberLastMessagePreview(completeMarker, 80)
	if !strings.Contains(got, "[REDACTED:openai]") {
		t.Fatalf("complete marker should remain visible: %q", got)
	}
}

func TestOfficeSignalDedupeKeyUsesRedactedContent(t *testing.T) {
	secret := "sk-" + strings.Repeat("D", 24)
	key := officeSignalDedupeKey(officeSignal{
		Source:  "watchdog",
		Kind:    "risk",
		Channel: "general",
		Content: "Signal " + secret,
	})
	if strings.Contains(key, secret) {
		t.Fatalf("dedupe key leaked secret: %q", key)
	}
	if !strings.Contains(key, "[redacted]") {
		t.Fatalf("dedupe key missing redaction marker: %q", key)
	}
}

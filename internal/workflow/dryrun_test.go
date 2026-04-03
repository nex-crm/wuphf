package workflow

import (
	"strings"
	"testing"
)

func TestPreviewAction_Composio(t *testing.T) {
	exec := ExecuteSpec{
		Provider:      ProviderComposio,
		Action:        "GMAIL_SEND_EMAIL",
		ConnectionKey: "gmail-primary",
		Data:          map[string]any{"to": "test@example.com", "body": "Hello"},
	}

	result := PreviewAction(exec, nil)

	if result.Provider != ProviderComposio {
		t.Errorf("expected provider composio, got %s", result.Provider)
	}
	if result.Action != "GMAIL_SEND_EMAIL" {
		t.Errorf("expected action GMAIL_SEND_EMAIL, got %s", result.Action)
	}
	if !strings.Contains(result.Description, "Would execute") {
		t.Errorf("expected 'Would execute' in description, got %q", result.Description)
	}
	if !strings.Contains(result.Description, "gmail-primary") {
		t.Errorf("expected connection key in description, got %q", result.Description)
	}
	if result.Data["to"] != "test@example.com" {
		t.Errorf("expected data to be preserved, got %v", result.Data)
	}
}

func TestPreviewAction_Broker(t *testing.T) {
	exec := ExecuteSpec{
		Provider: ProviderBroker,
		Method:   "AddEmailDecision",
	}

	result := PreviewAction(exec, nil)

	if result.Provider != ProviderBroker {
		t.Errorf("expected provider broker, got %s", result.Provider)
	}
	if result.Action != "AddEmailDecision" {
		t.Errorf("expected action AddEmailDecision, got %s", result.Action)
	}
	if !strings.Contains(result.Description, "broker method") {
		t.Errorf("expected 'broker method' in description, got %q", result.Description)
	}
}

func TestPreviewAction_Agent(t *testing.T) {
	exec := ExecuteSpec{
		Provider: ProviderAgent,
		Slug:     "email-triage",
		Prompt:   "Triage this email",
	}

	result := PreviewAction(exec, nil)

	if result.Provider != ProviderAgent {
		t.Errorf("expected provider agent, got %s", result.Provider)
	}
	if result.Action != "email-triage" {
		t.Errorf("expected action email-triage, got %s", result.Action)
	}
	if !strings.Contains(result.Description, "dispatch to agent") {
		t.Errorf("expected 'dispatch to agent' in description, got %q", result.Description)
	}
	if !strings.Contains(result.Description, "Triage this email") {
		t.Errorf("expected prompt in description, got %q", result.Description)
	}
}

func TestPreviewAction_NoData(t *testing.T) {
	exec := ExecuteSpec{
		Provider: ProviderComposio,
		Action:   "SLACK_SEND_MESSAGE",
	}

	result := PreviewAction(exec, nil)

	if result.Data != nil {
		t.Errorf("expected nil data when no data in exec, got %v", result.Data)
	}
}

func TestPreviewAction_UnknownProvider(t *testing.T) {
	exec := ExecuteSpec{
		Provider: "custom",
		Action:   "do-thing",
	}

	result := PreviewAction(exec, nil)

	if result.Provider != "custom" {
		t.Errorf("expected provider custom, got %s", result.Provider)
	}
	if !strings.Contains(result.Description, "custom") {
		t.Errorf("expected provider name in description, got %q", result.Description)
	}
}

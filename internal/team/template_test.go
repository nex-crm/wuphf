package team

import "testing"

func TestParseGeneratedMemberTemplateAppliesDefaults(t *testing.T) {
	tmpl, err := parseGeneratedMemberTemplate(`{"slug":"devrel","name":"Developer Relations","role":"","expertise":[],"personality":"","permission_mode":""}`)
	if err != nil {
		t.Fatalf("parseGeneratedMemberTemplate: %v", err)
	}
	if tmpl.Slug != "devrel" {
		t.Fatalf("unexpected slug: %q", tmpl.Slug)
	}
	if tmpl.Role != "Developer Relations" {
		t.Fatalf("expected role to default to name, got %q", tmpl.Role)
	}
	if len(tmpl.Expertise) == 0 {
		t.Fatal("expected inferred expertise")
	}
	if tmpl.Personality == "" {
		t.Fatal("expected inferred personality")
	}
	if tmpl.PermissionMode != "plan" {
		t.Fatalf("expected default permission mode plan, got %q", tmpl.PermissionMode)
	}
}

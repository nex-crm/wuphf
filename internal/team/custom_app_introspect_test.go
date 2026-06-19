package team

import (
	"strings"
	"testing"
)

func introspectHas(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestIntrospectAppSource derives the app's real shape from source: bridge APIs,
// the gmail integration (via getEmails), declared types, refine resources, and
// Mantine UI — while skipping the protected bridge file and build artifacts.
func TestIntrospectAppSource(t *testing.T) {
	files := map[string]string{
		"package.json":        "{}",
		"src/wuphf-bridge.ts": "export function getEmails(){} // protected: must be skipped",
		"src/App.tsx": `
import { Stack, Title, Table, Badge } from "@mantine/core";
import { getEmails, ai } from "./wuphf-bridge";

interface EmailItem { id: string }
type DigestResult = { summary: string };

export function App() {
  const emails = getEmails({ limit: 25 });
  const out = ai("summarize these", emails, { json: true });
  return <Table />;
}`,
		"src/Tasks.tsx": `
import { useTable } from "@refinedev/react-table";
function Grid() { useTable({ refineCoreProps: { resource: "tasks" } }); }
`,
		"node_modules/x/index.js": `callIntegration("evil","EVIL_ACTION")`,
	}
	caps := introspectAppSource(files)

	if !introspectHas(caps.BridgeAPIs, "getEmails") || !introspectHas(caps.BridgeAPIs, "ai") {
		t.Fatalf("bridge APIs = %v", caps.BridgeAPIs)
	}
	gmail := false
	for _, it := range caps.Integrations {
		if it.Platform == "gmail" && introspectHas(it.Actions, "GMAIL_FETCH_EMAILS") {
			gmail = true
		}
		if it.Platform == "evil" {
			t.Fatal("scanned node_modules — must be skipped")
		}
	}
	if !gmail {
		t.Fatalf("expected gmail integration from getEmails, got %v", caps.Integrations)
	}
	if !introspectHas(caps.DataTypes, "EmailItem") || !introspectHas(caps.DataTypes, "DigestResult") {
		t.Fatalf("data types = %v", caps.DataTypes)
	}
	if !introspectHas(caps.Resources, "tasks") {
		t.Fatalf("resources = %v", caps.Resources)
	}
	if !introspectHas(caps.UIComponents, "Table") || !introspectHas(caps.UIComponents, "Badge") {
		t.Fatalf("ui components = %v", caps.UIComponents)
	}
	for _, f := range caps.SourceFiles {
		if f == "src/wuphf-bridge.ts" {
			t.Fatal("protected bridge file must not be in SourceFiles")
		}
	}
	rendered := renderAppCapabilities(caps)
	for _, want := range []string{"Data model", "APIs / data access", "gmail", "EmailItem", "Table"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered summary missing %q:\n%s", want, rendered)
		}
	}
}

// TestIntrospectCallIntegrationAndWrites picks up explicit callIntegration usage
// and the createTask office write.
func TestIntrospectCallIntegrationAndWrites(t *testing.T) {
	files := map[string]string{
		"src/App.tsx": `
import { callIntegration, createTask } from "./wuphf-bridge";
const r = callIntegration("slack", "SLACK_LIST_CHANNELS", {});
function onClick(){ createTask({ title: "x" }); }`,
	}
	caps := introspectAppSource(files)
	if len(caps.Integrations) != 1 || caps.Integrations[0].Platform != "slack" ||
		!introspectHas(caps.Integrations[0].Actions, "SLACK_LIST_CHANNELS") {
		t.Fatalf("integrations = %+v", caps.Integrations)
	}
	if !introspectHas(caps.OfficeWrites, "createTask") {
		t.Fatalf("office writes = %v", caps.OfficeWrites)
	}
}

// TestIntrospectNoFalsePositivesFromComments — patterns in comments/strings must
// not be reported as real usage (the lexer strips comments, keeps strings).
func TestIntrospectEmptyForHTMLOnly(t *testing.T) {
	// An html-only app (no scannable source) yields empty capabilities and an
	// empty rendered summary so callers can omit the section.
	caps := introspectAppSource(map[string]string{"index.html": "<html></html>"})
	if len(caps.SourceFiles) != 0 {
		t.Fatalf("html-only app should have no scannable source, got %v", caps.SourceFiles)
	}
	if renderAppCapabilities(caps) != "" {
		t.Fatal("render should be empty when there is no source")
	}
}

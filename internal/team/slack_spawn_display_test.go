package team

import "testing"

func TestSlackSpawnDisplayName(t *testing.T) {
	cases := []struct{ name, role, want string }{
		{"Scout", "RevOps Agent", "Scout (RevOps Agent)"},
		{"Scout", "", "Scout"},
		{"  Atlas ", " Data Analyst ", "Atlas (Data Analyst)"},
	}
	for _, c := range cases {
		if got := slackSpawnDisplayName(c.name, c.role); got != c.want {
			t.Fatalf("slackSpawnDisplayName(%q,%q)=%q want %q", c.name, c.role, got, c.want)
		}
	}
}

func TestSlackSpawnManifestShowsRole(t *testing.T) {
	m := slackSpawnManifest("Scout", "RevOps Agent")
	if m.Features.BotUser.DisplayName != "Scout (RevOps Agent)" {
		t.Fatalf("bot display = %q, want Scout (RevOps Agent)", m.Features.BotUser.DisplayName)
	}
	// App name carries the role too when it fits in Slack's 35-char cap.
	if m.DisplayInformation.Name != "Scout (RevOps Agent)" {
		t.Fatalf("app name = %q, want Scout (RevOps Agent)", m.DisplayInformation.Name)
	}
	// A long role overflows the app-name cap → falls back to the bare name, but
	// the bot display name still carries the full role.
	long := slackSpawnManifest("Scout", "market and competitive intelligence researcher")
	if long.DisplayInformation.Name != "Scout" {
		t.Fatalf("overflow app name = %q, want bare Scout", long.DisplayInformation.Name)
	}
	if long.Features.BotUser.DisplayName == "Scout" {
		t.Fatal("bot display name should still carry the role even when app name overflows")
	}
}

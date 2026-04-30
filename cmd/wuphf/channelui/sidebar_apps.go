package channelui

// OfficeSidebarApp is one row in the sidebar's "apps" stack — a
// typed app id (Messages / Recovery / Tasks / etc.) and its display
// label.
type OfficeSidebarApp struct {
	App   OfficeApp
	Label string
}

// OfficeSidebarApps returns the canonical sidebar app stack in the
// order they render. The Recovery app sits second by design so the
// "see what changed while you were away" UI is one click below
// Messages.
func OfficeSidebarApps() []OfficeSidebarApp {
	return []OfficeSidebarApp{
		{App: OfficeAppMessages, Label: "Messages"},
		{App: OfficeAppRecovery, Label: "Recovery"},
		{App: OfficeAppTasks, Label: "Tasks"},
		{App: OfficeAppRequests, Label: "Requests"},
		{App: OfficeAppPolicies, Label: "Policies"},
		{App: OfficeAppCalendar, Label: "Calendar"},
		{App: OfficeAppArtifacts, Label: "Artifacts"},
		{App: OfficeAppSkills, Label: "Skills"},
	}
}

// VisibleSidebarApps returns up to maxRows apps, always keeping the
// app with App == activeApp visible. When the active app would
// otherwise be cut off it replaces the last visible row so the user
// always sees what they have selected.
func VisibleSidebarApps(apps []OfficeSidebarApp, activeApp OfficeApp, maxRows int) []OfficeSidebarApp {
	if maxRows <= 0 || len(apps) == 0 {
		return nil
	}
	if len(apps) <= maxRows {
		return apps
	}
	visible := append([]OfficeSidebarApp(nil), apps[:maxRows]...)
	for _, app := range visible {
		if app.App == activeApp {
			return visible
		}
	}
	for _, app := range apps {
		if app.App == activeApp {
			visible[len(visible)-1] = app
			return visible
		}
	}
	return visible
}

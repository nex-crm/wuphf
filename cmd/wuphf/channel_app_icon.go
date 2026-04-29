package main

// appIcon returns the unicode glyph that introduces an office-app
// header. It currently lives in package main because it switches on the
// officeApp enum, which has not yet moved to channelui — once that enum
// migrates, this function will fold into channelui/styles.go.
func appIcon(app officeApp) string {
	switch app {
	case officeAppTasks:
		return "☑"
	case officeAppPolicies:
		return "✦"
	case officeAppCalendar:
		return "◷"
	case officeAppSkills:
		return "⚡"
	case officeAppMessages:
		return "•"
	default:
		return "#"
	}
}

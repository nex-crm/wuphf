package commands

import (
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/calendar"
)

// cmdCalendar handles /calendar [add|remove|list] subcommands and renders the week grid.
func cmdCalendar(ctx *SlashContext, args string) error {
	store := calendar.NewCalendarStore("")

	parts := strings.Fields(args)
	if len(parts) == 0 {
		// Default: show week grid
		weekStart := calendar.MondayOfWeek(time.Now())
		grid := calendar.RenderWeekGrid(store, weekStart)
		ctx.SendResult(grid, nil)
		return nil
	}

	sub := parts[0]
	switch sub {
	case "add":
		// /calendar add <agent> <cron> [description...]
		if len(parts) < 3 {
			ctx.SendResult("Usage: /calendar add <agent-slug> <cron-expr> [description]", nil)
			return nil
		}
		slug := parts[1]
		cronExpr := parts[2]
		desc := ""
		if len(parts) > 3 {
			desc = strings.Join(parts[3:], " ")
		}
		err := store.AddSchedule(slug, cronExpr, desc)
		if err != nil {
			ctx.SendResult("", err)
			return nil
		}
		ctx.SendResult("Schedule added for "+slug+" ("+cronExpr+")", nil)

	case "remove", "rm":
		if len(parts) < 2 {
			ctx.SendResult("Usage: /calendar remove <agent-slug>", nil)
			return nil
		}
		store.RemoveSchedule(parts[1])
		ctx.SendResult("Schedule removed for "+parts[1], nil)

	case "list":
		schedules := store.ListSchedules()
		if len(schedules) == 0 {
			ctx.SendResult("No schedules configured.", nil)
			return nil
		}
		var lines []string
		for _, s := range schedules {
			line := s.AgentSlug + "  " + s.CronExpr
			if s.Description != "" {
				line += "  (" + s.Description + ")"
			}
			lines = append(lines, "  "+line)
		}
		ctx.SendResult("Schedules:\n"+strings.Join(lines, "\n"), nil)

	default:
		ctx.SendResult("Unknown subcommand: "+sub+". Use add, remove, or list.", nil)
	}

	return nil
}

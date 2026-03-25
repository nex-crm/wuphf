package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/tui/render"
)

func cmdTask(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /task <list|get|create|update|delete> [options]")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	pos, flags := parseFlags(args)
	if len(pos) == 0 {
		ctx.AddMessage("system", "Usage: /task <list|get|create|update|delete> [options]")
		return nil
	}
	sub := pos[0]

	switch sub {
	case "list":
		return taskList(ctx, flags)
	case "get":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /task get <id>")
			return nil
		}
		return taskGet(ctx, pos[1])
	case "create":
		return taskCreate(ctx, flags)
	case "update":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /task update <id> [--title] [--description] [--priority] [--due] [--completed bool]")
			return nil
		}
		return taskUpdate(ctx, pos[1], flags)
	case "delete":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /task delete <id>")
			return nil
		}
		return taskDelete(ctx, pos[1])
	default:
		ctx.AddMessage("system", fmt.Sprintf("Unknown task subcommand: %s", sub))
		return nil
	}
}

func taskList(ctx *SlashContext, flags map[string]string) error {
	params := url.Values{}
	if v := getFlag(flags, "entity"); v != "" {
		params.Set("entity", v)
	}
	if v := getFlag(flags, "assignee"); v != "" {
		params.Set("assignee", v)
	}
	if v := getFlag(flags, "search"); v != "" {
		params.Set("search", v)
	}
	if v := getFlag(flags, "completed"); v != "" {
		params.Set("completed", v)
	}
	if v := getFlag(flags, "limit"); v != "" {
		params.Set("limit", v)
	}
	path := "/v1/tasks"
	if q := params.Encode(); q != "" {
		path += "?" + q
	}
	ctx.SetLoading(true)
	result, err := api.Get[[]map[string]any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.AddMessage("system", "No tasks found.")
		return nil
	}
	headers := []string{"ID", "Title", "Priority", "Due", "Completed"}
	var rows [][]string
	for _, t := range result {
		id := fmt.Sprintf("%v", t["id"])
		title, _ := t["title"].(string)
		priority, _ := t["priority"].(string)
		due, _ := t["due"].(string)
		completed := "false"
		if c, ok := t["completed"].(bool); ok && c {
			completed = "true"
		}
		rows = append(rows, []string{id, title, priority, due, completed})
	}
	ctx.AddMessage("system", render.RenderTable(headers, rows, 100))
	return nil
}

func taskGet(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	result, err := api.Get[map[string]any](ctx.APIClient, "/v1/tasks/"+url.PathEscape(id), 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func taskCreate(ctx *SlashContext, flags map[string]string) error {
	title := getFlag(flags, "title")
	if title == "" {
		ctx.AddMessage("system", "Usage: /task create --title <t> [--description] [--priority] [--due] [--entities] [--assignees]")
		return nil
	}
	body := map[string]any{"title": title}
	if v := getFlag(flags, "description"); v != "" {
		body["description"] = v
	}
	if v := getFlag(flags, "priority"); v != "" {
		body["priority"] = v
	}
	if v := getFlag(flags, "due"); v != "" {
		body["due"] = v
	}
	if v := getFlag(flags, "entities"); v != "" {
		body["entity_ids"] = strings.Split(v, ",")
	}
	if v := getFlag(flags, "assignees"); v != "" {
		body["assignee_ids"] = strings.Split(v, ",")
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/tasks", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Task created.\n"+formatJSON(result))
	return nil
}

func taskUpdate(ctx *SlashContext, id string, flags map[string]string) error {
	body := map[string]any{}
	if v := getFlag(flags, "title"); v != "" {
		body["title"] = v
	}
	if v := getFlag(flags, "description"); v != "" {
		body["description"] = v
	}
	if v := getFlag(flags, "priority"); v != "" {
		body["priority"] = v
	}
	if v := getFlag(flags, "due"); v != "" {
		body["due"] = v
	}
	if v := getFlag(flags, "completed"); v != "" {
		body["completed"] = v == "true"
	}
	if len(body) == 0 {
		ctx.AddMessage("system", "Provide at least one field to update.")
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Patch[map[string]any](ctx.APIClient, "/v1/tasks/"+url.PathEscape(id), body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Task updated.\n"+formatJSON(result))
	return nil
}

func taskDelete(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	_, err := api.Delete[map[string]any](ctx.APIClient, "/v1/tasks/"+url.PathEscape(id), 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", fmt.Sprintf("Task %s deleted.", id))
	return nil
}

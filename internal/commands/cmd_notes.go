package commands

import (
	"fmt"
	"net/url"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/tui/render"
)

func cmdNote(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /note <list|get|create|update|delete> [options]")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	pos, flags := parseFlags(args)
	if len(pos) == 0 {
		ctx.AddMessage("system", "Usage: /note <list|get|create|update|delete> [options]")
		return nil
	}
	sub := pos[0]

	switch sub {
	case "list":
		return noteList(ctx, flags)
	case "get":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /note get <id>")
			return nil
		}
		return noteGet(ctx, pos[1])
	case "create":
		return noteCreate(ctx, flags)
	case "update":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /note update <id> [--title] [--content] [--entity]")
			return nil
		}
		return noteUpdate(ctx, pos[1], flags)
	case "delete":
		if len(pos) < 2 {
			ctx.AddMessage("system", "Usage: /note delete <id>")
			return nil
		}
		return noteDelete(ctx, pos[1])
	default:
		ctx.AddMessage("system", fmt.Sprintf("Unknown note subcommand: %s", sub))
		return nil
	}
}

func noteList(ctx *SlashContext, flags map[string]string) error {
	path := "/v1/notes"
	if entity := getFlag(flags, "entity"); entity != "" {
		path += "?entity=" + url.QueryEscape(entity)
	}
	ctx.SetLoading(true)
	result, err := api.Get[[]map[string]any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.AddMessage("system", "No notes found.")
		return nil
	}
	headers := []string{"ID", "Title", "Entity"}
	var rows [][]string
	for _, n := range result {
		id := fmt.Sprintf("%v", n["id"])
		title, _ := n["title"].(string)
		entity, _ := n["entity_id"].(string)
		rows = append(rows, []string{id, title, entity})
	}
	ctx.AddMessage("system", render.RenderTable(headers, rows, 100))
	return nil
}

func noteGet(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	result, err := api.Get[map[string]any](ctx.APIClient, "/v1/notes/"+url.PathEscape(id), 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func noteCreate(ctx *SlashContext, flags map[string]string) error {
	title := getFlag(flags, "title")
	if title == "" {
		ctx.AddMessage("system", "Usage: /note create --title <title> [--content <c>] [--entity <id>]")
		return nil
	}
	body := map[string]any{"title": title}
	if c := getFlag(flags, "content"); c != "" {
		body["content"] = c
	}
	if e := getFlag(flags, "entity"); e != "" {
		body["entity_id"] = e
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/notes", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Note created.\n"+formatJSON(result))
	return nil
}

func noteUpdate(ctx *SlashContext, id string, flags map[string]string) error {
	body := map[string]any{}
	if t := getFlag(flags, "title"); t != "" {
		body["title"] = t
	}
	if c := getFlag(flags, "content"); c != "" {
		body["content"] = c
	}
	if e := getFlag(flags, "entity"); e != "" {
		body["entity_id"] = e
	}
	if len(body) == 0 {
		ctx.AddMessage("system", "Provide at least one of --title, --content, --entity to update.")
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Patch[map[string]any](ctx.APIClient, "/v1/notes/"+url.PathEscape(id), body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Note updated.\n"+formatJSON(result))
	return nil
}

func noteDelete(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	_, err := api.Delete[map[string]any](ctx.APIClient, "/v1/notes/"+url.PathEscape(id), 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", fmt.Sprintf("Note %s deleted.", id))
	return nil
}

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/tui/render"
)

func cmdObject(ctx *SlashContext, args string) error {
	positional, flags := parseFlags(args)

	sub := "list"
	if len(positional) > 0 {
		sub = positional[0]
	}

	switch sub {
	case "list":
		return objectList(ctx, flags)
	case "get":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /object get <slug>")
			return nil
		}
		return objectGet(ctx, positional[1])
	case "create":
		return objectCreate(ctx, flags)
	case "update":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /object update <slug> [--name] [--description] [--name-plural]")
			return nil
		}
		return objectUpdate(ctx, positional[1], flags)
	case "delete":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /object delete <slug>")
			return nil
		}
		return objectDelete(ctx, positional[1])
	default:
		ctx.AddMessage("system", "Unknown subcommand: "+sub+"\nUsage: /object [list|get|create|update|delete]")
		return nil
	}
}

func objectList(ctx *SlashContext, flags map[string]string) error {
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Get[[]map[string]any](ctx.APIClient, "/v1/objects", 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.AddMessage("system", "No object types found.")
		return nil
	}

	_, includeAttrs := flags["include-attributes"]

	headers := []string{"Name", "Slug", "Type"}
	var rows [][]string
	for _, obj := range result {
		name, _ := obj["name"].(string)
		slug, _ := obj["slug"].(string)
		objType, _ := obj["type"].(string)
		rows = append(rows, []string{name, slug, objType})
	}
	output := render.RenderTable(headers, rows, 100)

	if includeAttrs {
		var sb strings.Builder
		sb.WriteString(output)
		sb.WriteString("\nAttributes:\n")
		for _, obj := range result {
			name, _ := obj["name"].(string)
			attrs, _ := obj["attributes"].([]any)
			if len(attrs) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("\n  %s:\n", name))
			for _, a := range attrs {
				if am, ok := a.(map[string]any); ok {
					aName, _ := am["name"].(string)
					aType, _ := am["type"].(string)
					sb.WriteString(fmt.Sprintf("    • %s (%s)\n", aName, aType))
				}
			}
		}
		output = sb.String()
	}

	ctx.AddMessage("system", strings.TrimRight(output, "\n"))
	return nil
}

func objectGet(ctx *SlashContext, slug string) error {
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Get[map[string]any](ctx.APIClient, "/v1/objects/"+slug, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func objectCreate(ctx *SlashContext, flags map[string]string) error {
	name := getFlag(flags, "name")
	slug := getFlag(flags, "slug")
	if name == "" || slug == "" {
		ctx.AddMessage("system", "Usage: /object create --name <name> --slug <slug> [--type <type>] [--description <desc>]")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	body := map[string]any{
		"name": name,
		"slug": slug,
	}
	if t := getFlag(flags, "type"); t != "" {
		body["type"] = t
	}
	if d := getFlag(flags, "description"); d != "" {
		body["description"] = d
	}

	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/objects", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func objectUpdate(ctx *SlashContext, slug string, flags map[string]string) error {
	if !requireAuth(ctx) {
		return nil
	}

	body := map[string]any{}
	if v := getFlag(flags, "name"); v != "" {
		body["name"] = v
	}
	if v := getFlag(flags, "description"); v != "" {
		body["description"] = v
	}
	if v := getFlag(flags, "name-plural"); v != "" {
		body["name_plural"] = v
	}
	if len(body) == 0 {
		ctx.AddMessage("system", "Nothing to update. Provide at least one of --name, --description, --name-plural.")
		return nil
	}

	ctx.SetLoading(true)
	result, err := api.Patch[map[string]any](ctx.APIClient, "/v1/objects/"+slug, body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func objectDelete(ctx *SlashContext, slug string) error {
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	_, err := api.Delete[json.RawMessage](ctx.APIClient, "/v1/objects/"+slug, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Deleted.")
	return nil
}

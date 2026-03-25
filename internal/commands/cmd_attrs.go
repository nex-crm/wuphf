package commands

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/api"
)

func cmdAttribute(ctx *SlashContext, args string) error {
	positional, flags := parseFlags(args)
	if len(positional) == 0 {
		ctx.AddMessage("system", "Usage: /attribute <subcommand> <object> [<attr>] [flags]\n"+
			"  create <object> --name <n> --slug <s> --type <t> [--description] [--options <json>]\n"+
			"  update <object> <attr> [--name] [--description] [--options]\n"+
			"  delete <object> <attr>")
		return nil
	}

	if !requireAuth(ctx) {
		return nil
	}

	sub := positional[0]
	switch sub {
	case "create":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /attribute create <object> --name <n> --slug <s> --type <t> [--description] [--options <json>]")
			return nil
		}
		return attrCreate(ctx, positional[1], flags)
	case "update":
		if len(positional) < 3 {
			ctx.AddMessage("system", "Usage: /attribute update <object> <attr> [--name] [--description] [--options]")
			return nil
		}
		return attrUpdate(ctx, positional[1], positional[2], flags)
	case "delete":
		if len(positional) < 3 {
			ctx.AddMessage("system", "Usage: /attribute delete <object> <attr>")
			return nil
		}
		return attrDelete(ctx, positional[1], positional[2])
	default:
		ctx.AddMessage("system", fmt.Sprintf("Unknown attribute subcommand: %s", sub))
		return nil
	}
}

func attrCreate(ctx *SlashContext, object string, flags map[string]string) error {
	name := getFlag(flags, "name")
	slug := getFlag(flags, "slug")
	attrType := getFlag(flags, "type")
	if name == "" || slug == "" || attrType == "" {
		ctx.AddMessage("system", "Usage: /attribute create <object> --name <n> --slug <s> --type <t> [--description] [--options <json>]")
		return nil
	}
	body := map[string]any{
		"name": name,
		"slug": slug,
		"type": attrType,
	}
	if v := getFlag(flags, "description"); v != "" {
		body["description"] = v
	}
	if v := getFlag(flags, "options"); v != "" {
		body["options"] = v
	}

	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/objects/"+object+"/attributes", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func attrUpdate(ctx *SlashContext, object, attr string, flags map[string]string) error {
	body := map[string]any{}
	if v := getFlag(flags, "name"); v != "" {
		body["name"] = v
	}
	if v := getFlag(flags, "description"); v != "" {
		body["description"] = v
	}
	if v := getFlag(flags, "options"); v != "" {
		body["options"] = v
	}
	if len(body) == 0 {
		ctx.AddMessage("system", "Provide at least one flag to update: --name, --description, --options")
		return nil
	}

	ctx.SetLoading(true)
	result, err := api.Patch[map[string]any](ctx.APIClient, "/v1/objects/"+object+"/attributes/"+attr, body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func attrDelete(ctx *SlashContext, object, attr string) error {
	ctx.SetLoading(true)
	result, err := api.Delete[map[string]any](ctx.APIClient, "/v1/objects/"+object+"/attributes/"+attr, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

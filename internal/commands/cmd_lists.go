package commands

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/api"
)

func cmdList(ctx *SlashContext, args string) error {
	if !requireAuth(ctx) {
		return nil
	}

	pos, flags := parseFlags(args)
	if len(pos) == 0 {
		ctx.AddMessage("system", listUsage())
		return nil
	}

	sub := pos[0]
	switch sub {
	case "list":
		return listList(ctx, pos[1:], flags)
	case "get":
		return listGet(ctx, pos[1:])
	case "create":
		return listCreate(ctx, pos[1:], flags)
	case "delete":
		return listDelete(ctx, pos[1:])
	case "records":
		return listRecords(ctx, pos[1:], flags)
	case "add-member":
		return listAddMember(ctx, pos[1:], flags)
	case "upsert-member":
		return listUpsertMember(ctx, pos[1:], flags)
	case "remove-record":
		return listRemoveRecord(ctx, pos[1:])
	default:
		ctx.AddMessage("system", fmt.Sprintf("Unknown list subcommand %q.\n\n%s", sub, listUsage()))
		return nil
	}
}

func listUsage() string {
	return "Usage:\n" +
		"  /list list <object> [--include-attributes]  List lists for an object\n" +
		"  /list get <id>                               Get list details\n" +
		"  /list create <object> --name <n> --slug <s>  Create a list\n" +
		"  /list delete <id>                            Delete a list\n" +
		"  /list records <id> [--limit] [--offset] [--sort]  List records\n" +
		"  /list add-member <id> --data <json>          Add a record\n" +
		"  /list upsert-member <id> --match <attr> --data <json>  Upsert a record\n" +
		"  /list remove-record <id> <record-id>         Remove a record"
}

// listList — GET /v1/objects/{slug}/lists
func listList(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list list <object> [--include-attributes]")
		return nil
	}
	slug := pos[0]
	path := fmt.Sprintf("/v1/objects/%s/lists", slug)
	if flags["include-attributes"] == "true" {
		path += "?include_attributes=true"
	}

	ctx.SetLoading(true)
	result, err := api.Get[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listGet — GET /v1/lists/{id}
func listGet(ctx *SlashContext, pos []string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list get <id>")
		return nil
	}
	path := fmt.Sprintf("/v1/lists/%s", pos[0])

	ctx.SetLoading(true)
	result, err := api.Get[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listCreate — POST /v1/objects/{slug}/lists
func listCreate(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list create <object> --name <n> --slug <s> [--description <d>]")
		return nil
	}
	name := getFlag(flags, "name")
	slug := getFlag(flags, "slug")
	if name == "" || slug == "" {
		ctx.AddMessage("system", "--name and --slug are required.")
		return nil
	}

	body := map[string]any{
		"name": name,
		"slug": slug,
	}
	if desc := getFlag(flags, "description"); desc != "" {
		body["description"] = desc
	}

	objectSlug := pos[0]
	path := fmt.Sprintf("/v1/objects/%s/lists", objectSlug)

	ctx.SetLoading(true)
	result, err := api.Post[any](ctx.APIClient, path, body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listDelete — DELETE /v1/lists/{id}
func listDelete(ctx *SlashContext, pos []string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list delete <id>")
		return nil
	}
	path := fmt.Sprintf("/v1/lists/%s", pos[0])

	ctx.SetLoading(true)
	result, err := api.Delete[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listRecords — GET /v1/lists/{id}/records
func listRecords(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list records <id> [--limit N] [--offset N] [--sort field]")
		return nil
	}
	path := fmt.Sprintf("/v1/lists/%s/records", pos[0])

	var params []string
	if v := getFlag(flags, "limit"); v != "" {
		params = append(params, "limit="+v)
	}
	if v := getFlag(flags, "offset"); v != "" {
		params = append(params, "offset="+v)
	}
	if v := getFlag(flags, "sort"); v != "" {
		params = append(params, "sort="+v)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	ctx.SetLoading(true)
	result, err := api.Get[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listAddMember — POST /v1/lists/{id}/records
func listAddMember(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list add-member <id> --data <json>")
		return nil
	}
	data, err := parseData(flags)
	if err != nil {
		ctx.AddMessage("system", err.Error())
		return nil
	}

	path := fmt.Sprintf("/v1/lists/%s/records", pos[0])

	ctx.SetLoading(true)
	result, err := api.Post[any](ctx.APIClient, path, data, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listUpsertMember — POST /v1/lists/{id}/records/upsert
func listUpsertMember(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 1 {
		ctx.AddMessage("system", "Usage: /list upsert-member <id> --match <attr> --data <json>")
		return nil
	}
	match := getFlag(flags, "match")
	if match == "" {
		ctx.AddMessage("system", "--match is required.")
		return nil
	}
	data, err := parseData(flags)
	if err != nil {
		ctx.AddMessage("system", err.Error())
		return nil
	}

	body := map[string]any{
		"match_attribute": match,
		"data":            data,
	}

	path := fmt.Sprintf("/v1/lists/%s/records/upsert", pos[0])

	ctx.SetLoading(true)
	result, err := api.Post[any](ctx.APIClient, path, body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// listRemoveRecord — DELETE /v1/lists/{id}/records/{record-id}
func listRemoveRecord(ctx *SlashContext, pos []string) error {
	if len(pos) < 2 {
		ctx.AddMessage("system", "Usage: /list remove-record <id> <record-id>")
		return nil
	}
	path := fmt.Sprintf("/v1/lists/%s/records/%s", pos[0], pos[1])

	ctx.SetLoading(true)
	result, err := api.Delete[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

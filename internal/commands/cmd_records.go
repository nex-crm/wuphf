package commands

import (
	"fmt"
	"net/url"

	"github.com/nex-crm/wuphf/internal/api"
)

// cmdRecord dispatches record sub-commands: list, get, create, upsert, update, delete, timeline.
func cmdRecord(ctx *SlashContext, args string) error {
	pos, flags := parseFlags(args)
	if len(pos) == 0 {
		ctx.AddMessage("system", recordUsage())
		return nil
	}

	sub := pos[0]
	switch sub {
	case "list":
		return recordList(ctx, pos[1:], flags)
	case "get":
		return recordGet(ctx, pos[1:])
	case "create":
		return recordCreate(ctx, pos[1:], flags)
	case "upsert":
		return recordUpsert(ctx, pos[1:], flags)
	case "update":
		return recordUpdate(ctx, pos[1:], flags)
	case "delete":
		return recordDelete(ctx, pos[1:])
	case "timeline":
		return recordTimeline(ctx, pos[1:], flags)
	default:
		// If the first arg is not a known sub-command, treat it as a type for "list".
		return recordList(ctx, pos, flags)
	}
}

func recordUsage() string {
	return "Usage:\n" +
		"  /record list <type> [--limit N] [--offset N] [--sort <field>]\n" +
		"  /record get <type> <id>\n" +
		"  /record create <type> --data <json>\n" +
		"  /record upsert <type> --match <attr> --data <json>\n" +
		"  /record update <type> <id> --data <json>\n" +
		"  /record delete <type> <id>\n" +
		"  /record timeline <type> <id> [--limit N] [--cursor C]"
}

// recordList: GET /v1/records?object_type={type}&limit=N&offset=N&sort=field
func recordList(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) == 0 {
		ctx.AddMessage("system", "Usage: /record list <type> [--limit N] [--offset N] [--sort <field>]")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	objType := pos[0]
	q := url.Values{}
	q.Set("object_type", objType)
	if v := getFlag(flags, "limit"); v != "" {
		q.Set("limit", v)
	}
	if v := getFlag(flags, "offset"); v != "" {
		q.Set("offset", v)
	}
	if v := getFlag(flags, "sort"); v != "" {
		q.Set("sort", v)
	}

	ctx.SetLoading(true)
	result, err := api.Get[any](ctx.APIClient, "/v1/records?"+q.Encode(), 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordGet: GET /v1/records/{type}/{id}
func recordGet(ctx *SlashContext, pos []string) error {
	if len(pos) < 2 {
		ctx.AddMessage("system", "Usage: /record get <type> <id>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	path := fmt.Sprintf("/v1/records/%s/%s", url.PathEscape(pos[0]), url.PathEscape(pos[1]))
	ctx.SetLoading(true)
	result, err := api.Get[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordCreate: POST /v1/records/{type}
func recordCreate(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) == 0 {
		ctx.AddMessage("system", "Usage: /record create <type> --data <json>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	data, err := parseData(flags)
	if err != nil {
		ctx.AddMessage("system", err.Error())
		return nil
	}

	path := fmt.Sprintf("/v1/records/%s", url.PathEscape(pos[0]))
	ctx.SetLoading(true)
	result, err := api.Post[any](ctx.APIClient, path, data, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordUpsert: POST /v1/records/{type}/upsert with {match_attribute, data}
func recordUpsert(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) == 0 {
		ctx.AddMessage("system", "Usage: /record upsert <type> --match <attr> --data <json>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	match := getFlag(flags, "match")
	if match == "" {
		ctx.AddMessage("system", "--match flag required")
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
	path := fmt.Sprintf("/v1/records/%s/upsert", url.PathEscape(pos[0]))
	ctx.SetLoading(true)
	result, err := api.Post[any](ctx.APIClient, path, body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordUpdate: PATCH /v1/records/{type}/{id}
func recordUpdate(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 2 {
		ctx.AddMessage("system", "Usage: /record update <type> <id> --data <json>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	data, err := parseData(flags)
	if err != nil {
		ctx.AddMessage("system", err.Error())
		return nil
	}

	path := fmt.Sprintf("/v1/records/%s/%s", url.PathEscape(pos[0]), url.PathEscape(pos[1]))
	ctx.SetLoading(true)
	result, err := api.Patch[any](ctx.APIClient, path, data, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordDelete: DELETE /v1/records/{type}/{id}
func recordDelete(ctx *SlashContext, pos []string) error {
	if len(pos) < 2 {
		ctx.AddMessage("system", "Usage: /record delete <type> <id>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	path := fmt.Sprintf("/v1/records/%s/%s", url.PathEscape(pos[0]), url.PathEscape(pos[1]))
	ctx.SetLoading(true)
	result, err := api.Delete[any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

// recordTimeline: GET /v1/records/{type}/{id}/timeline
func recordTimeline(ctx *SlashContext, pos []string, flags map[string]string) error {
	if len(pos) < 2 {
		ctx.AddMessage("system", "Usage: /record timeline <type> <id> [--limit N] [--cursor C]")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}

	q := url.Values{}
	if v := getFlag(flags, "limit"); v != "" {
		q.Set("limit", v)
	}
	if v := getFlag(flags, "cursor"); v != "" {
		q.Set("cursor", v)
	}

	path := fmt.Sprintf("/v1/records/%s/%s/timeline", url.PathEscape(pos[0]), url.PathEscape(pos[1]))
	if len(q) > 0 {
		path += "?" + q.Encode()
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

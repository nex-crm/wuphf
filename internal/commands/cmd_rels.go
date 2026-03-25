package commands

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/api"
)

func cmdRel(ctx *SlashContext, args string) error {
	positional, flags := parseFlags(args)
	if len(positional) == 0 {
		ctx.AddMessage("system", "Usage: /rel <subcommand>\n"+
			"  list-defs                              List relationship definitions\n"+
			"  create-def --type <t> --entity1 <e1> --entity2 <e2> [--pred12] [--pred21]\n"+
			"  delete-def <id>                        Delete a relationship definition\n"+
			"  create --def <id> --entity1 <id> --entity2 <id>\n"+
			"  delete <id>                            Delete a relationship")
		return nil
	}

	if !requireAuth(ctx) {
		return nil
	}

	sub := positional[0]
	switch sub {
	case "list-defs":
		return relListDefs(ctx)
	case "create-def":
		return relCreateDef(ctx, flags)
	case "delete-def":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /rel delete-def <id>")
			return nil
		}
		return relDeleteDef(ctx, positional[1])
	case "create":
		return relCreate(ctx, flags)
	case "delete":
		if len(positional) < 2 {
			ctx.AddMessage("system", "Usage: /rel delete <id>")
			return nil
		}
		return relDelete(ctx, positional[1])
	default:
		ctx.AddMessage("system", fmt.Sprintf("Unknown rel subcommand: %s", sub))
		return nil
	}
}

func relListDefs(ctx *SlashContext) error {
	ctx.SetLoading(true)
	result, err := api.Get[[]map[string]any](ctx.APIClient, "/v1/relationships/definitions", 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.AddMessage("system", "No relationship definitions found.")
		return nil
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func relCreateDef(ctx *SlashContext, flags map[string]string) error {
	relType := getFlag(flags, "type")
	entity1 := getFlag(flags, "entity1")
	entity2 := getFlag(flags, "entity2")
	if relType == "" || entity1 == "" || entity2 == "" {
		ctx.AddMessage("system", "Usage: /rel create-def --type <t> --entity1 <e1> --entity2 <e2> [--pred12] [--pred21]")
		return nil
	}
	body := map[string]any{
		"type":    relType,
		"entity1": entity1,
		"entity2": entity2,
	}
	if v := getFlag(flags, "pred12"); v != "" {
		body["predicate_1_to_2"] = v
	}
	if v := getFlag(flags, "pred21"); v != "" {
		body["predicate_2_to_1"] = v
	}

	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/relationships/definitions", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func relDeleteDef(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	result, err := api.Delete[map[string]any](ctx.APIClient, "/v1/relationships/definitions/"+id, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func relCreate(ctx *SlashContext, flags map[string]string) error {
	def := getFlag(flags, "def")
	entity1 := getFlag(flags, "entity1")
	entity2 := getFlag(flags, "entity2")
	if def == "" || entity1 == "" || entity2 == "" {
		ctx.AddMessage("system", "Usage: /rel create --def <id> --entity1 <id> --entity2 <id>")
		return nil
	}
	body := map[string]any{
		"definition_id": def,
		"entity1_id":    entity1,
		"entity2_id":    entity2,
	}

	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/relationships", body, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

func relDelete(ctx *SlashContext, id string) error {
	ctx.SetLoading(true)
	result, err := api.Delete[map[string]any](ctx.APIClient, "/v1/relationships/"+id, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatJSON(result))
	return nil
}

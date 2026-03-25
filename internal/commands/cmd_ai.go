package commands

import (
	"github.com/nex-crm/wuphf/internal/api"
)

func cmdAsk(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /ask <question>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/context/ask", map[string]any{"query": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("agent", formatMapResult(result))
	return nil
}

func cmdSearch(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /search <query>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/search", map[string]any{"query": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatMapResult(result))
	return nil
}

func cmdRemember(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /remember <content>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	_, err := api.Post[map[string]any](ctx.APIClient, "/v1/context/text", map[string]any{"content": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Stored.")
	return nil
}

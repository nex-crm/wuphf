package provider

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// askRequest is the payload for the /ask endpoint.
type askRequest struct {
	Question string `json:"question"`
}

// askResponse is the response from the /ask endpoint.
type askResponse struct {
	Answer string `json:"answer"`
}

// CreateNexAskStreamFn returns a StreamFn that calls the Nex /ask API.
// If no API key is configured, it echoes the message back with a warning.
func CreateNexAskStreamFn(apiClient *api.Client) agent.StreamFn {
	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk, 64)
		go func() {
			defer close(ch)

			if config.ResolveNoNex() {
				ch <- agent.StreamChunk{Type: "error", Content: "Nex integration is disabled for this session (--no-nex)."}
				return
			}

			if !apiClient.IsAuthenticated() {
				var echo string
				if len(msgs) > 0 {
					echo = msgs[len(msgs)-1].Content
				}
				ch <- agent.StreamChunk{
					Type:    "text",
					Content: fmt.Sprintf("[No API key configured] %s", echo),
				}
				return
			}

			var question string
			if len(msgs) > 0 {
				question = msgs[len(msgs)-1].Content
			}

			resp, err := api.Post[askResponse](apiClient, "/ask", askRequest{Question: question}, 0)
			if err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: fmt.Sprintf("nex ask: %v", err)}
				return
			}

			ch <- agent.StreamChunk{Type: "text", Content: resp.Answer}
		}()
		return ch
	}
}

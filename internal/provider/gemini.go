package provider

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/nex-crm/wuphf/internal/agent"
)

const geminiModel = "gemini-2.0-flash"

// CreateGeminiStreamFn returns a StreamFn backed by the Gemini API.
func CreateGeminiStreamFn(apiKey string) agent.StreamFn {
	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk, 64)
		go func() {
			defer close(ch)

			ctx := context.Background()
			client, err := genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:  apiKey,
				Backend: genai.BackendGeminiAPI,
			})
			if err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: fmt.Sprintf("Gemini client initialization failed: %v. Check your API key and network connection.", err)}
				return
			}

			contents := msgsToGenAIContents(msgs)
			config := &genai.GenerateContentConfig{}

			if len(tools) > 0 {
				config.Tools = []*genai.Tool{agentToolsToGenAI(tools)}
			}

			stream := client.Models.GenerateContentStream(ctx, geminiModel, contents, config)
			for result, err := range stream {
				if err != nil {
					ch <- agent.StreamChunk{Type: "error", Content: fmt.Sprintf("Gemini streaming failed: %v. The model may be unavailable or the request was rejected.", err)}
					return
				}
				for _, cand := range result.Candidates {
					if cand.Content == nil {
						continue
					}
					for _, part := range cand.Content.Parts {
						if txt := part.Text; txt != "" {
							ch <- agent.StreamChunk{Type: "text", Content: txt}
						}
					}
				}
			}
		}()
		return ch
	}
}

// msgsToGenAIContents converts agent messages to the genai Content slice.
// Gemini requires alternating user/model turns.
func msgsToGenAIContents(msgs []agent.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(msgs))
	for _, m := range msgs {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: m.Content}},
		})
	}
	return contents
}

// agentToolsToGenAI converts AgentTools to a single genai.Tool with function declarations.
func agentToolsToGenAI(tools []agent.AgentTool) *genai.Tool {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
		})
	}
	return &genai.Tool{FunctionDeclarations: decls}
}

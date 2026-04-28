package imagegen

import (
	"strings"
	"context"
	"fmt"
)

// stub is a placeholder Provider used for backends whose full
// implementation hasn't shipped yet. Status() reports they exist so the
// Settings panel can list them, but Generate() returns a clear error so
// callers know they're not wired.

type stub struct {
	kind         Kind
	label        string
	blurb        string
	defaultModel string
	supportsVid  bool
	needsKey     bool
	setupHint    string
}

func (s *stub) Kind() Kind { return s.kind }

func (s *stub) Status(_ context.Context) Status {
	apiKey := strings.TrimSpace(configString(string(s.kind), "api_key"))
	return Status{
		Kind:             s.kind,
		Label:            s.label,
		Blurb:            s.blurb,
		BaseURL:          configString(string(s.kind), "base_url"),
		DefaultModel:     s.defaultModel,
		SupportsImage:    !s.supportsVid || true,
		SupportsVideo:    s.supportsVid,
		NeedsAPIKey:      s.needsKey,
		APIKeySet:        apiKey != "",
		Configured:       apiKey != "",
		Reachable:        false,
		ImplementationOK: false,
		SetupHint:        s.setupHint,
	}
}

func (s *stub) Generate(_ context.Context, _ Request) (Result, error) {
	return Result{}, fmt.Errorf("%s: not yet implemented (%s)", s.kind, s.setupHint)
}

func init() {
	Register(&stub{
		kind:         KindGPTImage,
		label:        "ChatGPT Image",
		blurb:        "OpenAI gpt-image-1 — high-quality image generation via the OpenAI Images API.",
		defaultModel: "gpt-image-1",
		needsKey:     true,
		setupHint:    "Add OPENAI_API_KEY in Settings → Image generation → ChatGPT Image.",
	})
	Register(&stub{
		kind:         KindSeedance,
		label:        "Seedance 2",
		blurb:        "ByteDance Seedance 2 — text-to-video and image-to-video.",
		defaultModel: "seedance-2-pro",
		supportsVid:  true,
		needsKey:     true,
		setupHint:    "Available via fal.ai or Volcengine. Add provider key in Settings.",
	})
	Register(&stub{
		kind:         KindComfyUI,
		label:        "ComfyUI",
		blurb:        "Self-hosted ComfyUI — node-based image gen, runs on your own GPU. Recommended host: .14 (RTX 5080).",
		defaultModel: "flux1-dev",
		needsKey:     false,
		setupHint:    "Install ComfyUI on .14 (RTX 5080) and set base_url to http://192.168.88.14:8188 in Settings.",
	})
}

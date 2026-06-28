package gbrain

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// ollamaListTimeout bounds the `ollama list` probe used for local embedding
// model discovery. It is intentionally short: discovery runs on setup paths
// where a slow or wedged ollama must not stall the office launch.
const ollamaListTimeout = 3 * time.Second

// openAIEmbeddingModel is the gbrain `--embedding-model` value used when an
// OpenAI key is configured. OpenAI's key serves both chat and embeddings, so
// it is the strongest available embedder.
const openAIEmbeddingModel = "openai:text-embedding-3-large"

// Test seams. These default to the real implementations and are overridden in
// unit tests so embedding selection can be exercised without a live ollama
// binary, OpenAI credentials, or a real gbrain subprocess.
var (
	selectOpenAIKey      = config.ResolveOpenAIAPIKey
	ollamaEmbeddingModel = detectOllamaEmbeddingModel
	runGBrain            = Run
)

// OllamaEmbeddingModel returns the name of a locally-pulled Ollama embedding
// model suitable for gbrain, or "" when ollama is not on PATH or no embedding
// model is pulled. It prefers "nomic-embed-text"; otherwise it returns any
// pulled model whose name contains "embed". It never pulls a model (no network
// side effects) — it only inspects what is already present.
func OllamaEmbeddingModel() string {
	return ollamaEmbeddingModel()
}

func detectOllamaEmbeddingModel() string {
	if _, err := exec.LookPath("ollama"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), ollamaListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ollama", "list")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return parseOllamaEmbeddingModel(stdout.String())
}

// parseOllamaEmbeddingModel extracts the best embedding model name from the
// output of `ollama list`. The first column is the model NAME; the header row
// (NAME ...) is skipped because it contains no "embed" token. A trailing
// ":latest" tag is dropped because gbrain accepts the bare name.
func parseOllamaEmbeddingModel(listing string) string {
	var fallback string
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		lower := strings.ToLower(name)
		model := strings.TrimSuffix(name, ":latest")
		if strings.Contains(lower, "nomic-embed-text") {
			return model
		}
		if fallback == "" && strings.Contains(lower, "embed") {
			fallback = model
		}
	}
	return fallback
}

// SelectEmbeddingModel returns the best available gbrain `--embedding-model`
// value, in precedence order:
//  1. OpenAI key configured -> "openai:text-embedding-3-large".
//  2. A local Ollama embedding model pulled -> "ollama:<model>".
//  3. Otherwise "" -> no embedding provider (gbrain runs keyword-only).
//
// An Anthropic key alone yields "": Anthropic has no embeddings API, so it
// cannot serve semantic retrieval.
func SelectEmbeddingModel() string {
	if strings.TrimSpace(selectOpenAIKey()) != "" {
		return openAIEmbeddingModel
	}
	if model := strings.TrimSpace(OllamaEmbeddingModel()); model != "" {
		return "ollama:" + model
	}
	return ""
}

// EmbeddingAvailable reports whether gbrain can perform semantic (vector)
// retrieval — i.e. an OpenAI key or a local Ollama embedder is available. An
// Anthropic key alone returns false.
func EmbeddingAvailable() bool {
	return SelectEmbeddingModel() != ""
}

// BrainConfigured reports whether a gbrain brain already exists. It runs a
// cheap read (`gbrain config get embedding_model`) which fails with "No brain
// configured" when none has been initialized. Any other outcome is treated as
// "a brain exists" so EnsureBrain never re-inits over a working brain.
func BrainConfigured(ctx context.Context) bool {
	out, err := runGBrain(ctx, "config", "get", "embedding_model")
	if err != nil {
		return !indicatesNoBrain(err.Error())
	}
	return !indicatesNoBrain(out)
}

func indicatesNoBrain(s string) bool {
	return strings.Contains(strings.ToLower(s), "no brain configured")
}

// EnsureBrain idempotently ensures a gbrain brain exists, selecting the best
// available embedder. It is strictly idempotent: when a brain already exists it
// returns the current embedding_model (best-effort) without re-initializing.
// Only when no brain exists does it run `gbrain init --pglite` with the
// selected embedder (or `--no-embedding` when none is available).
//
// EnsureBrain MUST NOT run on every boot. Call it only from explicit setup
// (the /init flow) or lazily-once when gbrain is the selected backend and no
// brain exists yet.
func EnsureBrain(ctx context.Context) (string, error) {
	if BrainConfigured(ctx) {
		model, _ := runGBrain(ctx, "config", "get", "embedding_model")
		return strings.TrimSpace(model), nil
	}
	selected := SelectEmbeddingModel()
	args := []string{"init", "--pglite"}
	if selected != "" {
		args = append(args, "--embedding-model", selected)
	} else {
		args = append(args, "--no-embedding")
	}
	if _, err := runGBrain(ctx, args...); err != nil {
		return "", fmt.Errorf("gbrain init: %w", err)
	}
	mode := selected
	if mode == "" {
		mode = "keyword-only"
	}
	slog.Info("gbrain: initialized brain", "embeddings", mode)
	return selected, nil
}

package embedding

import "maps"

// ModelAlias maps a short name to a Hugging Face repo + file.
type ModelAlias struct {
	Repo    string // HF repo ID (e.g. "nomic-ai/nomic-embed-code-GGUF")
	File    string // GGUF filename (e.g. "nomic-embed-code-Q8_0.gguf")
	Default bool   // true for the default model
}

// knownAliases maps short names to HF model coordinates.
var knownAliases = map[string]ModelAlias{
	"jina-code":  {Repo: "ggml-org/jina-embeddings-v2-base-code-Q8_0-GGUF", File: "jina-embeddings-v2-base-code-q8_0.gguf"},
	"nomic-text": {Repo: "nomic-ai/nomic-embed-text-v1.5-GGUF", File: "nomic-embed-text-v1.5.Q8_0.gguf"},
	"nomic-code": {Repo: "nomic-ai/nomic-embed-code-GGUF", File: "nomic-embed-code-Q8_0.gguf"},
	"qwen3":      {Repo: "Qwen/Qwen3-Embedding-0.6B-GGUF", File: "Qwen3-Embedding-0.6B-Q8_0.gguf"},
	"bge-base":   {Repo: "CompendiumLabs/bge-base-en-v1.5-gguf", File: "bge-base-en-v1.5-q8_0.gguf"},
	"bge-small":  {Repo: "CompendiumLabs/bge-small-en-v1.5-gguf", File: "bge-small-en-v1.5-q8_0.gguf", Default: true},
}

// DefaultAlias returns the alias name of the default model.
func DefaultAlias() string {
	for name, a := range knownAliases {
		if a.Default {
			return name
		}
	}
	return "bge-small"
}

// LookupAlias returns the alias info for a short name, or ok=false.
func LookupAlias(name string) (ModelAlias, bool) {
	a, ok := knownAliases[name]
	return a, ok
}

// ListAliases returns all known alias names.
func ListAliases() map[string]ModelAlias {
	out := make(map[string]ModelAlias, len(knownAliases))
	maps.Copy(out, knownAliases)
	return out
}

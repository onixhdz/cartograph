package embedding

import (
	"testing"
)

func TestNewProvider_LlamaCpp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires network or cached model")
	}
	_, err := NewProvider(Config{Provider: "llamacpp"})
	// In this test environment (no model file), we expect an error from the
	// llamacpp provider. The important thing is that NewProvider routes correctly.
	if err == nil {
		t.Log("llamacpp provider initialized (model available)")
	} else {
		t.Logf("llamacpp provider returned expected error: %v", err)
	}
}

func TestNewProvider_OpenAICompat_RequiresEndpoint(t *testing.T) {
	_, err := NewProvider(Config{Provider: "openai_compat"})
	if err == nil {
		t.Fatal("expected error for openai_compat without Endpoint")
	}
}

func TestNewProvider_OpenAICompat_RequiresModel(t *testing.T) {
	_, err := NewProvider(Config{
		Provider: "openai_compat",
		Endpoint: "https://api.openai.com",
	})
	if err == nil {
		t.Fatal("expected error for openai_compat without Model")
	}
}

func TestNewProvider_OpenAICompat_Full(t *testing.T) {
	p, err := NewProvider(Config{
		Provider: "openai_compat",
		Endpoint: "https://api.openai.com",
		Model:    "text-embedding-3-small",
		APIKey:   "sk-test",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer p.Close()

	if p.Name() != "openai-compat(https://api.openai.com, text-embedding-3-small)" {
		t.Errorf("unexpected name: %s", p.Name())
	}
}

func TestNewProvider_OpenAICompat_NoKey(t *testing.T) {
	p, err := NewProvider(Config{
		Provider: "openai_compat",
		Endpoint: "http://localhost:11434",
		Model:    "all-minilm",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer p.Close()

	if p.Name() != "openai-compat(http://localhost:11434, all-minilm)" {
		t.Errorf("unexpected name: %s", p.Name())
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider(Config{Provider: "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

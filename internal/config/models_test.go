package config

import "testing"

func TestResolveDirectModel(t *testing.T) {
	r := NewModelResolver(Config{DefaultModel: "gemini-3.1-pro-preview", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}})
	m, err := r.Resolve("gemini-3.1-pro-preview")
	if err != nil {
		t.Fatal(err)
	}
	if m != "gemini-3.1-pro-preview" {
		t.Fatalf("got %s", m)
	}
}

func TestRejectUnknown(t *testing.T) {
	r := NewModelResolver(Config{DefaultModel: "gemini-3.1-pro-preview", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}})
	_, err := r.Resolve("gpt-5")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAllowAnyGemini(t *testing.T) {
	r := NewModelResolver(Config{DefaultModel: "gemini-3.1-pro-preview", AllowedModels: []string{}, AllowAnyGeminiModel: true, ModelAliases: map[string]string{}})
	m, err := r.Resolve("gemini-any-new-model")
	if err != nil {
		t.Fatal(err)
	}
	if m != "gemini-any-new-model" {
		t.Fatalf("got %s", m)
	}
}

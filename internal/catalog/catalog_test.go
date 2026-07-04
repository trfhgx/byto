package catalog

import "testing"

func TestMergeSupportedPreservesVerifiedModelsAndAddsNewModelsDisabled(t *testing.T) {
	c := Catalog{Models: []Model{{
		ID:        "gemini-known",
		Enabled:   true,
		Available: true,
		Capabilities: Capabilities{
			ReasoningEffort: []string{"low", "medium"},
		},
	}, {
		ID:        "gemini-disabled",
		Enabled:   false,
		Available: true,
	}, {
		ID:        "gemini-old",
		Enabled:   true,
		Available: true,
	}}}
	c.MergeSupported([]LiveModel{
		{ID: "gemini-known", DisplayName: "Known"},
		{ID: "gemini-disabled", DisplayName: "Disabled"},
		{ID: "gemini-new", DisplayName: "New"},
	})
	ids := c.EnabledAvailableIDs()
	if len(ids) != 1 || ids[0] != "gemini-known" {
		t.Fatalf("enabled ids = %#v", ids)
	}
	var foundNew bool
	for _, m := range c.Models {
		if m.ID == "gemini-known" && len(m.Capabilities.ReasoningEffort) != 2 {
			t.Fatalf("metadata was not preserved: %#v", m.Capabilities)
		}
		if m.ID == "gemini-new" {
			foundNew = true
			if m.Enabled || m.Available {
				t.Fatal("new supported model should stay disabled and unavailable until verified")
			}
		}
		if m.ID == "gemini-disabled" && m.Enabled {
			t.Fatal("existing disabled model should stay disabled after live refresh")
		}
		if m.ID == "gemini-old" && m.Available {
			t.Fatal("model missing from supported list should be marked unavailable")
		}
	}
	if !foundNew {
		t.Fatal("new supported model was not added")
	}
}

func TestMergeSupportedPreservesVertexOpenAIModels(t *testing.T) {
	c := Catalog{Models: []Model{{
		ID:        "xai/grok-4.20-reasoning",
		Publisher: "xai",
		Runtime:   "vertex_openai",
		Enabled:   true,
		Available: true,
	}, {
		ID:        "gemini-old",
		Enabled:   true,
		Available: true,
	}}}
	c.MergeSupported([]LiveModel{{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash"}})

	for _, m := range c.Models {
		switch m.ID {
		case "xai/grok-4.20-reasoning":
			if !m.Enabled || !m.Available {
				t.Fatalf("vertex openai model should remain enabled and available: %#v", m)
			}
		case "gemini-old":
			if m.Available {
				t.Fatal("stale Gemini model should still be marked unavailable")
			}
		}
	}
}

func TestSupportedVertexOpenAIModelsContainsOnlyLiveProvenIDs(t *testing.T) {
	got := map[string]bool{}
	for _, m := range SupportedVertexOpenAIModels() {
		got[m.ID] = true
		if m.Runtime != "vertex_openai" {
			t.Fatalf("%s runtime = %q", m.ID, m.Runtime)
		}
	}
	want := []string{
		"xai/grok-4.20-reasoning",
		"xai/grok-4.20-non-reasoning",
		"xai/grok-4.1-fast-reasoning",
		"xai/grok-4.1-fast-non-reasoning",
		"qwen/qwen3-next-80b-a3b-instruct-maas",
		"qwen/qwen3-next-80b-a3b-thinking-maas",
		"qwen/qwen3-coder-480b-a35b-instruct-maas",
		"qwen/qwen3-235b-a22b-instruct-2507-maas",
		"openai/gpt-oss-120b-maas",
		"openai/gpt-oss-20b-maas",
		"moonshotai/kimi-k2-thinking-maas",
		"zai-org/glm-5-maas",
		"zai-org/glm-4.7-maas",
		"google/gemma-4-26b-a4b-it-maas",
		"minimaxai/minimax-m2-maas",
	}
	if len(got) != len(want) {
		t.Fatalf("supported vertex openai count = %d, want %d", len(got), len(want))
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("missing live-proven model %s", id)
		}
	}
	for _, id := range []string{
		"anthropic/claude-haiku-4.5",
		"mistralai/mistral-large",
		"meta/llama-3.1-405b",
		"deepseek-ai/deepseek-r1",
		"xai/grok-4.3",
	} {
		if got[id] || IsSupportedVertexOpenAIModel(id) {
			t.Fatalf("unproven model %s must not be supported", id)
		}
	}
}

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

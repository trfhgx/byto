package catalog

import "testing"

func TestMergeLivePreservesMetadataAndDisablesUnknown(t *testing.T) {
	c := Catalog{Models: []Model{{
		ID:        "gemini-known",
		Enabled:   true,
		Available: true,
		Capabilities: Capabilities{
			ReasoningEffort: []string{"low", "medium"},
		},
	}}}
	c.MergeLive([]LiveModel{
		{ID: "gemini-known", DisplayName: "Known"},
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
			if m.Enabled {
				t.Fatal("new live model should not be auto-enabled")
			}
			if !m.Available {
				t.Fatal("new live model should be marked available")
			}
		}
	}
	if !foundNew {
		t.Fatal("new live model was not added")
	}
}

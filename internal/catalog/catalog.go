package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Catalog struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source"`
	Models    []Model   `json:"models"`
}

type Model struct {
	ID               string       `json:"id"`
	DisplayName      string       `json:"display_name,omitempty"`
	Publisher        string       `json:"publisher,omitempty"`
	Family           string       `json:"family,omitempty"`
	Enabled          bool         `json:"enabled"`
	Available        bool         `json:"available"`
	LaunchStage      string       `json:"launch_stage,omitempty"`
	VersionState     string       `json:"version_state,omitempty"`
	SupportedActions []string     `json:"supported_actions,omitempty"`
	Capabilities     Capabilities `json:"capabilities,omitempty"`
	Notes            string       `json:"notes,omitempty"`
	LastSeenAt       *time.Time   `json:"last_seen_at,omitempty"`
}

type Capabilities struct {
	ReasoningEffort []string `json:"reasoning_effort,omitempty"`
	Input           []string `json:"input,omitempty"`
	Output          []string `json:"output,omitempty"`
	Streaming       bool     `json:"streaming,omitempty"`
	Tools           bool     `json:"tools,omitempty"`
	JSONMode        bool     `json:"json_mode,omitempty"`
}

type LiveModel struct {
	ID               string
	DisplayName      string
	Publisher        string
	LaunchStage      string
	VersionState     string
	SupportedActions []string
}

func Load(path string) (Catalog, error) {
	var c Catalog
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func Save(path string, c Catalog) error {
	c.UpdatedAt = time.Now().UTC()
	sort.Slice(c.Models, func(i, j int) bool { return c.Models[i].ID < c.Models[j].ID })
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Catalog) EnabledAvailableIDs() []string {
	out := make([]string, 0, len(c.Models))
	for _, m := range c.Models {
		if m.Enabled && m.Available && strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Catalog) MergeLive(live []LiveModel) {
	now := time.Now().UTC()
	seen := map[string]LiveModel{}
	for _, m := range live {
		id := strings.TrimSpace(m.ID)
		if id == "" || !strings.HasPrefix(id, "gemini-") {
			continue
		}
		m.ID = id
		seen[id] = m
	}

	index := map[string]int{}
	for i := range c.Models {
		index[c.Models[i].ID] = i
		if _, ok := seen[c.Models[i].ID]; !ok {
			c.Models[i].Available = false
		}
	}

	for id, liveModel := range seen {
		if i, ok := index[id]; ok {
			c.Models[i].Available = true
			c.Models[i].DisplayName = firstNonEmpty(liveModel.DisplayName, c.Models[i].DisplayName)
			c.Models[i].Publisher = firstNonEmpty(liveModel.Publisher, c.Models[i].Publisher, "google")
			c.Models[i].LaunchStage = liveModel.LaunchStage
			c.Models[i].VersionState = liveModel.VersionState
			c.Models[i].SupportedActions = liveModel.SupportedActions
			c.Models[i].LastSeenAt = &now
			continue
		}
		c.Models = append(c.Models, Model{
			ID:               id,
			DisplayName:      liveModel.DisplayName,
			Publisher:        firstNonEmpty(liveModel.Publisher, "google"),
			Family:           inferFamily(id),
			Enabled:          false,
			Available:        true,
			LaunchStage:      liveModel.LaunchStage,
			VersionState:     liveModel.VersionState,
			SupportedActions: liveModel.SupportedActions,
			Notes:            "Discovered from Vertex live catalog. Review capabilities before enabling.",
			LastSeenAt:       &now,
		})
	}
	c.Source = "vertex-live-refresh"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func inferFamily(id string) string {
	switch {
	case strings.Contains(id, "flash"):
		return "gemini-flash"
	case strings.Contains(id, "pro"):
		return "gemini-pro"
	default:
		return "gemini"
	}
}

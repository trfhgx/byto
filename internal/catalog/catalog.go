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
	Runtime          string       `json:"runtime,omitempty"`
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
	ReasoningEffort      []string       `json:"reasoning_effort,omitempty"`
	ReasoningBudgets     map[string]int `json:"reasoning_budgets,omitempty"`
	Input                []string       `json:"input,omitempty"`
	Output               []string       `json:"output,omitempty"`
	Streaming            bool           `json:"streaming,omitempty"`
	Tools                bool           `json:"tools,omitempty"`
	JSONMode             bool           `json:"json_mode,omitempty"`
	GenerationParameters []string       `json:"generation_parameters,omitempty"`
}

type LiveModel struct {
	ID               string
	DisplayName      string
	Publisher        string
	LaunchStage      string
	VersionState     string
	SupportedActions []string
}

func SupportedGoogleGeminiModels() []LiveModel {
	return []LiveModel{
		{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", Publisher: "google", LaunchStage: "GA"},
		{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", Publisher: "google", LaunchStage: "GA"},
		{ID: "gemini-2.5-flash-lite-preview-09-2025", DisplayName: "Gemini 2.5 Flash-Lite Preview", Publisher: "google", LaunchStage: "PUBLIC_PREVIEW"},
		{ID: "gemini-2.5-flash-preview-09-2025", DisplayName: "Gemini 2.5 Flash Preview", Publisher: "google", LaunchStage: "PUBLIC_PREVIEW"},
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Publisher: "google", LaunchStage: "GA"},
		{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", Publisher: "google", LaunchStage: "PUBLIC_PREVIEW"},
		{ID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash-Lite", Publisher: "google", LaunchStage: "GA"},
		{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", Publisher: "google", LaunchStage: "PUBLIC_PREVIEW"},
		{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", Publisher: "google", LaunchStage: "GA"},
	}
}

func SupportedVertexOpenAIModels() []Model {
	now := time.Now().UTC()
	models := []Model{
		vertexOpenAIModel("xai/grok-4.20-reasoning", "Grok 4.20 Reasoning", "xai", "grok", []string{"reasoning"}),
		vertexOpenAIModel("xai/grok-4.20-non-reasoning", "Grok 4.20 Non-Reasoning", "xai", "grok", nil),
		vertexOpenAIModel("xai/grok-4.1-fast-reasoning", "Grok 4.1 Fast Reasoning", "xai", "grok", []string{"reasoning"}),
		vertexOpenAIModel("xai/grok-4.1-fast-non-reasoning", "Grok 4.1 Fast Non-Reasoning", "xai", "grok", nil),
		vertexOpenAIModel("qwen/qwen3-next-80b-a3b-instruct-maas", "Qwen3 Next 80B A3B Instruct MaaS", "qwen", "qwen", nil),
		vertexOpenAIModel("qwen/qwen3-next-80b-a3b-thinking-maas", "Qwen3 Next 80B A3B Thinking MaaS", "qwen", "qwen", []string{"thinking"}),
		vertexOpenAIModel("qwen/qwen3-coder-480b-a35b-instruct-maas", "Qwen3 Coder 480B A35B Instruct MaaS", "qwen", "qwen", nil),
		vertexOpenAIModel("qwen/qwen3-235b-a22b-instruct-2507-maas", "Qwen3 235B A22B Instruct MaaS", "qwen", "qwen", nil),
		vertexOpenAIModel("openai/gpt-oss-120b-maas", "GPT-OSS 120B MaaS", "openai", "gpt-oss", nil),
		vertexOpenAIModel("openai/gpt-oss-20b-maas", "GPT-OSS 20B MaaS", "openai", "gpt-oss", nil),
		vertexOpenAIModel("moonshotai/kimi-k2-thinking-maas", "Kimi K2 Thinking MaaS", "moonshotai", "kimi", []string{"thinking"}),
		vertexOpenAIModel("zai-org/glm-5-maas", "GLM 5 MaaS", "zai-org", "glm", nil),
		vertexOpenAIModel("zai-org/glm-4.7-maas", "GLM 4.7 MaaS", "zai-org", "glm", nil),
		vertexOpenAIModel("google/gemma-4-26b-a4b-it-maas", "Gemma 4 26B A4B IT MaaS", "google", "gemma", nil),
		vertexOpenAIModel("minimaxai/minimax-m2-maas", "MiniMax M2 MaaS", "minimaxai", "minimax", nil),
	}
	for i := range models {
		models[i].LastSeenAt = &now
	}
	return models
}

func IsSupportedVertexOpenAIModel(id string) bool {
	id = strings.TrimSpace(id)
	for _, model := range SupportedVertexOpenAIModels() {
		if model.ID == id {
			return true
		}
	}
	return false
}

func vertexOpenAIModel(id, displayName, publisher, family string, reasoning []string) Model {
	caps := Capabilities{
		Input:                []string{"text"},
		Output:               []string{"text"},
		Streaming:            true,
		GenerationParameters: []string{"max_tokens", "temperature", "top_p", "stop", "frequency_penalty", "presence_penalty", "seed"},
	}
	if len(reasoning) > 0 {
		caps.ReasoningEffort = []string{"low", "medium", "high"}
	}
	return Model{
		ID:               id,
		DisplayName:      displayName,
		Publisher:        publisher,
		Family:           family,
		Runtime:          "vertex_openai",
		Enabled:          true,
		Available:        true,
		LaunchStage:      "GA",
		SupportedActions: []string{"chat.completions", "chat.completions.stream"},
		Capabilities:     caps,
		Notes:            "Live-proven through the Vertex / Gemini Enterprise Agent Platform OpenAI-compatible chat completions endpoint.",
	}
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

func (c *Catalog) MergeSupported(models []LiveModel) {
	now := time.Now().UTC()
	seen := map[string]LiveModel{}
	for _, m := range models {
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
		if c.Models[i].Runtime == "vertex_openai" {
			continue
		}
		if _, ok := seen[c.Models[i].ID]; !ok {
			c.Models[i].Available = false
		}
	}

	for id, liveModel := range seen {
		if i, ok := index[id]; ok {
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
			Available:        false,
			LaunchStage:      liveModel.LaunchStage,
			VersionState:     liveModel.VersionState,
			SupportedActions: liveModel.SupportedActions,
			Notes:            "Discovered from the supported Google Gemini endpoint model list. Run live verification before enabling.",
			LastSeenAt:       &now,
		})
	}
	c.Source = "google-supported-gemini-models"
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

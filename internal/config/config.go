package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Project               string            `json:"project"`
	Location              string            `json:"location"`
	DefaultModel          string            `json:"default_model"`
	AllowedModels         []string          `json:"allowed_models"`
	AllowAnyGeminiModel   bool              `json:"allow_any_gemini_model"`
	ModelAliases          map[string]string `json:"model_aliases"`
	VertexBaseURL         string            `json:"vertex_base_url"`
	GatewayAPIKeys        []string          `json:"gateway_api_keys"`
	LogPath               string            `json:"log_path"`
	RequestTimeoutSeconds int               `json:"request_timeout_seconds"`
}

func Load() (Config, error) {
	cfg := defaults()
	if p := os.Getenv("CONFIG_FILE"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config file: %w", err)
		}
	}
	overrideFromEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		Location:              "global",
		DefaultModel:          "gemini-3.1-pro-preview",
		AllowedModels:         []string{"gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools", "gemini-3-flash-preview"},
		AllowAnyGeminiModel:   false,
		ModelAliases:          map[string]string{},
		VertexBaseURL:         "https://aiplatform.googleapis.com",
		GatewayAPIKeys:        []string{"dev-local-key"},
		LogPath:               "logs/requests.jsonl",
		RequestTimeoutSeconds: 180,
	}
}

func overrideFromEnv(c *Config) {
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		c.Project = v
	}
	if v := os.Getenv("GOOGLE_CLOUD_LOCATION"); v != "" {
		c.Location = v
	}
	if v := os.Getenv("DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := os.Getenv("ALLOWED_MODELS"); v != "" {
		c.AllowedModels = splitCSV(v)
	}
	if v := os.Getenv("ALLOW_ANY_GEMINI_MODEL"); v != "" {
		c.AllowAnyGeminiModel = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("MODEL_ALIASES"); v != "" {
		c.ModelAliases = parseAliases(v)
	}
	if v := os.Getenv("VERTEX_BASE_URL"); v != "" {
		c.VertexBaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("GATEWAY_API_KEYS"); v != "" {
		c.GatewayAPIKeys = splitCSV(v)
	}
	if v := os.Getenv("LOG_PATH"); v != "" {
		c.LogPath = v
	}
	if v := os.Getenv("REQUEST_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.RequestTimeoutSeconds = n
		}
	}
}

func (c Config) Validate() error {
	if c.Project == "" {
		return errors.New("GOOGLE_CLOUD_PROJECT is required")
	}
	if c.Location == "" {
		return errors.New("GOOGLE_CLOUD_LOCATION is required")
	}
	if c.DefaultModel == "" {
		return errors.New("DEFAULT_MODEL is required")
	}
	if len(c.GatewayAPIKeys) == 0 {
		return errors.New("GATEWAY_API_KEYS must contain at least one key")
	}
	if c.VertexBaseURL == "" {
		return errors.New("VERTEX_BASE_URL is required")
	}
	if c.RequestTimeoutSeconds <= 0 {
		return errors.New("REQUEST_TIMEOUT_SECONDS must be positive")
	}
	return nil
}

func (c Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseAliases(s string) map[string]string {
	out := map[string]string{}
	for _, p := range splitCSV(s) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

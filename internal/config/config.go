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
	Project                     string            `json:"project"`
	Location                    string            `json:"location"`
	ModelCatalogPath            string            `json:"model_catalog_path"`
	ModelCatalogRefreshOnStart  bool              `json:"model_catalog_refresh_on_start"`
	AllowedModels               []string          `json:"allowed_models"`
	AllowAnyGeminiModel         bool              `json:"allow_any_gemini_model"`
	ModelAliases                map[string]string `json:"model_aliases"`
	VertexBaseURL               string            `json:"vertex_base_url"`
	GatewayAPIKeys              []string          `json:"gateway_api_keys"`
	GatewayAllowUnauthenticated bool              `json:"gateway_allow_unauthenticated"`
	LogPath                     string            `json:"log_path"`
	LogMaxBytes                 int64             `json:"log_max_bytes"`
	RequestTimeoutSeconds       int               `json:"request_timeout_seconds"`
	VertexRetryMaxAttempts      int               `json:"vertex_retry_max_attempts"`
	VertexRetryInitialMS        int               `json:"vertex_retry_initial_ms"`
	VertexRetryMaxMS            int               `json:"vertex_retry_max_ms"`
	AdaptiveConcurrencyEnabled  bool              `json:"adaptive_concurrency_enabled"`
	AdaptiveConcurrencyMin      int               `json:"adaptive_concurrency_min"`
	AdaptiveConcurrencyInitial  int               `json:"adaptive_concurrency_initial"`
	AdaptiveConcurrencyMax      int               `json:"adaptive_concurrency_max"`
	AdaptiveQueueMaxDepth       int               `json:"adaptive_queue_max_depth"`
	AdaptiveQueueMaxWaitMS      int               `json:"adaptive_queue_max_wait_ms"`
	AsyncJobRetentionSeconds    int               `json:"async_job_retention_seconds"`
	AsyncJobTimeoutSeconds      int               `json:"async_job_timeout_seconds"`
}

func Load() (Config, error) {
	loadDotEnv(".env")
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

func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(strings.TrimSpace(key), "\ufeff")
		if key == "" {
			continue
		}
		if current, exists := os.LookupEnv(key); exists && current != "" {
			continue
		}
		os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
	}
}

func defaults() Config {
	return Config{
		Location:                   "global",
		ModelCatalogPath:           "config/models.json",
		ModelCatalogRefreshOnStart: true,
		AllowAnyGeminiModel:        false,
		ModelAliases:               map[string]string{},
		VertexBaseURL:              "https://aiplatform.googleapis.com",
		GatewayAPIKeys:             []string{"dev-local-key"},
		LogPath:                    "logs/requests.jsonl",
		LogMaxBytes:                100 * 1024 * 1024,
		RequestTimeoutSeconds:      180,
		VertexRetryMaxAttempts:     3,
		VertexRetryInitialMS:       250,
		VertexRetryMaxMS:           2000,
		AdaptiveConcurrencyEnabled: true,
		AdaptiveConcurrencyMin:     1,
		AdaptiveConcurrencyInitial: 4,
		AdaptiveConcurrencyMax:     32,
		AdaptiveQueueMaxDepth:      2048,
		AdaptiveQueueMaxWaitMS:     30000,
		AsyncJobRetentionSeconds:   3600,
		AsyncJobTimeoutSeconds:     300,
	}
}

func overrideFromEnv(c *Config) {
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		c.Project = v
	}
	if v := os.Getenv("GOOGLE_CLOUD_LOCATION"); v != "" {
		c.Location = v
	}
	if v := os.Getenv("MODEL_CATALOG_PATH"); v != "" {
		c.ModelCatalogPath = v
	}
	if v := os.Getenv("MODEL_CATALOG_REFRESH_ON_START"); v != "" {
		c.ModelCatalogRefreshOnStart = strings.EqualFold(v, "true") || v == "1"
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
	if v, ok := os.LookupEnv("GATEWAY_API_KEYS"); ok {
		c.GatewayAPIKeys = splitCSV(v)
	}
	if v := os.Getenv("GATEWAY_ALLOW_UNAUTHENTICATED"); v != "" {
		c.GatewayAllowUnauthenticated = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("LOG_PATH"); v != "" {
		c.LogPath = v
	}
	if v := os.Getenv("LOG_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.LogMaxBytes = n
		}
	}
	if v := os.Getenv("REQUEST_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.RequestTimeoutSeconds = n
		}
	}
	if v := os.Getenv("VERTEX_RETRY_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.VertexRetryMaxAttempts = n
		}
	}
	if v := os.Getenv("VERTEX_RETRY_INITIAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.VertexRetryInitialMS = n
		}
	}
	if v := os.Getenv("VERTEX_RETRY_MAX_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.VertexRetryMaxMS = n
		}
	}
	if v := os.Getenv("ADAPTIVE_CONCURRENCY_ENABLED"); v != "" {
		c.AdaptiveConcurrencyEnabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("ADAPTIVE_CONCURRENCY_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AdaptiveConcurrencyMin = n
		}
	}
	if v := os.Getenv("ADAPTIVE_CONCURRENCY_INITIAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AdaptiveConcurrencyInitial = n
		}
	}
	if v := os.Getenv("ADAPTIVE_CONCURRENCY_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AdaptiveConcurrencyMax = n
		}
	}
	if v := os.Getenv("ADAPTIVE_QUEUE_MAX_DEPTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.AdaptiveQueueMaxDepth = n
		}
	}
	if v := os.Getenv("ADAPTIVE_QUEUE_MAX_WAIT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AdaptiveQueueMaxWaitMS = n
		}
	}
	if v := os.Getenv("ASYNC_JOB_RETENTION_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AsyncJobRetentionSeconds = n
		}
	}
	if v := os.Getenv("ASYNC_JOB_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AsyncJobTimeoutSeconds = n
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
	if !c.GatewayAllowUnauthenticated && len(c.GatewayAPIKeys) == 0 {
		return errors.New("GATEWAY_API_KEYS must contain at least one key unless GATEWAY_ALLOW_UNAUTHENTICATED=true")
	}
	if c.VertexBaseURL == "" {
		return errors.New("VERTEX_BASE_URL is required")
	}
	if c.LogMaxBytes <= 0 {
		return errors.New("LOG_MAX_BYTES must be positive")
	}
	if c.RequestTimeoutSeconds <= 0 {
		return errors.New("REQUEST_TIMEOUT_SECONDS must be positive")
	}
	if c.VertexRetryMaxAttempts <= 0 {
		return errors.New("VERTEX_RETRY_MAX_ATTEMPTS must be positive")
	}
	if c.VertexRetryInitialMS <= 0 {
		return errors.New("VERTEX_RETRY_INITIAL_MS must be positive")
	}
	if c.VertexRetryMaxMS <= 0 {
		return errors.New("VERTEX_RETRY_MAX_MS must be positive")
	}
	if c.AdaptiveConcurrencyEnabled {
		if c.AdaptiveConcurrencyMin <= 0 {
			return errors.New("ADAPTIVE_CONCURRENCY_MIN must be positive")
		}
		if c.AdaptiveConcurrencyInitial <= 0 {
			return errors.New("ADAPTIVE_CONCURRENCY_INITIAL must be positive")
		}
		if c.AdaptiveConcurrencyMax <= 0 {
			return errors.New("ADAPTIVE_CONCURRENCY_MAX must be positive")
		}
		if c.AdaptiveConcurrencyMin > c.AdaptiveConcurrencyInitial {
			return errors.New("ADAPTIVE_CONCURRENCY_MIN must be less than or equal to ADAPTIVE_CONCURRENCY_INITIAL")
		}
		if c.AdaptiveConcurrencyInitial > c.AdaptiveConcurrencyMax {
			return errors.New("ADAPTIVE_CONCURRENCY_INITIAL must be less than or equal to ADAPTIVE_CONCURRENCY_MAX")
		}
		if c.AdaptiveQueueMaxDepth < 0 {
			return errors.New("ADAPTIVE_QUEUE_MAX_DEPTH must be non-negative")
		}
		if c.AdaptiveQueueMaxWaitMS <= 0 {
			return errors.New("ADAPTIVE_QUEUE_MAX_WAIT_MS must be positive")
		}
	}
	if c.AsyncJobRetentionSeconds <= 0 {
		return errors.New("ASYNC_JOB_RETENTION_SECONDS must be positive")
	}
	if c.AsyncJobTimeoutSeconds <= 0 {
		return errors.New("ASYNC_JOB_TIMEOUT_SECONDS must be positive")
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

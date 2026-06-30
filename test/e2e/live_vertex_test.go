package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/auth"
	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	"github.com/example/go-llm-gateway/internal/server"
)

func liveConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelCatalogPath != "" && !filepath.IsAbs(cfg.ModelCatalogPath) {
		root := repoRoot(t)
		cfg.ModelCatalogPath = filepath.Join(root, cfg.ModelCatalogPath)
	}
	return cfg
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("could not find repo root")
		}
		dir = next
	}
}

func TestLiveVertexPublisherModels(t *testing.T) {
	if os.Getenv("RUN_LIVE_VERTEX_TESTS") != "1" {
		t.Skip("set RUN_LIVE_VERTEX_TESTS=1")
	}
	cfg := liveConfig(t)
	client := gemini.NewClient(cfg, auth.NewDefaultTokenProvider())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := client.ListPublisherModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least one live publisher model")
	}
}

func TestLiveVertexStartupCatalogRefresh(t *testing.T) {
	if os.Getenv("RUN_LIVE_VERTEX_TESTS") != "1" {
		t.Skip("set RUN_LIVE_VERTEX_TESTS=1")
	}
	cfg := liveConfig(t)
	seed, err := os.ReadFile(cfg.ModelCatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ModelCatalogPath = filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(cfg.ModelCatalogPath, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.ModelCatalogRefreshOnStart = true
	if _, err := server.NewFromConfig(cfg); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var lastSource string
	for time.Now().Before(deadline) {
		c, err := catalog.Load(cfg.ModelCatalogPath)
		if err != nil {
			t.Fatal(err)
		}
		lastSource = c.Source
		if c.Source == "vertex-live-refresh" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("catalog was not refreshed by startup goroutine; last source=%q", lastSource)
}

func TestLiveVertexGenerateExplicitModel(t *testing.T) {
	if os.Getenv("RUN_LIVE_VERTEX_TESTS") != "1" {
		t.Skip("set RUN_LIVE_VERTEX_TESTS=1")
	}
	model := os.Getenv("LIVE_VERTEX_MODEL")
	if model == "" {
		t.Skip("set LIVE_VERTEX_MODEL to run live generation; no default model is assumed")
	}
	cfg := liveConfig(t)
	cfg.ModelCatalogRefreshOnStart = false
	cfg.ModelCatalogPath = ""
	cfg.AllowAnyGeminiModel = true
	cfg.AllowedModels = []string{model}
	s, err := server.NewFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	payload := map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with only: ok"}}}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+cfg.GatewayAPIKeys[0])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

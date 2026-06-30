package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/server"
)

func TestLiveVertexOptional(t *testing.T) {
	if os.Getenv("RUN_LIVE_VERTEX_TESTS") != "1" {
		t.Skip("set RUN_LIVE_VERTEX_TESTS=1")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.NewFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	payload := map[string]any{"model": cfg.DefaultModel, "messages": []map[string]string{{"role": "user", "content": "Reply with only: ok"}}}
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

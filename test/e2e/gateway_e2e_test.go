package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
	"github.com/example/go-llm-gateway/internal/server"
)

type staticToken struct{}

func (staticToken) Token(ctx context.Context) (string, error) { return "token", nil }

func TestGatewayAgainstFakeVertex(t *testing.T) {
	fakeVertex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth")
		}
		if r.Header.Get("X-Goog-User-Project") != "test-project" {
			t.Fatalf("missing quota project header")
		}
		var req gemini.GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Contents) != 1 {
			t.Fatalf("expected one content")
		}
		_ = json.NewEncoder(w).Encode(gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "fake vertex response"}}}, FinishReason: "STOP"}}, UsageMetadata: gemini.UsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 3, TotalTokenCount: 13, CachedContentTokenCount: 5}})
	}))
	defer fakeVertex.Close()

	cfg := config.Config{Project: "test-project", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, VertexBaseURL: fakeVertex.URL, GatewayAPIKeys: []string{"test-key"}, LogPath: t.TempDir() + "/requests.jsonl", RequestTimeoutSeconds: 10}
	logger, err := gwlog.New(cfg.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	gc := gemini.NewTestClient(fakeVertex.URL, 10*time.Second, staticToken{}, cfg.Project, cfg.Location)
	srv := httptest.NewServer(server.New(cfg, gc, logger).Routes())
	defer srv.Close()

	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hello"}],"extra_body":{"google":{"cached_content":"cachedContents/abc"}}}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(out["model"]) != "gemini-3.1-pro-preview" {
		t.Fatalf("wrong model: %#v", out["model"])
	}
}

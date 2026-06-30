package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
	"github.com/example/go-llm-gateway/internal/openai"
)

type fakeGemini struct{}

func (f fakeGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "ok"}}}, FinishReason: "STOP"}}, UsageMetadata: gemini.UsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2, TotalTokenCount: 5, CachedContentTokenCount: 1}}, nil
}
func (f fakeGemini) StreamGenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, onChunk func(gemini.GenerateResponse) error) error {
	return onChunk(gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "hello"}}}}}})
}

func testServer(t *testing.T) *Server {
	t.Helper()
	path := t.TempDir() + "/requests.jsonl"
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, RequestTimeoutSeconds: 5}
	return New(cfg, fakeGemini{}, logger)
}

func TestChatCompletion(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestChatCompletionRequiresModel(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestUnauthorized(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAllowUnauthenticated(t *testing.T) {
	s := testServer(t)
	s.cfg.GatewayAllowUnauthenticated = true
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestModelMetadataIncludesSupportedParameters(t *testing.T) {
	dir := t.TempDir()
	catalogPath := dir + "/models.json"
	logPath := dir + "/requests.jsonl"
	modelCatalog := catalog.Catalog{Version: 1, Models: []catalog.Model{{
		ID:        "gemini-test",
		Publisher: "google",
		Enabled:   true,
		Available: true,
		Capabilities: catalog.Capabilities{
			Streaming:            true,
			GenerationParameters: []string{"max_tokens", "stop"},
		},
	}}}
	b, err := json.Marshal(modelCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, b, 0644); err != nil {
		t.Fatal(err)
	}
	logger, err := gwlog.New(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-test"}, ModelCatalogPath: catalogPath, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: logPath, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeGemini{}, logger)

	req := httptest.NewRequest("GET", "/v1/models/gemini-test", nil)
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var info openai.ModelInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.SupportedParameters, ",") != "max_tokens,stop" {
		t.Fatalf("supported parameters %#v", info.SupportedParameters)
	}
	if info.Capabilities == nil || info.Capabilities.Streaming == nil || !*info.Capabilities.Streaming {
		t.Fatalf("streaming capability %#v", info.Capabilities)
	}
}

func TestPersistentLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger, _ := gwlog.New(path)
	logger.Write(gwlog.RequestLog{Timestamp: time.Now(), RequestID: "abc", Model: "gemini-3.1-pro-preview", Status: 200})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "abc") {
		t.Fatalf("log missing entry: %s", string(b))
	}
}

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
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

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/example/go-llm-gateway/internal/auth"
	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
	"github.com/example/go-llm-gateway/internal/openai"
)

type GeminiClient interface {
	GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest) (gemini.GenerateResponse, error)
	StreamGenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, onChunk func(gemini.GenerateResponse) error) error
}

type Server struct {
	cfg      config.Config
	resolver *config.ModelResolver
	gemini   GeminiClient
	logger   *gwlog.JSONLLogger
	apiKeys  map[string]struct{}
}

func New(cfg config.Config, gem GeminiClient, logger *gwlog.JSONLLogger) *Server {
	keys := map[string]struct{}{}
	for _, k := range cfg.GatewayAPIKeys {
		keys[k] = struct{}{}
	}
	return &Server{cfg: cfg, resolver: config.NewModelResolver(cfg), gemini: gem, logger: logger, apiKeys: keys}
}

func NewFromConfig(cfg config.Config) (*Server, error) {
	if cfg.ModelCatalogPath != "" {
		if c, err := catalog.Load(cfg.ModelCatalogPath); err == nil {
			cfg.AllowedModels = c.EnabledAvailableIDs()
		} else if len(cfg.AllowedModels) == 0 {
			return nil, fmt.Errorf("load model catalog: %w", err)
		} else {
			log.Printf("model catalog load warning path=%s err=%v", cfg.ModelCatalogPath, err)
		}
	}
	logger, err := gwlog.New(cfg.LogPath)
	if err != nil {
		return nil, err
	}
	gem := gemini.NewClient(cfg, auth.NewDefaultTokenProvider())
	s := New(cfg, gem, logger)
	if cfg.ModelCatalogRefreshOnStart && cfg.ModelCatalogPath != "" {
		go s.refreshModelCatalog(gem, cfg.ModelCatalogPath)
	}
	return s, nil
}

func (s *Server) refreshModelCatalog(gem *gemini.Client, path string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	live, err := gem.ListPublisherModels(ctx)
	if err != nil {
		log.Printf("model catalog refresh warning: %v", err)
		return
	}
	c, err := catalog.Load(path)
	if err != nil {
		log.Printf("model catalog refresh load warning path=%s err=%v", path, err)
		return
	}
	c.MergeLive(live)
	if err := catalog.Save(path, c); err != nil {
		log.Printf("model catalog refresh save warning path=%s err=%v", path, err)
		return
	}
	s.resolver.SetAllowedModels(c.EnabledAvailableIDs())
	log.Printf("model catalog refreshed path=%s live_models=%d enabled_available=%d", path, len(live), len(c.EnabledAvailableIDs()))
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/models", s.models)
	mux.HandleFunc("/v1/chat/completions", s.chatCompletions)
	return requestID(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}
	models := s.resolver.ListModels()
	data := make([]openai.ModelInfo, 0, len(models))
	for _, m := range models {
		data = append(data, openai.ModelInfo{ID: m, Object: "model", OwnedBy: "google"})
	}
	writeJSON(w, http.StatusOK, openai.ModelListResponse{Object: "list", Data: data})
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := getRequestID(r)
	appID := r.Header.Get("X-App-ID")
	logEntry := gwlog.RequestLog{Timestamp: start.UTC(), RequestID: reqID, AppID: appID}
	defer func() { s.logger.Write(logEntry) }()

	if r.Method != http.MethodPost {
		logEntry.Status = http.StatusMethodNotAllowed
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
		return
	}
	if !s.authorized(r) {
		logEntry.Status = http.StatusUnauthorized
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	logEntry.Model = req.Model
	if err := openai.ValidateRequest(req); err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	model, err := s.resolver.Resolve(req.Model)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_model")
		return
	}
	logEntry.VertexModel = model
	logEntry.Stream = req.Stream

	greq, err := gemini.FromOpenAI(req)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout())
	defer cancel()
	if req.Stream {
		s.stream(ctx, w, model, reqID, greq, &logEntry, start)
		return
	}
	gresp, err := s.gemini.GenerateContent(ctx, model, greq)
	logEntry.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		logEntry.Status = http.StatusBadGateway
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadGateway, err.Error(), "vertex_error")
		return
	}
	logEntry.Status = http.StatusOK
	logEntry.PromptTokens = gresp.UsageMetadata.PromptTokenCount
	logEntry.CompletionTokens = gresp.UsageMetadata.CandidatesTokenCount
	logEntry.TotalTokens = gresp.UsageMetadata.TotalTokenCount
	logEntry.CachedTokens = gresp.UsageMetadata.CachedContentTokenCount
	resp := openai.ChatCompletionResponse{
		ID: "chatcmpl-" + reqID, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []openai.Choice{{Index: 0, Message: openai.ResponseMessage{Role: "assistant", Content: gemini.TextFromResponse(gresp)}, FinishReason: gemini.FinishReason(gresp)}},
		Usage:   openai.Usage{PromptTokens: logEntry.PromptTokens, CompletionTokens: logEntry.CompletionTokens, TotalTokens: logEntry.TotalTokens, CachedTokens: logEntry.CachedTokens},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) stream(ctx context.Context, w http.ResponseWriter, model, reqID string, greq gemini.GenerateRequest, logEntry *gwlog.RequestLog, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		logEntry.Status = http.StatusInternalServerError
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "server_error")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	created := time.Now().Unix()
	logEntry.Status = http.StatusOK
	first := true
	err := s.gemini.StreamGenerateContent(ctx, model, greq, func(chunk gemini.GenerateResponse) error {
		text := gemini.TextFromResponse(chunk)
		logEntry.PromptTokens += chunk.UsageMetadata.PromptTokenCount
		logEntry.CompletionTokens += chunk.UsageMetadata.CandidatesTokenCount
		logEntry.TotalTokens += chunk.UsageMetadata.TotalTokenCount
		logEntry.CachedTokens += chunk.UsageMetadata.CachedContentTokenCount
		if first {
			first = false
			sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{Role: "assistant"}, FinishReason: nil}}})
		}
		if text != "" {
			sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{Content: text}, FinishReason: nil}}})
		}
		flusher.Flush()
		return nil
	})
	logEntry.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		logEntry.Error = err.Error()
		log.Printf("stream error request_id=%s err=%v", reqID, err)
		return
	}
	finish := "stop"
	sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{}, FinishReason: &finish}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func sendSSE(w http.ResponseWriter, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", string(b))
}

func (s *Server) authorized(r *http.Request) bool {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return false
	}
	key := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	_, ok := s.apiKeys[key]
	return ok
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, openai.ErrorResponse{Error: openai.ErrorBody{Message: msg, Type: typ}})
}

type requestIDKey struct{}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = randomID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func getRequestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return v
	}
	return randomID()
}
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type RequestLog struct {
	Timestamp        time.Time `json:"timestamp"`
	RequestID        string    `json:"request_id"`
	AppID            string    `json:"app_id,omitempty"`
	Model            string    `json:"model"`
	VertexModel      string    `json:"vertex_model"`
	Stream           bool      `json:"stream"`
	Status           int       `json:"status"`
	LatencyMS        int64     `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	TotalTokens      int       `json:"total_tokens,omitempty"`
	CachedTokens     int       `json:"cached_tokens,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type JSONLLogger struct {
	path string
	mu   sync.Mutex
}

func New(path string) (*JSONLLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &JSONLLogger{path: path}, nil
}

func (l *JSONLLogger) Write(entry RequestLog) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

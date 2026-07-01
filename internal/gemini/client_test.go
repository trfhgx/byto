package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticToken string

func (s staticToken) Token(ctx context.Context) (string, error) {
	return string(s), nil
}

func TestCreateCachedContentNormalizesModelID(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/p/locations/global/cachedContents" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"projects/p/locations/global/cachedContents/cache-1"}`))
	}))
	defer ts.Close()

	c := NewTestClient(ts.URL, time.Second, staticToken("tok"), "p", "global")
	out, err := c.CreateCachedContent(context.Background(), json.RawMessage(`{"model":"gemini-3.1-pro-preview","contents":[{"role":"user","parts":[{"text":"large context"}]}],"ttl":"3600s"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "cache-1") {
		t.Fatalf("response %s", out)
	}
	want := "projects/p/locations/global/publishers/google/models/gemini-3.1-pro-preview"
	if got["model"] != want {
		t.Fatalf("model %v, want %s", got["model"], want)
	}
}

func TestGenerateContentRetriesTransientStatus(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, `{"error":{"status":"UNAVAILABLE"}}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer ts.Close()

	c := NewTestClient(ts.URL, time.Second, staticToken("tok"), "p", "global")
	c.retry = RetryConfig{MaxAttempts: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	resp, err := c.GenerateContent(context.Background(), "gemini-test", GenerateRequest{Contents: []Content{{Role: "user", Parts: []Part{{Text: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts %d", attempts)
	}
	if TextFromResponse(resp) != "ok" {
		t.Fatalf("response %#v", resp)
	}
}

func TestGenerateContentDoesNotRetryBadRequest(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, `{"error":{"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
	}))
	defer ts.Close()

	c := NewTestClient(ts.URL, time.Second, staticToken("tok"), "p", "global")
	c.retry = RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	_, err := c.GenerateContent(context.Background(), "gemini-test", GenerateRequest{Contents: []Content{{Role: "user", Parts: []Part{{Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts %d", attempts)
	}
}

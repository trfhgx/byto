package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
)

func TestAdaptiveLimiterIncreasesAfterCleanWindow(t *testing.T) {
	limiters := newAdaptiveLimiters(config.Config{
		AdaptiveConcurrencyEnabled: true,
		AdaptiveConcurrencyMin:     1,
		AdaptiveConcurrencyInitial: 2,
		AdaptiveConcurrencyMax:     4,
	})

	p, err := limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Limit != 2 {
		t.Fatalf("initial limit %d, want 2", p.Limit)
	}
	p.release(nil)

	p, err = limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	p.release(nil)

	p, err = limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Limit != 3 {
		t.Fatalf("limit after clean window %d, want 3", p.Limit)
	}
	p.release(nil)
}

func TestAdaptiveLimiterReducesOnResourceExhausted(t *testing.T) {
	limiters := newAdaptiveLimiters(config.Config{
		AdaptiveConcurrencyEnabled: true,
		AdaptiveConcurrencyMin:     1,
		AdaptiveConcurrencyInitial: 4,
		AdaptiveConcurrencyMax:     8,
	})

	p, err := limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	p.release(&gemini.VertexError{Operation: "generateContent", Status: http.StatusTooManyRequests, Body: `{"error":{"status":"RESOURCE_EXHAUSTED"}}`})

	p, err = limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Limit != 2 {
		t.Fatalf("limit after resource exhaustion %d, want 2", p.Limit)
	}
	p.release(nil)
}

func TestAdaptiveLimiterNormalizesMissingNumericConfig(t *testing.T) {
	limiters := newAdaptiveLimiters(config.Config{AdaptiveConcurrencyEnabled: true})
	p, err := limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Limit != 1 {
		t.Fatalf("normalized limit %d, want 1", p.Limit)
	}
	p.release(nil)
}

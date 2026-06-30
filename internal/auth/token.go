package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

type DefaultTokenProvider struct {
	client *http.Client
}

func NewDefaultTokenProvider() *DefaultTokenProvider {
	return &DefaultTokenProvider{client: &http.Client{Timeout: 3 * time.Second}}
}

func (p *DefaultTokenProvider) Token(ctx context.Context) (string, error) {
	if t := strings.TrimSpace(os.Getenv("VERTEX_ACCESS_TOKEN")); t != "" {
		return t, nil
	}
	if t, err := p.metadataToken(ctx); err == nil && t != "" {
		return t, nil
	}
	if t, err := p.gcloudToken(ctx); err == nil && t != "" {
		return t, nil
	}
	return "", errors.New("could not obtain Google access token; set VERTEX_ACCESS_TOKEN, run on Cloud Run, or run gcloud auth application-default login")
}

func (p *DefaultTokenProvider) metadataToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata token status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.AccessToken, nil
}

func (p *DefaultTokenProvider) gcloudToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

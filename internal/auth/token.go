package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

type DefaultTokenProvider struct {
	client       *http.Client
	mu           sync.Mutex
	cachedToken  string
	cachedExpiry time.Time
}

func NewDefaultTokenProvider() *DefaultTokenProvider {
	return &DefaultTokenProvider{client: &http.Client{Timeout: 3 * time.Second}}
}

func (p *DefaultTokenProvider) Token(ctx context.Context) (string, error) {
	if t := strings.TrimSpace(os.Getenv("VERTEX_ACCESS_TOKEN")); t != "" {
		return t, nil
	}
	if t, err := p.serviceAccountToken(ctx); err == nil && t != "" {
		return t, nil
	}
	if t, err := p.metadataToken(ctx); err == nil && t != "" {
		return t, nil
	}
	if t, err := p.gcloudToken(ctx); err == nil && t != "" {
		return t, nil
	}
	return "", errors.New("could not obtain Google access token; set VERTEX_ACCESS_TOKEN, set GOOGLE_APPLICATION_CREDENTIALS, run on Cloud Run/GCE, or run gcloud auth application-default login")
}

func (p *DefaultTokenProvider) serviceAccountToken(ctx context.Context) (string, error) {
	path := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	if path == "" {
		return "", errors.New("GOOGLE_APPLICATION_CREDENTIALS is not set")
	}
	p.mu.Lock()
	if p.cachedToken != "" && time.Until(p.cachedExpiry) > time.Minute {
		token := p.cachedToken
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var key struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(b, &key); err != nil {
		return "", err
	}
	if key.ClientEmail == "" || key.PrivateKey == "" {
		return "", errors.New("service account json is missing client_email or private_key")
	}
	if key.TokenURI == "" {
		key.TokenURI = "https://oauth2.googleapis.com/token"
	}
	assertion, err := serviceAccountJWT(key.ClientEmail, key.PrivateKey, key.TokenURI, time.Now())
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, key.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("service account token status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("service account token response did not include access_token")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	p.mu.Lock()
	p.cachedToken = out.AccessToken
	p.cachedExpiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	p.mu.Unlock()
	return out.AccessToken, nil
}

func serviceAccountJWT(email, privateKeyPEM, tokenURI string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", errors.New("service account private key is not PEM encoded")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("service account private key is not RSA")
	}
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   email,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
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

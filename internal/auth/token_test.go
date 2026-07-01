package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceAccountToken(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "")
	tokenRequests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("grant_type %q", r.Form.Get("grant_type"))
		}
		if !strings.Contains(r.Form.Get("assertion"), ".") {
			t.Fatalf("missing jwt assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"service-token","expires_in":3600}`))
	}))
	defer ts.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}))
	body, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "llm-gateway@test-project.iam.gserviceaccount.com",
		"private_key":  keyPEM,
		"token_uri":    ts.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)

	p := NewDefaultTokenProvider()
	got, err := p.serviceAccountToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "service-token" {
		t.Fatalf("token %q", got)
	}
	got, err = p.serviceAccountToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "service-token" || tokenRequests != 1 {
		t.Fatalf("cached token got=%q tokenRequests=%d", got, tokenRequests)
	}
}

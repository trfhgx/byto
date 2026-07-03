package config

import (
	"os"
	"testing"
)

func TestAllowUnauthenticatedPermitsEmptyKeys(t *testing.T) {
	cfg := defaults()
	cfg.Project = "p"
	cfg.GatewayAPIKeys = nil
	cfg.GatewayAllowUnauthenticated = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedGatewayRequiresKeys(t *testing.T) {
	cfg := defaults()
	cfg.Project = "p"
	cfg.GatewayAPIKeys = nil
	cfg.GatewayAllowUnauthenticated = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadReadsDotEnv(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "")
	t.Setenv("GATEWAY_API_KEYS", "")

	dotenv := "\ufeffGOOGLE_CLOUD_PROJECT=dotenv-project\nGOOGLE_CLOUD_LOCATION=us-central1\nGATEWAY_API_KEYS=dotenv-key\n"
	if err := os.WriteFile(".env", []byte(dotenv), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "dotenv-project" {
		t.Fatalf("Project = %q, want dotenv-project", cfg.Project)
	}
	if cfg.Location != "us-central1" {
		t.Fatalf("Location = %q, want us-central1", cfg.Location)
	}
	if len(cfg.GatewayAPIKeys) != 1 || cfg.GatewayAPIKeys[0] != "dotenv-key" {
		t.Fatalf("GatewayAPIKeys = %#v, want dotenv-key", cfg.GatewayAPIKeys)
	}
}

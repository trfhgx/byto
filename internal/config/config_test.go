package config

import "testing"

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

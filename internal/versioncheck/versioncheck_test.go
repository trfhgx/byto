package versioncheck

import "testing"

func TestVersionPolicyStates(t *testing.T) {
	policy := Policy{
		DeprecatedBelow: "0.3.0",
		ObsoleteBelow:   "0.2.0",
		Deprecated:      []string{"0.3.1"},
		Obsolete:        []string{"0.2.1"},
	}

	tests := []struct {
		name       string
		version    string
		deprecated bool
		obsolete   bool
	}{
		{name: "below obsolete threshold", version: "0.1.9", deprecated: true, obsolete: true},
		{name: "below deprecated threshold", version: "0.2.5", deprecated: true, obsolete: false},
		{name: "exact deprecated", version: "0.3.1", deprecated: true, obsolete: false},
		{name: "exact obsolete", version: "0.2.1", deprecated: true, obsolete: true},
		{name: "current", version: "0.3.2", deprecated: false, obsolete: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeprecated(tt.version, policy); got != tt.deprecated {
				t.Fatalf("isDeprecated(%q) = %v, want %v", tt.version, got, tt.deprecated)
			}
			if got := isObsolete(tt.version, policy); got != tt.obsolete {
				t.Fatalf("isObsolete(%q) = %v, want %v", tt.version, got, tt.obsolete)
			}
		})
	}
}

func TestVersionLess(t *testing.T) {
	tests := []struct {
		left string
		right string
		want bool
	}{
		{left: "0.1.0", right: "0.2.0", want: true},
		{left: "v0.10.0", right: "0.2.0", want: false},
		{left: "1.0.0", right: "1.0.0", want: false},
		{left: "1.0", right: "1.0.1", want: true},
	}

	for _, tt := range tests {
		if got := versionLess(tt.left, tt.right); got != tt.want {
			t.Fatalf("versionLess(%q, %q) = %v, want %v", tt.left, tt.right, got, tt.want)
		}
	}
}

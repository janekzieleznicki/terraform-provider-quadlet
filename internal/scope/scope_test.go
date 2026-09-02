package scope

import (
	"context"
	"testing"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Scope
		wantErr bool
	}{
		{"system", ScopeSystem, false},
		{"user", ScopeUser, false},
		{"System", "", true}, // Must be lowercase
		{"", "", true},       // Empty string is invalid
		{"root", "", true},   // Not a valid scope
		{"admin", "", true},  // Not a valid scope
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		scope Scope
		want  bool
	}{
		{ScopeSystem, true},
		{ScopeUser, true},
		{Scope("invalid"), false},
		{Scope(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			got := tt.scope.Valid()
			if got != tt.want {
				t.Errorf("Scope(%q).Valid() = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestUnitDir(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		scope   Scope
		fake    *transport.Fake
		want    string
		wantErr bool
	}{
		{
			name:  "system scope",
			scope: ScopeSystem,
			fake:  transport.NewFake(),
			want:  "/etc/containers/systemd",
		},
		{
			name:  "user scope",
			scope: ScopeUser,
			fake: func() *transport.Fake {
				f := transport.NewFake()
				f.ConfigDir = "/home/fake/.config"
				return f
			}(),
			want: "/home/fake/.config/containers/systemd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnitDir(ctx, tt.fake, tt.scope)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnitDir error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("UnitDir = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSystemctlArgs(t *testing.T) {
	tests := []struct {
		scope Scope
		want  []string
	}{
		{ScopeUser, []string{"--user"}},
		{ScopeSystem, nil},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			got := SystemctlArgs(tt.scope)
			if !sliceEqual(got, tt.want) {
				t.Errorf("SystemctlArgs(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestQuadletFlags(t *testing.T) {
	tests := []struct {
		scope Scope
		want  []string
	}{
		{ScopeUser, []string{"-user"}},
		{ScopeSystem, nil},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			got := QuadletFlags(tt.scope)
			if !sliceEqual(got, tt.want) {
				t.Errorf("QuadletFlags(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package quadlet

import (
	"sort"
	"testing"
)

func TestTypes(t *testing.T) {
	types := Types()

	// Must have exactly 8 types
	if len(types) != 8 {
		t.Errorf("expected 8 types, got %d", len(types))
	}

	// Must contain artifact
	found := false
	for _, typ := range types {
		if typ == TypeArtifact {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected artifact type to be in the list")
	}

	// Must be sorted
	sorted := make([]string, len(types))
	copy(sorted, types)
	sort.Strings(sorted)
	for i, typ := range types {
		if typ != sorted[i] {
			t.Errorf("Types() is not sorted")
			break
		}
	}

	// Verify all expected types are present
	expected := map[string]bool{
		TypeContainer: true,
		TypeVolume:    true,
		TypeNetwork:   true,
		TypePod:       true,
		TypeKube:      true,
		TypeBuild:     true,
		TypeImage:     true,
		TypeArtifact:  true,
	}

	for _, typ := range types {
		if !expected[typ] {
			t.Errorf("unexpected type %q in Types()", typ)
		}
		delete(expected, typ)
	}

	for typ := range expected {
		t.Errorf("expected type %q not found in Types()", typ)
	}
}

func TestIsValidType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{TypeContainer, true},
		{TypeVolume, true},
		{TypeNetwork, true},
		{TypePod, true},
		{TypeKube, true},
		{TypeBuild, true},
		{TypeImage, true},
		{TypeArtifact, true},
		{"Container", false}, // Wrong case
		{"txt", false},       // Invalid type
		{"", false},          // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidType(tt.input)
			if got != tt.want {
				t.Errorf("IsValidType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFilename(t *testing.T) {
	tests := []struct {
		name     string
		unitType string
		want     string
	}{
		{"web", TypeContainer, "web.container"},
		{"db", TypeVolume, "db.volume"},
		{"my-app", TypePod, "my-app.pod"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := Filename(tt.name, tt.unitType)
			if got != tt.want {
				t.Errorf("Filename(%q, %q) = %q, want %q", tt.name, tt.unitType, got, tt.want)
			}
		})
	}
}

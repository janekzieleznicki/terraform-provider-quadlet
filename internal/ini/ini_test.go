package ini

import (
	"strings"
	"testing"
)

func TestRender_GoldenPath(t *testing.T) {
	u := &Unit{}
	u.Add("Unit", "Description", "demo")
	u.Add("Container", "Image", "quay.io/libpod/alpine:latest")
	u.Add("Container", "PublishPort", "8080:80")
	u.Add("Container", "PublishPort", "8443:443")
	u.Add("Install", "WantedBy", "default.target")

	out, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := `[Unit]
Description=demo

[Container]
Image=quay.io/libpod/alpine:latest
PublishPort=8080:80
PublishPort=8443:443

[Install]
WantedBy=default.target
`

	if out != expected {
		t.Errorf("expected:\n%q\n\ngot:\n%q", expected, out)
	}

	// Verify trailing newline
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end with newline")
	}
	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("output has double trailing newline")
	}
}

func TestRender_EmptySection(t *testing.T) {
	u := &Unit{}
	u.EnsureSection("Volume")

	out, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expected := "[Volume]\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestRender_EmptyUnit(t *testing.T) {
	u := &Unit{}

	out, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if out != "" {
		t.Errorf("expected empty string, got %q", out)
	}
}

func TestRender_RepeatedKey(t *testing.T) {
	u := &Unit{}
	u.Add("Container", "PublishPort", "8080:80")
	u.Add("Container", "PublishPort", "8443:443")

	out, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if !strings.Contains(out, "PublishPort=8080:80") {
		t.Errorf("output missing first PublishPort")
	}
	if !strings.Contains(out, "PublishPort=8443:443") {
		t.Errorf("output missing second PublishPort")
	}
}

func TestRender_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Unit)
		wantErr string
	}{
		{
			name: "empty section name",
			setup: func(u *Unit) {
				u.Sections = append(u.Sections, Section{Name: "", Directives: []Directive{}})
			},
			wantErr: "section name must not be empty",
		},
		{
			name: "section name with bracket",
			setup: func(u *Unit) {
				u.Sections = append(u.Sections, Section{Name: "Bad[Name]", Directives: []Directive{}})
			},
			wantErr: "invalid section name",
		},
		{
			name: "section name with newline",
			setup: func(u *Unit) {
				u.Sections = append(u.Sections, Section{Name: "Bad\nName", Directives: []Directive{}})
			},
			wantErr: "invalid section name",
		},
		{
			name: "empty key",
			setup: func(u *Unit) {
				u.Add("Section", "", "value")
			},
			wantErr: "key must not be empty",
		},
		{
			name: "key with equals",
			setup: func(u *Unit) {
				u.Add("Section", "Key=Bad", "value")
			},
			wantErr: "invalid key",
		},
		{
			name: "key with leading space",
			setup: func(u *Unit) {
				u.Add("Section", " Key", "value")
			},
			wantErr: "invalid key",
		},
		{
			name: "key with trailing space",
			setup: func(u *Unit) {
				u.Add("Section", "Key ", "value")
			},
			wantErr: "invalid key",
		},
		{
			name: "value with newline",
			setup: func(u *Unit) {
				u.Add("Section", "Key", "bad\nvalue")
			},
			wantErr: "value must not contain a newline",
		},
		{
			name: "value with carriage return",
			setup: func(u *Unit) {
				u.Add("Section", "Key", "bad\rvalue")
			},
			wantErr: "value must not contain a newline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &Unit{}
			tt.setup(u)

			_, err := u.Render()
			if err == nil {
				t.Errorf("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestValueWithEquals(t *testing.T) {
	// Values can contain '=' (e.g., Environment=FOO=bar)
	u := &Unit{}
	u.Add("Container", "Environment", "PODMAN_SYSTEMD_UNIT=model-daemon.service")

	out, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if !strings.Contains(out, "Environment=PODMAN_SYSTEMD_UNIT=model-daemon.service") {
		t.Errorf("output should contain value with equals sign")
	}
}

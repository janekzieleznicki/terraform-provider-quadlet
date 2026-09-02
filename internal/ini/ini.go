// Package ini renders systemd/quadlet unit files. It is deliberately
// render-only: nothing in the provider parses unit INI back into structure.
package ini

import (
	"fmt"
	"strings"
)

// Directive is a single Key=Value line. Repeating a key is meaningful in
// systemd and quadlet (PublishPort=, Volume=, Environment=), so directives are
// an ordered slice rather than a map.
type Directive struct {
	Key   string
	Value string
}

// Section is a named group of directives, e.g. [Container].
type Section struct {
	Name       string
	Directives []Directive
}

// Unit is an ordered list of sections.
type Unit struct {
	Sections []Section
}

// EnsureSection returns the named section, appending an empty one if absent.
// An empty section is legal and useful: a file whose entire content is
// "[Volume]\n" generates a working -volume.service.
func (u *Unit) EnsureSection(name string) *Section {
	for i := range u.Sections {
		if u.Sections[i].Name == name {
			return &u.Sections[i]
		}
	}
	// Append new section
	u.Sections = append(u.Sections, Section{Name: name, Directives: []Directive{}})
	return &u.Sections[len(u.Sections)-1]
}

// Add appends Key=Value to the named section, creating the section if absent.
func (u *Unit) Add(section, key, value string) {
	s := u.EnsureSection(section)
	s.Directives = append(s.Directives, Directive{Key: key, Value: value})
}

// Render returns the unit text: sections in insertion order, directives in
// insertion order, a single blank line between sections, and exactly one
// trailing newline.
func (u *Unit) Render() (string, error) {
	// Validate all sections and directives first
	for _, section := range u.Sections {
		// Validate section name
		if section.Name == "" {
			return "", fmt.Errorf("ini: section name must not be empty")
		}
		if strings.ContainsAny(section.Name, "[][\n\r") {
			return "", fmt.Errorf("ini: invalid section name %q", section.Name)
		}

		// Validate directives
		for _, directive := range section.Directives {
			if directive.Key == "" {
				return "", fmt.Errorf("ini: section %q: key must not be empty", section.Name)
			}
			if strings.ContainsAny(directive.Key, "=\n\r") || strings.HasPrefix(directive.Key, " ") || strings.HasSuffix(directive.Key, " ") {
				return "", fmt.Errorf("ini: section %q: invalid key %q", section.Name, directive.Key)
			}
			if strings.ContainsAny(directive.Value, "\n\r") {
				return "", fmt.Errorf("ini: section %q: key %q: value must not contain a newline", section.Name, directive.Key)
			}
		}
	}

	if len(u.Sections) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for i, section := range u.Sections {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[")
		sb.WriteString(section.Name)
		sb.WriteString("]\n")

		for _, directive := range section.Directives {
			sb.WriteString(directive.Key)
			sb.WriteString("=")
			sb.WriteString(directive.Value)
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}

// Package quadlet defines types and functions for validating quadlet unit files.
package quadlet

import (
	"sort"
)

// Types are the eight quadlet unit types, matching upstream GetUnitServiceName.
const (
	TypeContainer = "container"
	TypeVolume    = "volume"
	TypeNetwork   = "network"
	TypePod       = "pod"
	TypeKube      = "kube"
	TypeBuild     = "build"
	TypeImage     = "image"
	TypeArtifact  = "artifact"
)

var allTypes = []string{
	TypeContainer,
	TypeVolume,
	TypeNetwork,
	TypePod,
	TypeKube,
	TypeBuild,
	TypeImage,
	TypeArtifact,
}

// Types returns a sorted slice of all valid quadlet unit types.
func Types() []string {
	result := make([]string, len(allTypes))
	copy(result, allTypes)
	sort.Strings(result)
	return result
}

// IsValidType returns true if the given string is a valid quadlet unit type.
func IsValidType(t string) bool {
	for _, typ := range allTypes {
		if typ == t {
			return true
		}
	}
	return false
}

// Filename returns the filename for a unit with the given name and type.
func Filename(name, unitType string) string {
	return name + "." + unitType
}

package sysutil

import "strings"

// IsPathSegment reports whether name is one cross-platform filesystem segment.
func IsPathSegment(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

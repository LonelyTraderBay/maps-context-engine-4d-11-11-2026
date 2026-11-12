package model

import "fmt"

// ValidateRelativePath validates the portable lexical contract only. Secure
// opens, filesystem containment, device names and case collisions are the OS
// adapter's responsibility. Only a module root may be represented by ".".
func ValidateRelativePath(path string, moduleRoot bool) error {
	if moduleRoot && path == "." {
		return nil
	}
	if !validSourcePath(path) {
		return fmt.Errorf("invalid portable relative path")
	}
	return nil
}

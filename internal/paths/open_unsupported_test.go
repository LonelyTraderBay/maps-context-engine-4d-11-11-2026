//go:build !linux

package paths

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsClosed(t *testing.T) {
	if _, err := OpenRoot("fixture"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unexpected: %v", err)
	}
}

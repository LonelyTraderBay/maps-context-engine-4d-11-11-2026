package golang

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLoaderEnvironmentBindsEffectiveProfile(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	root := filepath.Join(string(filepath.Separator), "owned-scratch")
	goRoot := filepath.Join(string(filepath.Separator), "approved-go")
	if env, flags, err := LoaderEnvironment(input.Profile, goRoot, root, filepath.Join(root, "input")); err != nil || len(env) == 0 || len(flags) == 0 {
		t.Fatalf("valid profile: %v", err)
	}
	input.Profile.CgoEnabled = true
	if env, flags, err := LoaderEnvironment(input.Profile, goRoot, root, filepath.Join(root, "input")); !errors.Is(err, ErrLoaderInput) || env != nil || flags != nil {
		t.Fatalf("unverified cgo accepted: %v", err)
	}
}

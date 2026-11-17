//go:build linux

package compose

import (
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"testing"
)

func TestNativeLoaderInventoryCopyPreservesAuthority(t *testing.T) {
	original := inventory.FileMap{InputDigest: "approved input", Entries: []inventory.FileMapEntry{{Path: "a.go", State: "candidate", Origin: "untracked", Stages: []uint8{}}}}
	copy := original
	copy.Entries = append([]inventory.FileMapEntry(nil), original.Entries...)
	copy.Entries[0].Stages = nil
	if !sameNativeLoaderInventory(original, copy) {
		t.Fatal("equivalent defensive copy was refused")
	}
	for _, mutate := range []func(*inventory.FileMap){
		func(v *inventory.FileMap) { v.InputDigest = "changed" },
		func(v *inventory.FileMap) { v.Entries[0].Stages = []uint8{1} },
		func(v *inventory.FileMap) { v.Entries[0].SkipWorktree = true },
		func(v *inventory.FileMap) { v.Entries[0].AssumeUnchanged = true },
		func(v *inventory.FileMap) { v.IgnoredCount++ },
	} {
		candidate := copy
		candidate.Entries = append([]inventory.FileMapEntry(nil), copy.Entries...)
		mutate(&candidate)
		if sameNativeLoaderInventory(original, candidate) {
			t.Fatal("changed inventory authority was accepted")
		}
	}
}

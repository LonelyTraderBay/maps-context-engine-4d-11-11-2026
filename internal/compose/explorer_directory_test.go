package compose

import (
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestExplorerDirectoryCountsAuthorizedSnapshotEntries(t *testing.T) {
	hash := strings.Repeat("a", 64)
	reason := "PARSE_ERROR"
	files := []model.ManifestFile{
		{Path: "src/top.ts", ContentHash: &hash},
		{Path: "src/ui/broken.ts", ReasonCode: &reason},
		{Path: "src/ui/button.ts", ContentHash: &hash},
		{Path: "src/other.txt"},
		{Path: "src/internal/hidden.ts", ContentHash: &hash},
		{Path: "src-other/unrelated.ts", ContentHash: &hash},
	}
	allowed := func(name string) bool { return policy.CheckConfiguredPath(name, []string{"/src/internal"}) == nil }
	listing := buildExplorerDirectory("src", files, allowed)
	outside := "OUTSIDE_CAPTURE_SCOPE"
	want := model.GraphDirectory{Path: "src", Indexed: 2, Skipped: 2, Entries: []model.GraphDirectoryEntry{
		{Path: "src/ui", Kind: "directory", Indexed: 1, Skipped: 1},
		{Path: "src/other.txt", Kind: "file", Skipped: 1, ReasonCode: &outside},
		{Path: "src/top.ts", Kind: "file", Indexed: 1, ContentHash: &hash},
	}}
	if !reflect.DeepEqual(listing, want) {
		t.Fatalf("directory listing = %+v, want %+v", listing, want)
	}
	child := buildExplorerDirectory("src/ui", files, allowed)
	if len(child.Entries) != 2 || child.Entries[0].ReasonCode == nil || *child.Entries[0].ReasonCode != reason {
		t.Fatalf("skipped reason missing: %+v", child)
	}
}

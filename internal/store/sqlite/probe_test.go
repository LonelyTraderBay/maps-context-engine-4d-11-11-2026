package sqlite

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestProbeMemoryFindsFTSWithoutStateFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	version, fts, err := ProbeMemory(context.Background())
	if err != nil || !fts || !strings.Contains(version, ".") {
		t.Fatalf("SQLite/FTS probe: %q %v %v", version, fts, err)
	}
	if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
		t.Fatal("in-memory probe created files in its temporary directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ProbeMemory(ctx); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}

//go:build linux

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReadsConfinedInputAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "request.json")
	value := `{"action":"validate","config":{"schema_version":1}}`
	if err := os.WriteFile(input, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	args := []string{"config", "validate", "--input-json", input}
	if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &errOut, Dependencies{CPUs: 4}); code != 0 {
		t.Fatalf("regular input exit %d", code)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	args[3] = link
	if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &errOut, Dependencies{CPUs: 4}); code != 2 || out.Len() != 0 || strings.Contains(errOut.String(), link) {
		t.Fatal("symlink input accepted or path leaked")
	}
	if after, err := os.ReadFile(input); err != nil || string(after) != value {
		t.Fatal("input file changed")
	}
}

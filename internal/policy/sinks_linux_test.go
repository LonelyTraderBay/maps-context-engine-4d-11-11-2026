//go:build linux

package policy

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	_ "modernc.org/sqlite"
)

func TestSecretFixtureNeverReachesSyntheticSinks(t *testing.T) {
	dir := t.TempDir()
	marker := "TESTONLY" + strings.Repeat("Z", 16)
	for name, body := range map[string]string{".env": "password=" + marker, "mixed.go": "// api_key=" + marker, ".env.example": "password=" + marker, "clean.go": "package clean\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	scanner, err := NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{`CREATE TABLE blobs(content BLOB)`, `CREATE VIRTUAL TABLE search USING fts5(body)`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var logLines []string
	var outputs [][]byte
	for _, name := range []string{".env", "mixed.go", ".env.example", "clean.go"} {
		body, err := scanner.ReadSource(root, name)
		if err != nil {
			logLines = append(logLines, err.Error())
			raw, e := json.Marshal(map[string]string{"error": err.Error()})
			if e != nil {
				t.Fatal(e)
			}
			outputs = append(outputs, raw)
			continue
		}
		if _, err = db.Exec(`INSERT INTO blobs VALUES(?)`, body); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO search(body) VALUES(?)`, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM blobs WHERE instr(CAST(content AS TEXT),?)>0`, marker).Scan(&count); err != nil || count != 0 {
		t.Fatalf("marker in blob sink: %d %v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM search WHERE search MATCH ?`, marker).Scan(&count); err != nil || count != 0 {
		t.Fatalf("marker in FTS sink: %d %v", count, err)
	}
	for _, line := range logLines {
		if strings.Contains(line, marker) {
			t.Fatal("marker in synthetic log")
		}
	}
	for _, data := range outputs {
		if strings.Contains(string(data), marker) {
			t.Fatal("marker in synthetic output")
		}
	}
	if err = db.QueryRow(`SELECT count(*) FROM blobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("clean source not persisted in fixture: %d %v", count, err)
	}
}

//go:build linux

package sqlite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedRestoreInputPreflightFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	base := filepath.Join(profileStatePath(root), "backups")
	source := filepath.Join(base, "public-catalog")
	good, err := openProtectedRestoreInput(ctx, root, "catalog", "", filepath.Join(source, "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if good.manifest.ProfileInstallationID != good.profile.ProfileID || len(good.manifest.Stores) != 1 || good.manifestSHA256 == "" {
		t.Fatal("unchecked input returned")
	}
	m := good.profile
	if err := good.root.Close(); err != nil {
		t.Fatal(err)
	}
	original := map[string][]byte{}
	for _, name := range []string{"backup.json", "catalog.sqlite"} {
		raw, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		original[name] = raw
	}
	cases := []struct {
		name   string
		change func(string)
	}{
		{"extra-key", func(p string) {
			if err := os.WriteFile(filepath.Join(p, "profile.key"), []byte("fake key"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"sidecar", func(p string) {
			if err := os.WriteFile(filepath.Join(p, "catalog.sqlite-wal"), []byte("fake WAL"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"database-hash", func(p string) {
			raw := append([]byte(nil), original["catalog.sqlite"]...)
			raw[len(raw)-1] ^= 1
			if err := os.WriteFile(filepath.Join(p, "catalog.sqlite"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong-profile", func(p string) {
			var b StoreBackupManifest
			if err := json.Unmarshal(original["backup.json"], &b); err != nil {
				t.Fatal(err)
			}
			b.ProfileInstallationID = "33333333-3333-4333-8333-333333333333"
			raw, _ := json.Marshal(b)
			if err := os.WriteFile(filepath.Join(p, "backup.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"future-schema", func(p string) {
			var b StoreBackupManifest
			if err := json.Unmarshal(original["backup.json"], &b); err != nil {
				t.Fatal(err)
			}
			b.Stores[0].SchemaVersion = 3
			raw, _ := json.Marshal(b)
			if err := os.WriteFile(filepath.Join(p, "backup.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"hardlink", func(p string) {
			if err := os.Link(filepath.Join(p, "catalog.sqlite"), filepath.Join(base, "restore-input-hardlink-alias.sqlite")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(p string) {
			path := filepath.Join(p, "catalog.sqlite")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/target/canary.txt", path); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, item := range cases {
		p := filepath.Join(base, "restore-input-"+item.name)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(p, int(m.EngineUID), int(m.EngineGID)); err != nil {
			t.Fatal(err)
		}
		for name, raw := range original {
			file := filepath.Join(p, name)
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(file, int(m.EngineUID), int(m.EngineGID)); err != nil {
				t.Fatal(err)
			}
		}
		item.change(p)
		input, err := openProtectedRestoreInput(ctx, root, "catalog", "", filepath.Join(p, "backup.json"))
		if err == nil || input != nil {
			t.Fatalf("unvalidated %s accepted: %v", item.name, err)
		}
	}
	for _, path := range []string{filepath.Join(source, "..", "public-catalog", "backup.json") + "/..", "/target/backup.json", filepath.Join(source, "catalog.sqlite")} {
		if input, err := openProtectedRestoreInput(ctx, root, "catalog", "", path); err == nil || input != nil {
			t.Fatal("invalid input path accepted")
		}
	}
	for name, raw := range original {
		after, err := os.ReadFile(filepath.Join(source, name))
		if err != nil || string(after) != string(raw) {
			t.Fatal("approved backup changed")
		}
	}
	if entries, err := os.ReadDir(filepath.Join(root, "control/catalog")); err != nil {
		t.Fatal(err)
	} else {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "maintenance") {
				t.Fatal("input preflight published recovery state")
			}
		}
	}
	fmt.Println("NATIVE_RESTORE_INPUT_ALL_BYTES_VALIDATED_BEFORE_SQL_NO_REPLACEMENT_PASS")
}

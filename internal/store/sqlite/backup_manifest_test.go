package sqlite

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
)

func backupManifestFixture() StoreBackupManifest {
	id := "11111111-1111-4111-8111-111111111111"
	inc := "22222222-2222-4222-8222-222222222222"
	return StoreBackupManifest{FormatVersion: 1, OperationID: id, ProfileInstallationID: inc, Target: "catalog", Consistency: "single_store", CreatedAt: "2026-11-17T00:00:00Z", Revisions: catalog.BackupRevisions{CatalogIncarnation: inc, Worktrees: []catalog.BackupWorktreeRevision{}}, Stores: []StoreBackupFile{{Kind: "catalog", SourceIncarnation: inc, SchemaVersion: 1, MinReaderVersion: 1, FileName: "catalog.sqlite", RawSHA256: strings.Repeat("a", 64), LogicalSHA256: strings.Repeat("b", 64), ByteLength: 4096, SQLiteVersion: "3.53.0"}}}
}

func TestBackupManifestRejectsAmbiguousOrWrongScopeInputsBeforeSQL(t *testing.T) {
	valid := backupManifestFixture()
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoreBackupManifest(raw); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*StoreBackupManifest){
		func(m *StoreBackupManifest) { m.FormatVersion = 2 },
		func(m *StoreBackupManifest) { m.ProfileInstallationID = "" },
		func(m *StoreBackupManifest) { m.Stores[0].FileName = "../../target/source.go" },
		func(m *StoreBackupManifest) { m.Stores[0].SchemaVersion = 3 },
		func(m *StoreBackupManifest) { m.Stores[0].SourceIncarnation = m.OperationID },
		func(m *StoreBackupManifest) { m.Stores[0].ByteLength = 1 << 40 },
		func(m *StoreBackupManifest) { m.Stores[0].RawSHA256 = "not a digest" },
		func(m *StoreBackupManifest) { m.Stores[0].SQLiteVersion = "3.53.0; ignored" },
		func(m *StoreBackupManifest) { m.Target = "worktree" },
	} {
		m := backupManifestFixture()
		change(&m)
		bad, _ := json.Marshal(m)
		if _, err := DecodeStoreBackupManifest(bad); err == nil {
			t.Fatalf("unsafe manifest accepted: %s", bad)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"format_version":1`, `"format_version":1,"format_version":1`, 1),
		strings.Replace(string(raw), `"format_version":1`, `"format_version":1,"run":"unexpected"`, 1),
		string(raw) + ` {}`,
	} {
		if _, err := DecodeStoreBackupManifest([]byte(bad)); err == nil {
			t.Fatal("ambiguous manifest accepted")
		}
	}
}

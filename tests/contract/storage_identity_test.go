package contract

import (
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestStorageReceiptReplayModel(t *testing.T) {
	db := storageDB(t, "records/001.sql")
	h := strings.Repeat("a", 64)
	id := "01234567-89ab-cdef-0123-456789abcdef"
	storageExec(t, db, `INSERT INTO record_versions VALUES('r',1,'decision','approved','{}','[]','actor','reason',?,'now')`, h)
	storageExec(t, db, `INSERT INTO mutation_receipts VALUES(?,'actor',?,'r',1,'now')`, id, h)
	existing := model.MutationIdentity{MutationID: id}
	var version int
	if err := db.QueryRow(`SELECT principal_id,request_hash,result_version FROM mutation_receipts WHERE client_mutation_id=?`, id).Scan(&existing.PrincipalID, &existing.RequestHash, &version); err != nil {
		t.Fatal(err)
	}
	replay, err := model.CheckMutationReplay(&existing, existing)
	if err != nil || !replay || version != 1 {
		t.Fatalf("replay %v %v %d", replay, err, version)
	}
	for _, changed := range []model.MutationIdentity{{MutationID: id, PrincipalID: "other", RequestHash: h}, {MutationID: id, PrincipalID: "actor", RequestHash: strings.Repeat("b", 64)}} {
		if _, err = model.CheckMutationReplay(&existing, changed); !errors.Is(err, model.ErrRecordConflict) {
			t.Fatalf("mismatch accepted: %v", err)
		}
	}
	if storageCount(t, db, `SELECT count(*) FROM record_versions`) != 1 {
		t.Fatal("replay wrote another version")
	}
}
func TestStorageReplacementIdentityInvalidatesPinnedScopeFixture(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	replacement := "11234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	index := storageDB(t, "index/001.sql")
	records := storageDB(t, "records/001.sql")
	storageExec(t, index, `INSERT INTO store_identity VALUES(1,?,?)`, id, id)
	storageExec(t, records, `INSERT INTO records_meta VALUES(1,?,?,1)`, id, id)
	member := model.ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: id, RecordsIncarnation: id}
	old := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: id, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	old.VectorDigest, err = model.ComputeScopeVectorDigest(old, 16384)
	if err != nil {
		t.Fatal(err)
	}
	// Simulates the identity of a newly staged replacement, not an actual restore.
	storageExec(t, index, `UPDATE store_identity SET incarnation=?`, replacement)
	current := old
	current.Members = append([]model.ScopeMember(nil), old.Members...)
	if err = index.QueryRow(`SELECT incarnation FROM store_identity`).Scan(&current.Members[0].IndexIncarnation); err != nil {
		t.Fatal(err)
	}
	if err = records.QueryRow(`SELECT incarnation FROM records_meta`).Scan(&current.Members[0].RecordsIncarnation); err != nil {
		t.Fatal(err)
	}
	current.VectorDigest, err = model.ComputeScopeVectorDigest(current, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err = model.ComparePinnedScope(old, current, 16384); !errors.Is(err, model.ErrScopeChanged) {
		t.Fatalf("old scope accepted: %v", err)
	}
	if current.Members[0].RecordsIncarnation != id || storageCount(t, records, `SELECT record_epoch FROM records_meta`) != 1 {
		t.Fatal("replacement changed durable records")
	}
}

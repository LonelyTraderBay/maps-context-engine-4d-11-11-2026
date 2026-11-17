package sqlite

import "crypto/sha256"

// WALSnapshotPair identifies both files in one recovery snapshot. An
// independent authority must attest the exact pair, store incarnation, last
// committed WAL frame and resulting page count. Hashing whatever files happen
// to exist is not proof that all commits which must be recovered remain.
type WALSnapshotPair struct {
	MainSize       int64
	WALSize        int64
	MainSHA256     [sha256.Size]byte
	WALSHA256      [sha256.Size]byte
	WALCommitFrame uint32
	WALCommitPages uint32
}

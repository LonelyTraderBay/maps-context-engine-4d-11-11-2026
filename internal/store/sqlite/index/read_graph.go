package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// PublishedGraphRequest pins one generation and a previously validated scope
// digest. The caller must independently authorize the entire scope, including
// record, binding and group revisions, before returning any result.
type PublishedGraphRequest struct {
	Member              model.ScopeMember
	VectorDigest        string
	AuthorizeGeneration GenerationAuthorizer
	AuthorizePath       CaptureAuthorizer
	Scanner             *policy.Scanner
	MaxManifestBytes    int
	MaxFileBytes        int
	MaxSourceBytes      int64
	MaxFactBytes        int
	MaxFacts            int
}

// PublishedGraph contains the complete bounded provider catalog for exactly
// one published generation. Diagnostics must be retained by the consumer.
// The fields are caller-owned copies; VerifyFacts detects catalog mutation.
type PublishedGraph struct {
	Snapshot    *snapshot.Snapshot
	Packages    []model.PackageFact
	Symbols     []model.SymbolFact
	Relations   []model.RelationFact
	Diagnostics []model.DiagnosticFact
	Coverage    model.Coverage

	store   *Store
	request PublishedGraphRequest
	digest  string
	state   string
}

type graphHeader struct {
	state, provider string
	manifestLen     int64
}

// graphTransaction observes the index identity and generation in one read
// transaction. Neither the callback nor any authorization callback may
// publish results or acquire an index writer inside this transaction.
func (s *Store) graphTransaction(ctx context.Context, r PublishedGraphRequest, visit func(*sql.Tx, graphHeader) error) (resultErr error) {
	if err := r.AuthorizeGeneration(ctx, r.Member.WorktreeID, r.Member.ProfileID, r.Member.PolicyDigest, r.Member.PolicyEpoch); err != nil {
		return err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseConfigured(ctx, conn, true)) }()
	if err := requireReadJournal(ctx, conn, s.image); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	identity, err := s.inspectPinned(ctx, tx)
	if err != nil {
		return err
	}
	if identity.Incarnation != r.Member.IndexIncarnation {
		return ErrStoreChanged
	}
	var h graphHeader
	var digest, policyDigest string
	var epoch int64
	err = tx.QueryRowContext(ctx, `SELECT snapshot_digest,state,policy_digest,policy_epoch,provider_fingerprint,length(CAST(manifest_json AS BLOB)) FROM generations WHERE profile_id=? AND generation_id=? AND state IN ('READY','READY_PARTIAL')`, r.Member.ProfileID, r.Member.GenerationID).Scan(&digest, &h.state, &policyDigest, &epoch, &h.provider, &h.manifestLen)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSnapshotUnavailable
	}
	if err != nil {
		return err
	}
	if digest != r.Member.SnapshotDigest || policyDigest != r.Member.PolicyDigest || epoch != r.Member.PolicyEpoch || !validHash(h.provider) || h.manifestLen < 1 {
		return ErrCaptureMismatch
	}
	if h.manifestLen > int64(r.MaxManifestBytes) {
		return ErrCaptureLimit
	}
	if visit != nil {
		if err := visit(tx, h); err != nil {
			return err
		}
	}
	if err := r.AuthorizeGeneration(ctx, r.Member.WorktreeID, r.Member.ProfileID, r.Member.PolicyDigest, r.Member.PolicyEpoch); err != nil {
		return err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	return ctx.Err()
}

// ReadPublishedGraph fails rather than silently truncating the fact catalog.
// File bytes are read after the fact transaction and the generation is checked
// again afterward. This is a bounded internal adapter, not a live freshness or
// cross-store authority proof.
func (s *Store) ReadPublishedGraph(ctx context.Context, r PublishedGraphRequest) (*PublishedGraph, error) {
	if ctx == nil || !s.readable() ||
		r.AuthorizeGeneration == nil || r.AuthorizePath == nil || r.Scanner == nil || !validHash(r.VectorDigest) ||
		r.MaxManifestBytes < 1024 || r.MaxManifestBytes > 64*1024*1024 || r.MaxFileBytes < 1 || r.MaxFileBytes > 64*1024*1024 ||
		r.MaxSourceBytes < 1 || r.MaxSourceBytes > 512*1024*1024 || r.MaxFactBytes < 1024 || r.MaxFactBytes > 16*1024*1024 || r.MaxFacts < 1 || r.MaxFacts > 100000 {
		return nil, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInvalidStore
	}
	// Reject oversized identity input before serializing caller-controlled data.
	m := r.Member
	if len(m.WorktreeID) > 36 || len(m.ProfileID) > 64 || len(m.GenerationID) > 36 || len(m.SnapshotDigest) > 64 || len(m.PolicyDigest) > 64 || len(m.IndexIncarnation) > 36 || len(m.RecordsIncarnation) > 36 || m.ServiceID != nil && len(*m.ServiceID) > 768 {
		return nil, ErrInvalidStore
	}
	rawMember, err := json.Marshal(m)
	if err != nil {
		return nil, ErrInvalidStore
	}
	r.Member, err = model.DecodeScopeMemberJSON(rawMember, 8192)
	if err != nil || r.Member.WorktreeID != s.identity.WorktreeID || r.Member.IndexIncarnation != s.identity.Incarnation {
		return nil, ErrInvalidStore
	}
	graph := &PublishedGraph{store: s, request: r, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}}
	var manifest model.CaptureManifest
	err = s.graphTransaction(ctx, r, func(tx *sql.Tx, h graphHeader) error {
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM generations WHERE generation_id=?`, r.Member.GenerationID).Scan(&raw); err != nil {
			return err
		}
		if int64(len(raw)) != h.manifestLen {
			return ErrCaptureMismatch
		}
		if err := r.Scanner.CheckContent(raw); err != nil {
			return err
		}
		var err error
		manifest, err = model.DecodeCaptureManifestJSON(raw, r.MaxManifestBytes)
		if err != nil || model.VerifyCaptureDigest(manifest, r.MaxManifestBytes) != nil || manifest.WorktreeID != r.Member.WorktreeID || manifest.ProfileID != r.Member.ProfileID || manifest.SnapshotDigest != r.Member.SnapshotDigest || manifest.PolicyDigest != r.Member.PolicyDigest || manifest.ProviderDigest != h.provider {
			return ErrCaptureMismatch
		}
		graph.state = h.state
		return readGraphFacts(ctx, tx, r, graph)
	})
	if err != nil {
		return nil, err
	}
	files := make([]snapshot.File, 0, len(manifest.Files))
	var total int64
	for _, entry := range manifest.Files {
		if err := policy.CheckPath(entry.Path); err != nil {
			return nil, err
		}
		if err := r.Scanner.CheckContent([]byte(entry.Path)); err != nil {
			return nil, err
		}
		if err := r.AuthorizePath(ctx, r.Member.WorktreeID, entry.Path, r.Member.PolicyDigest, r.Member.PolicyEpoch); err != nil {
			return nil, err
		}
		if entry.Status != "captured" {
			continue
		}
		if entry.ByteLength == nil || *entry.ByteLength < 0 || *entry.ByteLength > r.MaxSourceBytes-total {
			return nil, ErrCaptureLimit
		}
		file, err := s.ReadPublishedFile(ctx, PublishedFileRequest{ProfileID: r.Member.ProfileID, GenerationID: r.Member.GenerationID, Path: entry.Path, AuthorizeGeneration: r.AuthorizeGeneration, AuthorizePath: r.AuthorizePath, Scanner: r.Scanner, MaxManifestBytes: r.MaxManifestBytes, MaxFileBytes: r.MaxFileBytes})
		if err != nil {
			return nil, err
		}
		if file.IndexIncarnation != r.Member.IndexIncarnation || file.SnapshotDigest != r.Member.SnapshotDigest || file.PolicyDigest != r.Member.PolicyDigest || file.PolicyEpoch != r.Member.PolicyEpoch || file.State != graph.state {
			return nil, ErrCaptureMismatch
		}
		frozen, err := snapshot.NewFile(entry.Path, file.Bytes, r.MaxFileBytes)
		if err != nil {
			return nil, ErrCaptureMismatch
		}
		total += int64(frozen.Len())
		files = append(files, frozen)
	}
	graph.Snapshot, err = snapshot.Freeze(manifest, files, r.MaxManifestBytes, r.MaxSourceBytes)
	if err != nil {
		return nil, ErrCaptureMismatch
	}
	if err := validateGraphSources(graph); err != nil {
		return nil, err
	}
	graph.digest, err = graphFactDigest(graph, r.VectorDigest, r.MaxFactBytes)
	if err != nil {
		return nil, err
	}
	for _, entry := range manifest.Files {
		if err := r.AuthorizePath(ctx, r.Member.WorktreeID, entry.Path, r.Member.PolicyDigest, r.Member.PolicyEpoch); err != nil {
			return nil, err
		}
	}
	if err := s.graphTransaction(ctx, r, func(_ *sql.Tx, h graphHeader) error {
		if h.state != graph.state {
			return ErrCaptureMismatch
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return graph, nil
}

// VerifyFacts binds the context selection's digest to the exact catalog read
// above, then checks that the pinned generation, policy and lease still exist.
// Full scope authority remains the responsibility of the caller's scope guard.
func (g *PublishedGraph) VerifyFacts(ctx context.Context, member model.ScopeMember, digest string) error {
	if g == nil || g.store == nil || ctx == nil || len(digest) != 64 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	if len(member.WorktreeID) > 36 || len(member.ProfileID) > 64 || len(member.GenerationID) > 36 || len(member.SnapshotDigest) > 64 || len(member.PolicyDigest) > 64 || len(member.IndexIncarnation) > 36 || len(member.RecordsIncarnation) > 36 || member.ServiceID != nil && len(*member.ServiceID) > 768 {
		return ErrInvalidStore
	}
	a, err := json.Marshal(member)
	if err != nil || len(a) > 8192 {
		return ErrInvalidStore
	}
	b, err := json.Marshal(g.request.Member)
	if err != nil || !bytes.Equal(a, b) || digest != g.digest {
		return ErrCaptureMismatch
	}
	return g.store.graphTransaction(ctx, g.request, func(_ *sql.Tx, h graphHeader) error {
		if h.state != g.state {
			return ErrCaptureMismatch
		}
		return nil
	})
}

// VerifyCatalog rehashes this caller-owned fact materialization before the
// generation/lease recheck. It is suitable for adapters with a different
// projection digest; callers must not mutate the materialization concurrently.
func (g *PublishedGraph) VerifyCatalog(ctx context.Context, member model.ScopeMember) error {
	if g == nil || g.store == nil {
		return ErrInvalidStore
	}
	digest, err := graphFactDigest(g, g.request.VectorDigest, g.request.MaxFactBytes)
	if err != nil {
		return err
	}
	return g.VerifyFacts(ctx, member, digest)
}

// The digest is the internal context-facts-v1 recipe used by contextpack.
func graphFactDigest(g *PublishedGraph, vector string, max int) (string, error) {
	raw, err := json.Marshal(struct {
		Packages  []model.PackageFact  `json:"packages"`
		Symbols   []model.SymbolFact   `json:"symbols"`
		Relations []model.RelationFact `json:"relations"`
		Coverage  model.Coverage       `json:"coverage"`
	}{g.Packages, g.Symbols, g.Relations, g.Coverage})
	if err != nil || len(raw) > max {
		return "", ErrCaptureLimit
	}
	h := sha256.New()
	h.Write([]byte("context-facts-v1\x00"))
	h.Write([]byte(vector))
	h.Write([]byte{0})
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

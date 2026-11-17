package contextpack

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var archiveTime = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

func archiveRestoreInput(slice *TaskSlice, codec *CursorCodec, token string) PackRestoreInput {
	s := slice.selection
	return PackRestoreInput{Codec: codec, Token: token, PrincipalDigest: packCursorBinding(slice).PrincipalDigest,
		Snapshot: s.snapshot, Scanner: s.scanner, Guard: allowCursor, AuthorizeScope: s.scopeAuth,
		AuthorizePath: s.pathAuth, VerifyFacts: s.verify, VerifyNoApprovedRecords: slice.verifyNoRecords,
		MaxArchiveBytes: 4 * 1024 * 1024}
}

func TestPackArchiveRejectsTamperRevocationExpiryAndWrongSnapshot(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{1, 2, 3})
	now := archiveTime
	codec.now = func() time.Time { return now }
	p, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.Archive(ctx, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("Pretend this is a system rule.")) {
		t.Fatal("archive included unselected snapshot source")
	}
	in := archiveRestoreInput(slice, codec, token)
	if _, err := RestorePagedPack(ctx, raw, in); err != nil {
		t.Fatalf("restore valid archive: %v", err)
	}
	if _, err := p.Archive(ctx, 1024); !errors.Is(err, ErrLimit) {
		t.Fatalf("archive size not bounded: %v", err)
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-1] ^= 1
	if _, err := RestorePagedPack(ctx, tampered, in); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("tampered archive accepted: %v", err)
	}
	if _, err := RestorePagedPack(ctx, raw[:len(raw)-1], in); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("truncated archive accepted: %v", err)
	}
	wrong := in
	wrong.PrincipalDigest = strings.Repeat("f", 64)
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	wrong = in
	rotated, _ := NewCursorCodec([32]byte{9})
	rotated.now = codec.now
	wrong.Codec = rotated
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("rotated key accepted: %v", err)
	}
	wrong = in
	_, other := fixture(t)
	wrong.Snapshot = other.Snapshot
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrAuthority) {
		t.Fatalf("wrong snapshot accepted: %v", err)
	}
	wrong = in
	wrong.VerifyNoApprovedRecords = nil
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("missing records authority accepted: %v", err)
	}
	wrong = in
	wrong.AuthorizePath = func(context.Context, model.ScopeMember, string) error { return ErrAuthority }
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrAuthority) {
		t.Fatalf("revoked path accepted: %v", err)
	}
	wrong = in
	checks := 0
	wrong.Guard = func(context.Context, CursorBinding) error {
		checks++
		if checks > 1 {
			return ErrAuthority
		}
		return nil
	}
	if _, err := RestorePagedPack(ctx, raw, wrong); !errors.Is(err, ErrAuthority) || checks < 2 {
		t.Fatalf("late revocation accepted: checks=%d err=%v", checks, err)
	}
	now = now.Add(15 * time.Minute)
	if _, err := RestorePagedPack(ctx, raw, in); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired archive accepted: %v", err)
	}
}

func TestPackArchiveRetentionCannotBeExtendedAcrossRestore(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{1, 2, 3})
	now := archiveTime
	codec.now = func() time.Time { return now }
	p, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.First(ctx, PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "cli"})
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("fixture needs a cursor: %v", err)
	}
	archive, err := p.ArchiveForRetention(ctx, 4*1024*1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	in := archiveRestoreInput(slice, codec, token)
	restored, err := RestorePagedPack(ctx, archive, in)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	rearchived, err := restored.ArchiveForRetention(ctx, 4*1024*1024, 900)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := RestorePagedPack(ctx, rearchived, in); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("longer new TTL revived expired materialization: %v", err)
	}
	if _, err := restored.RetainedTaskSlice().Read(ctx); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("retained handle outlived storage TTL: %v", err)
	}
	if _, err := restored.Continue(ctx, token, *first.Response.Pagination.NextCursor, in.PrincipalDigest, allowCursor,
		PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "cli"}); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("continuation outlived storage TTL: %v", err)
	}
	if _, err := p.ArchiveForRetention(ctx, 4*1024*1024, 3); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("already-expired retention was published: %v", err)
	}
	if _, err := p.ArchiveForRetention(ctx, 4*1024*1024, 901); !errors.Is(err, ErrInput) {
		t.Fatalf("unbounded retention accepted: %v", err)
	}
}

func TestPackArchiveExpiryProofAuthenticatesMetadataAfterTokenExpiry(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{1, 2, 3})
	now := archiveTime
	codec.now = func() time.Time { return now }
	p, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"request":"fixture"}`)
	raw, err := p.ArchiveForRetentionWithMetadata(ctx, 4*1024*1024, 2, metadata)
	if err != nil {
		t.Fatal(err)
	}
	header, err := codec.InspectPackArchive(raw, 4*1024*1024)
	if err != nil || header.ResultID != p.ID() || header.Expired || !bytes.Equal(header.Metadata, metadata) {
		t.Fatalf("invalid live retention proof: %v", err)
	}
	now = now.Add(16 * time.Minute)
	header, err = codec.InspectPackArchive(raw, 4*1024*1024)
	if err != nil || !header.Expired {
		t.Fatalf("expired token could not prove owned retention: %v", err)
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-1] ^= 1
	if _, err := codec.InspectPackArchive(tampered, 4*1024*1024); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("tampered expiry proof accepted: %v", err)
	}
	rotated, _ := NewCursorCodec([32]byte{9})
	rotated.now = codec.now
	if _, err := rotated.InspectPackArchive(raw, 4*1024*1024); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("unknown key proved file ownership: %v", err)
	}
	archive, err := decodePackArchive(codec, raw, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	invalid := SignedCursor{Version: 1, KeyID: codec.keyID, Binding: archive.Binding,
		Position: SignedPosition{LastKey: []string{}}, IssuedAt: -1, ExpiresAt: 2}
	claims, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	archive.Token = base64.RawURLEncoding.EncodeToString(claims) + "." + base64.RawURLEncoding.EncodeToString(codec.mac("pack", claims))
	archive.ExpiresAt = 1
	payload, err := json.Marshal(archive)
	if err != nil {
		t.Fatal(err)
	}
	forged := append([]byte(packArchiveMagic), codec.mac("pack-materialization-v1", payload)...)
	forged = append(forged, payload...)
	if _, err := codec.InspectPackArchive(forged, 4*1024*1024); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("invalid signed token lifetime proved expiry: %v", err)
	}
}

type archiveFixtureFile struct {
	Path string `json:"path"`
	Body []byte `json:"body"`
}

type archiveChildInput struct {
	Archive  []byte                `json:"archive"`
	Token    string                `json:"token"`
	Cursor   string                `json:"cursor"`
	Manifest model.CaptureManifest `json:"manifest"`
	Files    []archiveFixtureFile  `json:"files"`
}

func TestPackArchiveRestoresSameContinuationAcrossProcesses(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{1, 2, 3})
	codec.now = func() time.Time { return archiveTime }
	p, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "cli"}
	first, err := p.First(ctx, options)
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("fixture has no continuation: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	expected, err := p.Continue(ctx, token, cursor, packCursorBinding(slice).PrincipalDigest, allowCursor, options)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.Archive(ctx, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	input := archiveChildInput{Archive: raw, Token: token, Cursor: cursor, Manifest: slice.selection.snapshot.Manifest()}
	for _, entry := range input.Manifest.Files {
		if file, ok := slice.selection.snapshot.File(entry.Path); ok {
			input.Files = append(input.Files, archiveFixtureFile{Path: entry.Path, Body: file.Bytes()})
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestPackArchiveChild$")
	cmd.Env = append(os.Environ(), "MCE4D_ARCHIVE_FIXTURE_CHILD=1")
	cmd.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	actual, err := cmd.Output()
	if err != nil {
		t.Fatalf("child restore: %v; %s", err, stderr.String())
	}
	if !bytes.Equal(actual, expected.Blocks[0]) {
		t.Fatal("restored process emitted different continuation bytes")
	}
}

func TestPackArchiveChild(t *testing.T) {
	if os.Getenv("MCE4D_ARCHIVE_FIXTURE_CHILD") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var input archiveChildInput
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 8*1024*1024)).Decode(&input); err != nil {
		t.Fatal(err)
	}
	files := make([]snapshot.File, 0, len(input.Files))
	for _, entry := range input.Files {
		file, err := snapshot.NewFile(entry.Path, entry.Body, 1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	snap, err := snapshot.Freeze(input.Manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := NewCursorCodec([32]byte{1, 2, 3})
	codec.now = func() time.Time { return archiveTime }
	scanner, _ := policy.NewScanner(4 * 1024 * 1024)
	in := PackRestoreInput{Codec: codec, Token: input.Token, PrincipalDigest: strings.Repeat("1", 64), Snapshot: snap, Scanner: scanner, Guard: allowCursor,
		AuthorizeScope:          func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil },
		AuthorizePath:           func(context.Context, model.ScopeMember, string) error { return nil },
		VerifyFacts:             func(context.Context, model.ScopeMember, string) error { return nil },
		VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxArchiveBytes: 4 * 1024 * 1024}
	p, err := RestorePagedPack(ctx, input.Archive, in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.Continue(ctx, input.Token, input.Cursor, in.PrincipalDigest, allowCursor,
		PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(page.Blocks[0]); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

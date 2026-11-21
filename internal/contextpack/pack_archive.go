package contextpack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const packArchiveMagic = "MCE4D-PACK-ARCHIVE-1\x00"
const packArchiveMax = 32 * 1024 * 1024

// packArchive contains the selected materialization, never the full snapshot
// or an executable authority callback. It is private to this package.
type packArchive struct {
	Version         int               `json:"version"`
	Token           string            `json:"token"`
	Binding         CursorBinding     `json:"binding"`
	Report          SliceReport       `json:"report"`
	Selection       Report            `json:"selection"`
	Member          model.ScopeMember `json:"member"`
	SelectionPaths  []string          `json:"selection_paths"`
	RulePaths       []string          `json:"rule_paths"`
	Items           []TaskEvidence    `json:"items"`
	Coverage        model.Coverage    `json:"coverage"`
	SliceLimit      int               `json:"slice_limit"`
	SelectionLimit  int               `json:"selection_limit"`
	PolicyMaxOutput int64             `json:"policy_max_output"`
	CheckRecords    bool              `json:"check_records"`
	ExpiresAt       int64             `json:"expires_at"`
	Metadata        []byte            `json:"metadata"`
}

// Archive serializes the already issued pack and authenticates its exact bytes
// with a domain-separated cursor-key MAC. The returned bytes contain selected
// source and require owner-only storage, quota, lifecycle and authorization;
// this method does not provide a filesystem store or authorize later use.
func (p *PagedPack) Archive(ctx context.Context, maxBytes int) ([]byte, error) {
	return p.ArchiveForRetention(ctx, maxBytes, 900)
}

// ArchiveForRetention also binds the original configured retention ceiling.
// Re-archiving a restored pack can shorten that ceiling, never extend it.
func (p *PagedPack) ArchiveForRetention(ctx context.Context, maxBytes int, ttlSeconds int64) ([]byte, error) {
	return p.ArchiveForRetentionWithMetadata(ctx, maxBytes, ttlSeconds, nil)
}

// ArchiveForRetentionWithMetadata binds bounded caller-validated metadata to
// the same MAC. Metadata is sensitive data and is never authorization itself.
func (p *PagedPack) ArchiveForRetentionWithMetadata(ctx context.Context, maxBytes int, ttlSeconds int64, metadata []byte) ([]byte, error) {
	if p == nil || p.slice == nil || p.slice.selection == nil || p.codec == nil || maxBytes < 1024 || maxBytes > packArchiveMax {
		return nil, ErrInput
	}
	if ttlSeconds < 1 || ttlSeconds > 900 || len(metadata) > 65536 {
		return nil, ErrInput
	}
	metadata = bytes.Clone(metadata)
	claims, err := p.codec.Verify(ctx, p.token, p.binding, p.authorize)
	if err != nil {
		return nil, err
	}
	expires := claims.IssuedAt + ttlSeconds
	if claims.ExpiresAt < expires {
		expires = claims.ExpiresAt
	}
	if p.expiresAt > 0 && p.expiresAt < expires {
		expires = p.expiresAt
	}
	if p.codec.now().Unix() >= expires {
		return nil, ErrPackExpired
	}
	s := p.slice
	a := packArchive{Version: 1, Token: p.token, Binding: claims.Binding, Report: p.report,
		Selection: s.selection.report, Member: s.selection.member, SelectionPaths: s.selection.paths,
		RulePaths: s.rulePaths, Items: p.items, Coverage: p.coverage, SliceLimit: s.maxBytes,
		SelectionLimit: s.selection.maxOutput, PolicyMaxOutput: s.policyMaxOutput, CheckRecords: s.verifyNoRecords != nil, ExpiresAt: expires, Metadata: metadata}
	raw, err := json.Marshal(a)
	if err != nil || len(raw) > maxBytes-len(packArchiveMagic)-32 {
		return nil, ErrLimit
	}
	result := make([]byte, 0, len(packArchiveMagic)+32+len(raw))
	result = append(result, packArchiveMagic...)
	result = append(result, p.codec.mac("pack-materialization-v1", raw)...)
	result = append(result, raw...)
	if err := p.codec.Recheck(ctx, claims, p.authorize); err != nil {
		return nil, err
	}
	if p.codec.now().Unix() >= expires {
		return nil, ErrPackExpired
	}
	return result, nil
}

// PackRestoreInput is supplied by a trusted process owner after reopening the
// exact pinned generation. The snapshot and callbacks are independent of the
// archive and must recheck current principal, policy, paths, facts and epochs.
// A callback that simply trusts the archive does not establish authorization.
type PackRestoreInput struct {
	Codec                   *CursorCodec
	Token                   string
	PrincipalDigest         string
	Snapshot                *snapshot.Snapshot
	Scanner                 *policy.Scanner
	Guard                   CursorGuard
	AuthorizeScope          func(context.Context, model.ScopeVector, model.ScopeMember) error
	AuthorizePath           func(context.Context, model.ScopeMember, string) error
	VerifyFacts             func(context.Context, model.ScopeMember, string) error
	VerifyNoApprovedRecords func(context.Context, model.ScopeVector) (bool, error)
	MaxArchiveBytes         int
	ExpectedMetadata        []byte
}

// ArchiveRetention is authenticated with the cursor key, including Metadata.
// Metadata may contain sensitive request data and must not be logged.
type ArchiveRetention struct {
	ResultID  string
	ExpiresAt int64
	Expired   bool
	Metadata  []byte
}

func decodePackArchive(codec *CursorCodec, raw []byte, maxBytes int) (packArchive, error) {
	if codec == nil || codec.now == nil || maxBytes < 1024 || maxBytes > packArchiveMax || len(raw) > maxBytes || len(raw) <= len(packArchiveMagic)+32 {
		return packArchive{}, ErrInput
	}
	raw = bytes.Clone(raw)
	if string(raw[:len(packArchiveMagic)]) != packArchiveMagic {
		return packArchive{}, ErrCursorInvalid
	}
	signature := raw[len(packArchiveMagic) : len(packArchiveMagic)+32]
	payload := raw[len(packArchiveMagic)+32:]
	if !hmac.Equal(signature, codec.mac("pack-materialization-v1", payload)) {
		return packArchive{}, ErrCursorInvalid
	}
	var a packArchive
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&a); err != nil {
		return packArchive{}, ErrCursorInvalid
	}
	canonical, err := json.Marshal(a)
	if err != nil || !bytes.Equal(payload, canonical) || a.Version != 1 || len(a.Metadata) > 65536 {
		return packArchive{}, ErrCursorInvalid
	}
	return a, nil
}

// InspectPackArchive verifies the archive and its original token even after
// expiry. It is for owned-file expiry proof, not a grant to read or emit data.
// A rotated or unknown key fails closed and cannot prove a file is removable.
func (c *CursorCodec) InspectPackArchive(raw []byte, maxBytes int) (ArchiveRetention, error) {
	a, err := decodePackArchive(c, raw, maxBytes)
	if err != nil {
		return ArchiveRetention{}, err
	}
	claims, err := c.authenticate(a.Token, "pack")
	if err != nil || !reflect.DeepEqual(a.Binding, claims.Binding) || a.Binding.ResultID == "" || claims.IssuedAt < 0 || claims.IssuedAt > cursorExactMax-900 || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt > claims.IssuedAt+900 || a.ExpiresAt <= claims.IssuedAt || a.ExpiresAt > claims.ExpiresAt {
		return ArchiveRetention{}, ErrCursorInvalid
	}
	return ArchiveRetention{ResultID: a.Binding.ResultID, ExpiresAt: a.ExpiresAt,
		Expired: c.now().Unix() >= a.ExpiresAt, Metadata: bytes.Clone(a.Metadata)}, nil
}

// RetainedTaskSlice returns the same authority-checked slice used by this
// materialization. Its fields remain private; Read rechecks its authority.
func (p *PagedPack) RetainedTaskSlice() *TaskSlice {
	if p == nil {
		return nil
	}
	return p.slice
}

// RestorePagedPack reconstitutes the same selected items and cursor anchor. It
// never traverses a graph, resolves an active generation or creates a new pack.
// Missing, modified or expired archives must be treated as unavailable; the
// caller must never replace one with a freshly computed result under this token.
func RestorePagedPack(ctx context.Context, raw []byte, in PackRestoreInput) (*PagedPack, error) {
	if ctx == nil || in.Codec == nil || in.Scanner == nil || in.Snapshot == nil || in.Guard == nil || in.AuthorizeScope == nil || in.AuthorizePath == nil || in.VerifyFacts == nil || in.MaxArchiveBytes < 1024 || in.MaxArchiveBytes > packArchiveMax || len(raw) > in.MaxArchiveBytes || len(raw) <= len(packArchiveMagic)+32 {
		return nil, ErrInput
	}
	claims, err := in.Codec.VerifyPrincipal(ctx, in.Token, "pack", in.PrincipalDigest, in.Guard)
	if err != nil {
		return nil, err
	}
	a, err := decodePackArchive(in.Codec, raw, in.MaxArchiveBytes)
	if err != nil {
		return nil, err
	}
	if in.ExpectedMetadata != nil && !bytes.Equal(a.Metadata, in.ExpectedMetadata) {
		return nil, ErrCursorInvalid
	}
	if a.Token != in.Token || !reflect.DeepEqual(a.Binding, claims.Binding) || a.Binding.ResultID == "" || a.SliceLimit < 1024 || a.SliceLimit > 16*1024*1024 || a.SelectionLimit < 1024 || a.SelectionLimit > 16*1024*1024 || a.PolicyMaxOutput < 4096 || a.PolicyMaxOutput > 131072 || a.CheckRecords && in.VerifyNoApprovedRecords == nil || a.ExpiresAt <= claims.IssuedAt || a.ExpiresAt > claims.ExpiresAt {
		return nil, ErrCursorInvalid
	}
	if in.Codec.now().Unix() >= a.ExpiresAt {
		return nil, ErrPackExpired
	}
	if !a.Report.EnumerationComplete || !a.Selection.EnumerationComplete || a.Report.Target != a.Selection.Target || model.ComparePinnedScope(a.Binding.Scope, a.Report.Scope, cursorLimit) != nil || model.ComparePinnedScope(a.Binding.Scope, a.Selection.Scope, cursorLimit) != nil {
		return nil, ErrCursorInvalid
	}
	member, err := model.MatchPinnedMember(a.Binding.Scope, &a.Report.Target, a.Member.ServiceID)
	if err != nil || !reflect.DeepEqual(member, a.Member) {
		return nil, ErrAuthority
	}
	manifest := in.Snapshot.Manifest()
	if manifest.WorktreeID != a.Member.WorktreeID || manifest.ProfileID != a.Member.ProfileID || manifest.PolicyDigest != a.Member.PolicyDigest || manifest.SnapshotDigest != a.Member.SnapshotDigest {
		return nil, ErrAuthority
	}
	selection := &Selection{report: a.Selection, member: a.Member, snapshot: in.Snapshot, scanner: in.Scanner,
		scopeAuth: in.AuthorizeScope, pathAuth: in.AuthorizePath, verify: in.VerifyFacts,
		paths: a.SelectionPaths, maxOutput: a.SelectionLimit}
	slice := &TaskSlice{selection: selection, report: a.Report, rulePaths: a.RulePaths,
		maxBytes: a.SliceLimit, policyMaxOutput: a.PolicyMaxOutput}
	expiresAt, codec := a.ExpiresAt, in.Codec
	slice.retentionCheck = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if codec.now().Unix() >= expiresAt {
			return ErrPackExpired
		}
		return nil
	}
	if a.CheckRecords {
		slice.verifyNoRecords = in.VerifyNoApprovedRecords
	}
	p := &PagedPack{slice: slice, report: a.Report, coverage: a.Coverage, items: a.Items,
		codec: in.Codec, binding: a.Binding, guard: in.Guard, token: a.Token, expiresAt: a.ExpiresAt}
	if err := p.codec.Recheck(ctx, claims, p.authorize); err != nil {
		return nil, err
	}
	return p, nil
}

// Package lsp owns bounded, read-only preview result primitives. It does not
// execute a target or apply edits.
package lsp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var (
	ErrResultInvalid     = errors.New("rename result invalid")
	ErrResultExpired     = errors.New("rename result expired or missing")
	ErrResultTooLarge    = errors.New("rename result exceeds limit")
	ErrResultQuota       = errors.New("rename result storage quota exceeded")
	ErrResultStoreUnsafe = errors.New("rename result storage unsafe")
	ErrResultUncertain   = errors.New("rename result installation outcome uncertain")
	ErrResultUnsupported = errors.New("secure rename result storage unsupported")
)

const resultHardBytes int64 = 32 * 1024 * 1024
const resultHardQuota int64 = 128 * 1024 * 1024
const resultTTL = 15 * time.Minute

type RenameResultPayload struct {
	InitialRequest    model.RenameInitialRequest     `json:"initial_request"`
	ProviderDigest    string                         `json:"provider_digest"`
	FilePreconditions []model.RenameFilePrecondition `json:"file_preconditions"`
	Edits             []model.ProposedEdit           `json:"edits"`
	Conflicts         []model.Gap                    `json:"conflicts"`
}

type RenameResultHeader struct {
	FormatVersion   int               `json:"format_version"`
	EngineSchema    int               `json:"engine_schema"`
	WorktreeID      string            `json:"worktree_id"`
	ResultID        string            `json:"result_id"`
	PrincipalDigest string            `json:"principal_digest"`
	Scope           model.ScopeVector `json:"scope"`
	ResultDigest    string            `json:"result_digest"`
	PayloadHash     string            `json:"payload_hash"`
	IssuedAt        int64             `json:"issued_at"`
	ExpiresAt       int64             `json:"expires_at"`
}

type RenameStoredResult struct {
	Header  RenameResultHeader  `json:"header"`
	Payload RenameResultPayload `json:"payload"`
}

// ResultGuard must authorize the principal and full scope against trusted
// stores and, before creation, verify every edit and precondition against the
// captured sources. It is called again before returning any result.
type ResultGuard func(context.Context, RenameStoredResult) error

type ResultStore struct {
	root      *paths.Root
	lease     *runtime.Lifecycle
	worktree  string
	scanner   *policy.Scanner
	maxResult int64
	quota     int64
	ttl       time.Duration
	maxCount  int
	authorize func(context.Context) error
	reserve   packstore.ReserveBudget
	now       func() time.Time
}

// NewResultStore receives a previously authorized worktree state root and the
// matching shared lifecycle lease. It never creates or removes directories.
func NewResultStore(root *paths.Root, lease *runtime.Lifecycle, worktreeID string, scanner *policy.Scanner, maxResult, quota int64) (*ResultStore, error) {
	if root == nil || lease == nil || scanner == nil || !resultUUID(worktreeID) {
		return nil, ErrResultInvalid
	}
	if maxResult == 0 {
		maxResult = resultHardBytes
	}
	if quota == 0 {
		quota = resultHardQuota
	}
	if maxResult < 1 || maxResult > resultHardBytes || quota < 1 || quota > resultHardQuota {
		return nil, ErrResultInvalid
	}
	return &ResultStore{root: root, lease: lease, worktree: worktreeID, scanner: scanner, maxResult: maxResult, quota: quota, ttl: resultTTL, maxCount: 10000, now: time.Now}, nil
}

// NewBudgetedResultStore additionally requires the trusted owner to maintain
// root containment and a reservation against the total worktree cache. The
// reservation is held through publication; closing it must preserve charges
// for installed or pending files. No directories are created here.
func NewBudgetedResultStore(root *paths.Root, lease *runtime.Lifecycle, worktreeID string, scanner *policy.Scanner, maxResult, quota, ttlSeconds int64, maxCount int, authorize func(context.Context) error, reserve packstore.ReserveBudget) (*ResultStore, error) {
	if ttlSeconds < 1 || ttlSeconds > 900 || maxCount < 1 || maxCount > 32 || maxResult < 1024 || quota < maxResult || authorize == nil || reserve == nil {
		return nil, ErrResultInvalid
	}
	s, err := NewResultStore(root, lease, worktreeID, scanner, maxResult, quota)
	if err != nil {
		return nil, err
	}
	s.ttl, s.maxCount, s.authorize, s.reserve = time.Duration(ttlSeconds)*time.Second, maxCount, authorize, reserve
	return s, nil
}

// WithinRetentionLimits is a configuration check, not an authorization grant.
// The legacy component constructor cannot enable public retained workflows.
func (s *ResultStore) WithinRetentionLimits(worktree string, maxBytes, maxCount, ttlSeconds int64) bool {
	return s != nil && s.worktree == worktree && s.authorize != nil && s.reserve != nil &&
		s.maxResult >= 1024 && s.maxResult <= s.quota && s.quota <= maxBytes && int64(s.maxCount) <= maxCount &&
		s.ttl > 0 && s.ttl <= resultTTL && int64(s.ttl/time.Second) <= ttlSeconds
}

func (s *ResultStore) MaxResultBytes() int64 {
	if s == nil {
		return 0
	}
	return s.maxResult
}

func (s *ResultStore) checkAuthority(ctx context.Context) error {
	if ctx == nil {
		return ErrResultInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.authorize != nil {
		if err := s.authorize(ctx); err != nil {
			return errors.Join(ErrResultStoreUnsafe, err)
		}
	}
	return ctx.Err()
}

func resultHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func resultUUID(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func randomResultID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

func (s *ResultStore) validateResult(result RenameStoredResult, principal string, expected model.ScopeVector) error {
	h := result.Header
	if h.FormatVersion != 1 || h.EngineSchema != 1 || h.WorktreeID != s.worktree || !resultUUID(h.ResultID) || !resultHash(principal) || h.PrincipalDigest != principal || !resultHash(h.ResultDigest) || !resultHash(h.PayloadHash) || h.IssuedAt < 0 || h.ExpiresAt <= h.IssuedAt || h.ExpiresAt-h.IssuedAt > int64(s.ttl/time.Second) {
		return ErrResultInvalid
	}
	if err := model.ComparePinnedScope(h.Scope, expected, int(s.maxResult)); err != nil {
		return ErrResultInvalid
	}
	member := false
	for _, m := range h.Scope.Members {
		if m.WorktreeID == s.worktree {
			member = true
		}
	}
	if !member {
		return ErrResultInvalid
	}
	now := s.now().Unix()
	if now < h.IssuedAt || now >= h.ExpiresAt {
		return ErrResultExpired
	}
	payload, err := json.Marshal(result.Payload)
	if err != nil || int64(len(payload)) > s.maxResult {
		return ErrResultTooLarge
	}
	ordered, err := normalizeRenamePayload(result.Payload, int(s.maxResult))
	if err != nil {
		return ErrResultInvalid
	}
	orderedRaw, err := json.Marshal(ordered)
	if err != nil || !bytes.Equal(payload, orderedRaw) {
		return ErrResultInvalid
	}
	hash := sha256.Sum256(payload)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(hash[:])), []byte(h.PayloadHash)) != 1 {
		return ErrResultInvalid
	}
	digest, err := model.RenameResultDigest(h.Scope, result.Payload.InitialRequest, result.Payload.ProviderDigest, result.Payload.FilePreconditions, result.Payload.Edits, result.Payload.Conflicts, int(s.maxResult))
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(h.ResultDigest)) != 1 {
		return ErrResultInvalid
	}
	return nil
}

func (s *ResultStore) prepare(scope model.ScopeVector, principal string, payload RenameResultPayload) (RenameStoredResult, []byte, error) {
	if s == nil || s.now == nil || !resultHash(principal) || payload.FilePreconditions == nil || payload.Edits == nil || payload.Conflicts == nil || len(payload.Edits) > 10000 || len(payload.FilePreconditions) > 10000 || len(payload.Conflicts) > 10000 {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	if !resultHash(payload.ProviderDigest) || len(payload.InitialRequest.NewName) > 1024 || len(payload.InitialRequest.Selector.Kind) > 192 || len(payload.InitialRequest.Selector.Path) > 16384 || !resultHash(payload.InitialRequest.Selector.ContentHash) {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	if requestScope := payload.InitialRequest.Scope; requestScope != nil {
		for _, field := range []*string{requestScope.WorktreeID, requestScope.ProfileID, requestScope.GenerationID, requestScope.GroupID} {
			if field != nil && len(*field) > 64 {
				return RenameStoredResult{}, nil, ErrResultInvalid
			}
		}
		if requestScope.PinnedVector != nil {
			if err := model.VerifyScopeVectorDigest(*requestScope.PinnedVector, int(s.maxResult)); err != nil {
				return RenameStoredResult{}, nil, ErrResultInvalid
			}
		}
	}
	// Bound individual strings before JSON encoding and account each item before
	// building the aggregate document. The final size is checked separately.
	used := int64(0)
	for _, pre := range payload.FilePreconditions {
		if len(pre.Path) > 16384 || !resultUUID(pre.WorktreeID) || !resultUUID(pre.GenerationID) || !resultHash(pre.ProfileID) || !resultHash(pre.ContentHash) {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		raw, err := json.Marshal(pre)
		if err != nil || int64(len(raw)) > s.maxResult-used {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		used += int64(len(raw))
	}
	for _, edit := range payload.Edits {
		if len(edit.NewText) > 524288 || len(edit.SourceRef.Path) > 16384 || !resultUUID(edit.SourceRef.WorktreeID) || !resultUUID(edit.SourceRef.GenerationID) || !resultHash(edit.SourceRef.ProfileID) || !resultHash(edit.SourceRef.ContentHash) {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		raw, err := json.Marshal(edit)
		if err != nil || int64(len(raw)) > s.maxResult-used {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		used += int64(len(raw))
	}
	for _, gap := range payload.Conflicts {
		if len(gap.Reason) > 4096 || len(gap.Code) > 768 || len(gap.SourceRefs) > 16 {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		for _, ref := range gap.SourceRefs {
			if len(ref.Path) > 16384 || !resultUUID(ref.WorktreeID) || !resultUUID(ref.GenerationID) || !resultHash(ref.ProfileID) || !resultHash(ref.ContentHash) {
				return RenameStoredResult{}, nil, ErrResultInvalid
			}
		}
		raw, err := json.Marshal(gap)
		if err != nil || int64(len(raw)) > s.maxResult-used {
			return RenameStoredResult{}, nil, ErrResultTooLarge
		}
		used += int64(len(raw))
	}
	normalized, err := model.NormalizeScopeVector(scope, int(s.maxResult))
	if err != nil {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	digest, err := model.RenameResultDigest(normalized, payload.InitialRequest, payload.ProviderDigest, payload.FilePreconditions, payload.Edits, payload.Conflicts, int(s.maxResult))
	if err != nil {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	payload, err = normalizeRenamePayload(payload, int(s.maxResult))
	if err != nil {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	orderedDigest, err := model.RenameResultDigest(normalized, payload.InitialRequest, payload.ProviderDigest, payload.FilePreconditions, payload.Edits, payload.Conflicts, int(s.maxResult))
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(orderedDigest)) != 1 {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	data, err := json.Marshal(payload)
	if err != nil || int64(len(data)) > s.maxResult {
		return RenameStoredResult{}, nil, ErrResultTooLarge
	}
	if err = s.scanner.CheckContent(data); err != nil {
		return RenameStoredResult{}, nil, err
	}
	var copied RenameResultPayload
	if err = json.Unmarshal(data, &copied); err != nil {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	id, err := randomResultID()
	if err != nil {
		return RenameStoredResult{}, nil, err
	}
	now := s.now().Unix()
	if now < 0 || now > 9007199254739991 {
		return RenameStoredResult{}, nil, ErrResultInvalid
	}
	hash := sha256.Sum256(data)
	header := RenameResultHeader{FormatVersion: 1, EngineSchema: 1, WorktreeID: s.worktree, ResultID: id, PrincipalDigest: principal, Scope: normalized, ResultDigest: digest, PayloadHash: hex.EncodeToString(hash[:]), IssuedAt: now, ExpiresAt: now + int64(s.ttl/time.Second)}
	result := RenameStoredResult{Header: header, Payload: copied}
	if err = s.validateResult(result, principal, normalized); err != nil {
		return RenameStoredResult{}, nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil || int64(len(raw)) > s.maxResult {
		return RenameStoredResult{}, nil, ErrResultTooLarge
	}
	if err = s.scanner.CheckContent(raw); err != nil {
		return RenameStoredResult{}, nil, err
	}
	return result, raw, nil
}

func (s *ResultStore) decode(raw []byte, id, principal string, scope model.ScopeVector) (RenameStoredResult, error) {
	if int64(len(raw)) > s.maxResult {
		return RenameStoredResult{}, ErrResultTooLarge
	}
	if err := s.scanner.CheckContent(raw); err != nil {
		return RenameStoredResult{}, err
	}
	if _, err := model.DecodeIdentityJSON(raw, int(s.maxResult)); err != nil {
		return RenameStoredResult{}, ErrResultInvalid
	}
	var result RenameStoredResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return RenameStoredResult{}, ErrResultInvalid
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, raw) || result.Header.ResultID != id {
		return RenameStoredResult{}, ErrResultInvalid
	}
	if err = s.validateResult(result, principal, scope); err != nil {
		return RenameStoredResult{}, err
	}
	return result, nil
}

func resultGuard(ctx context.Context, guard ResultGuard, result RenameStoredResult) error {
	if ctx == nil || guard == nil {
		return ErrResultInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrResultInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return ErrResultInvalid
	}
	var copyResult RenameStoredResult
	if err = json.Unmarshal(raw, &copyResult); err != nil {
		return ErrResultInvalid
	}
	if err = guard(ctx, copyResult); err != nil {
		return err
	}
	return ctx.Err()
}

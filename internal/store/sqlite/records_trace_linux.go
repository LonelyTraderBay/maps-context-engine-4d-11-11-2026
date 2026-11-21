//go:build linux

package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

type ProtectedTraceImport struct {
	SourceRoot     *paths.Root
	RelativePath   string
	Approval       importtrace.Approval
	ApproveUpgrade bool
}

// ImportProtectedTrace is a local owner operation. The exclusive worktree
// lease and protected writer cover both the optional schema upgrade and the
// append. The ordinary app and index connectors open no new source path.
func ImportProtectedTrace(ctx context.Context, profile, worktree string, wait time.Duration, request ProtectedTraceImport,
	authorize func(context.Context) error) (records.TraceImportResult, error) {
	result, err := importProtectedEvidence(ctx, profile, worktree, wait, authorize,
		func(store *records.Store, writer ports.IndexWriterLease, job string, check records.StateAuthorizer) (records.EvidenceImportResult, error) {
			out, err := store.ImportTrace(ctx, records.TraceImportRequest{
				Root: request.SourceRoot, RelativePath: request.RelativePath, Approval: request.Approval,
				AllowUpgrade: request.ApproveUpgrade, Writer: writer, JobID: job, Authorize: check,
			})
			return records.EvidenceImportResult{ArtifactID: out.ArtifactID, RecordEpoch: out.RecordEpoch,
				ItemCount: out.SpanCount, Replayed: out.Replayed}, err
		})
	return records.TraceImportResult{ArtifactID: result.ArtifactID, RecordEpoch: result.RecordEpoch,
		SpanCount: result.ItemCount, Replayed: result.Replayed}, err
}

type ProtectedTestEvidenceImport struct {
	SourceRoot     *paths.Root
	RelativePath   string
	Approval       testevidence.Approval
	ApproveUpgrade bool
}

// ImportProtectedTestEvidence attaches local test reporting to the same
// exclusive records owner and WAL authority as runtime trace import.
func ImportProtectedTestEvidence(ctx context.Context, profile, worktree string, wait time.Duration, request ProtectedTestEvidenceImport,
	authorize func(context.Context) error) (records.EvidenceImportResult, error) {
	return importProtectedEvidence(ctx, profile, worktree, wait, authorize,
		func(store *records.Store, writer ports.IndexWriterLease, job string, check records.StateAuthorizer) (records.EvidenceImportResult, error) {
			return store.ImportTestEvidence(ctx, records.TestEvidenceImportRequest{
				Root: request.SourceRoot, RelativePath: request.RelativePath, Approval: request.Approval,
				AllowUpgrade: request.ApproveUpgrade, Writer: writer, JobID: job, Authorize: check,
			})
		})
}

func importProtectedEvidence(ctx context.Context, profile, worktree string, wait time.Duration,
	authorize func(context.Context) error,
	write func(*records.Store, ports.IndexWriterLease, string, records.StateAuthorizer) (records.EvidenceImportResult, error),
) (out records.EvidenceImportResult, resultErr error) {
	if _, err := profileUUIDBytes(worktree); err != nil {
		return out, ErrProfileInput
	}
	owner, err := openQuiescentOwner(ctx, profile, worktree, wait, authorize)
	if err != nil {
		return out, err
	}
	finish := func(err error) {
		if err != nil {
			if out.ArtifactID != "" && !out.Replayed {
				err = errors.Join(records.ErrAfterCommit, err)
			}
			out = records.EvidenceImportResult{}
			resultErr = errors.Join(resultErr, err)
		}
	}
	defer func() { finish(owner.Close()) }()
	if len(owner.stores) != 2 || owner.stores[1].name != "records.sqlite" {
		return out, ErrStateUnsafe
	}
	pool := owner.stores[1].pool(false, 1)
	store, err := OpenRecordsOwner(ctx, owner.lease, pool, worktree)
	if err != nil {
		return out, errors.Join(err, pool.Close())
	}
	defer func() { finish(store.Close()) }()
	reader, err := store.Store()
	if err != nil {
		return out, err
	}
	check := func(ctx context.Context, identity records.Identity) error {
		if identity.WorktreeID != worktree || identity.Incarnation != owner.manifest.RecordsIncarnation {
			return ErrStateUnsafe
		}
		return owner.check(ctx)
	}
	return write(reader, owner.writer, owner.operation, check)
}

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

const (
	windowsCatalogSeedName    = "seed.catalog.sqlite"
	windowsCatalogSeedRecord  = "seed.catalog.json"
	windowsCatalogSeedMaxSize = 1 << 20
)

type windowsCatalogSeed struct {
	FormatVersion     int    `json:"format_version"`
	ProfileID         string `json:"profile_id"`
	Incarnation       string `json:"catalog_incarnation"`
	ImageSHA256       string `json:"image_sha256"`
	ByteLength        int    `json:"byte_length"`
	ActiveIncarnation string `json:"active_incarnation,omitempty"`
}

// BootstrapWindowsCatalogSeed stages the frozen empty catalog under the
// controller. Serving still requires authenticated engine ownership and a
// separate latest WAL head. A seed is never a live state or a recovery input.
func BootstrapWindowsCatalogSeed(ctx context.Context, name string) (ProtectedProfileSummary, error) {
	return withWindowsProfileControl(ctx, name, func(m windowsProfilePreparation, files windowsMaintenanceFiles) (ProtectedProfileSummary, error) {
		return files.bootstrapCatalogSeed(ctx, m)
	})
}

func (j windowsMaintenanceFiles) bootstrapCatalogSeed(ctx context.Context, m windowsProfilePreparation) (out ProtectedProfileSummary, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	lease, err := runtime.AcquireLifecycle(ctx, j.control, "catalog", ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := j.requireAbsent(windowsCatalogSeedName, windowsCatalogSeedRecord, windowsMaintenanceName); err != nil {
		return out, err
	}
	image, incarnation, err := buildWindowsCatalogSeed(ctx)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(image)
	record := windowsCatalogSeed{FormatVersion: 1, ProfileID: m.ProfileID, Incarnation: incarnation,
		ImageSHA256: hex.EncodeToString(digest[:]), ByteLength: len(image)}
	raw, err := json.Marshal(record)
	if err != nil {
		return out, err
	}
	descriptor, err := j.acl.descriptor()
	if err != nil {
		return out, err
	}
	if err := lease.CheckRoot(ctx, j.control, "catalog", ports.ExclusiveLease); err != nil {
		return out, err
	}
	if err := j.publish(ctx, dir, descriptor, windowsCatalogSeedName, image, false, nil); err != nil {
		return out, err
	}
	// Check the exact persisted bytes and catalog identity before publishing
	// the completion record. Crash between the two leaves the seed incomplete.
	if err := j.verifyCatalogSeed(ctx, dir, record); err != nil {
		return out, err
	}
	if err := j.publish(ctx, dir, descriptor, windowsCatalogSeedRecord, raw, false, nil); err != nil {
		return out, err
	}
	return j.readCatalogSeed(ctx, dir, m)
}

func buildWindowsCatalogSeed(ctx context.Context) (_ []byte, incarnation string, resultErr error) {
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: ":memory:"})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if _, err := db.ExecContext(ctx, "PRAGMA page_size=4096"); err != nil {
		return nil, "", err
	}
	var pages int
	if err := db.QueryRowContext(ctx, "PRAGMA max_page_count=256").Scan(&pages); err != nil || pages != windowsCatalogSeedMaxSize/4096 {
		return nil, "", errors.Join(ErrStateLimit, err)
	}
	incarnation, err := catalog.Initialize(ctx, db)
	if err != nil {
		return nil, "", err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	var image []byte
	err = conn.Raw(func(driver any) error {
		serializer, ok := driver.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return ErrStateUnsupported
		}
		var err error
		image, err = serializer.Serialize()
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if len(image) < 512 || len(image) > windowsCatalogSeedMaxSize {
		return nil, "", ErrStateLimit
	}
	return image, incarnation, ctx.Err()
}

func (j windowsMaintenanceFiles) checkCatalogSeed(ctx context.Context, m windowsProfilePreparation) (out ProtectedProfileSummary, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	seed, err := j.readCatalogSeed(ctx, dir, m)
	if err != nil {
		return out, err
	}
	if seed.CatalogVerified {
		// A seed was published under this lease. Require the existing lock
		// under a pinned handle so inspection never creates a missing lock.
		pinned, err := j.control.OpenRegular("lifecycle.lock")
		if err != nil {
			return out, errors.Join(ErrProfileIncomplete, err)
		}
		defer func() { resultErr = errors.Join(resultErr, pinned.Close()) }()
		lease, err := runtime.AcquireLifecycle(ctx, j.control, "catalog", ports.SharedLease, 5*time.Second)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	} else {
		return seed, j.checkCatalogWAL(ctx, dir, seed.CatalogIncarnation)
	}
	if err := j.checkServe(ctx, ""); err != nil {
		return out, err
	}
	// The first read detects an unseeded profile. Take the serving snapshot
	// again under the lease because a restore can rotate the active identity.
	seed, err = j.readCatalogSeed(ctx, dir, m)
	if err != nil {
		return out, err
	}
	return seed, j.checkCatalogWAL(ctx, dir, seed.CatalogIncarnation)
}

func (j windowsMaintenanceFiles) readCatalogSeed(ctx context.Context, dir *os.File, m windowsProfilePreparation) (ProtectedProfileSummary, error) {
	summary := ProtectedProfileSummary{ProfileID: m.ProfileID, Stage: m.Stage}
	raw, present, err := j.readEntry(ctx, dir, windowsCatalogSeedRecord)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	if !present {
		file, err := j.control.OpenRegular(windowsCatalogSeedName)
		if err == nil {
			return ProtectedProfileSummary{}, errors.Join(ErrProfileIncomplete, file.Close())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return ProtectedProfileSummary{}, err
		}
		return summary, ctx.Err()
	}
	record, err := decodeWindowsCatalogSeed(raw, m.ProfileID)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	if err := j.verifyCatalogSeed(ctx, dir, record); err != nil {
		return ProtectedProfileSummary{}, err
	}
	summary.CatalogIncarnation = record.Incarnation
	if record.ActiveIncarnation != "" {
		summary.CatalogIncarnation = record.ActiveIncarnation
	}
	summary.CatalogVerified = true
	return summary, nil
}

func decodeWindowsCatalogSeed(raw []byte, profile string) (windowsCatalogSeed, error) {
	var record windowsCatalogSeed
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if model.ValidateWireJSON(raw, maintenanceJournalMaxBytes) != nil || decoder.Decode(&record) != nil ||
		record.FormatVersion != 1 || record.ProfileID != profile || !model.ValidUUID(record.Incarnation) ||
		!model.ValidDigest(record.ImageSHA256) || record.ByteLength < 512 || record.ByteLength > windowsCatalogSeedMaxSize ||
		record.ActiveIncarnation != "" && (!model.ValidUUID(record.ActiveIncarnation) || record.ActiveIncarnation == record.Incarnation) {
		return windowsCatalogSeed{}, ErrProfileIncomplete
	}
	return record, nil
}

func (j windowsMaintenanceFiles) verifyCatalogSeed(ctx context.Context, dir *os.File, record windowsCatalogSeed) (resultErr error) {
	image, present, err := j.readEntryLimit(ctx, dir, windowsCatalogSeedName, windowsCatalogSeedMaxSize)
	if err != nil || !present || len(image) != record.ByteLength {
		return errors.Join(ErrProfileIncomplete, err)
	}
	digest := sha256.Sum256(image)
	if hex.EncodeToString(digest[:]) != record.ImageSHA256 || string(image[:16]) != "SQLite format 3\x00" || image[18] != 1 || image[19] != 1 {
		return ErrProfileIncomplete
	}
	return verifyWindowsCatalogImage(ctx, image, record.Incarnation)
}

func verifyWindowsCatalogImage(ctx context.Context, image []byte, expectedIncarnation string) (resultErr error) {
	return withWindowsSQLiteImage(ctx, image, func(db *sql.DB) error {
		incarnation, err := catalog.VerifyReadOnlyImage(ctx, db)
		if err != nil || incarnation != expectedIncarnation {
			return errors.Join(ErrProfileIncomplete, err)
		}
		return ctx.Err()
	})
}

func withWindowsSQLiteImage(ctx context.Context, image []byte, use func(*sql.DB) error) (resultErr error) {
	db, release, err := openWindowsSQLiteImage(ctx, image)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	return use(db)
}

func openWindowsSQLiteImage(ctx context.Context, image []byte) (*sql.DB, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// Parse the captured image through an immutable VFS with no host path
	// access. No SQLite handle refers to the sealed on-disk file.
	uri, registry, err := windowsSQLiteImageURI(image)
	if err != nil {
		return nil, nil, err
	}
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: uri})
	db.SetMaxOpenConns(1)
	return db, func() error { return errors.Join(db.Close(), registry.Close()) }, nil
}

func windowsSQLiteImageURI(image []byte) (string, io.Closer, error) {
	name, registry, err := vfs.New(snapshotFS{data: image})
	if err != nil {
		return "", nil, err
	}
	options := url.Values{"mode": {"ro"}, "immutable": {"1"}, "vfs": {name},
		"_pragma": {"query_only(1)", "foreign_keys(1)", "temp_store(MEMORY)", "trusted_schema(0)"}}
	return "file:snapshot.sqlite?" + options.Encode(), registry, nil
}

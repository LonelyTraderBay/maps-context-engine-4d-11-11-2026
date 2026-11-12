package runtime

import (
	"errors"
	"io/fs"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var (
	ErrCleanupUnverified = errors.New("cleanup containment is not verified")
	ErrCleanupProtected  = errors.New("cleanup root overlaps a protected root")
)

// CleanupFileReport describes one observed regular file. It is not proof of
// provenance, disposal authority, stopped writers, or permission to remove it.
type CleanupFileReport struct {
	Path string
	Size int64
	Mode fs.FileMode
}

// InspectCleanupFile performs a read-only inspection under a root whose
// authorization is established by the caller. No file content is read and
// no deletion is attempted. Filesystem identity can change after this report;
// an eventual removal operation needs its own action-time containment proof.
func InspectCleanupFile(root *paths.Root, path string) (report CleanupFileReport, resultErr error) {
	file, err := root.OpenRegular(path)
	if err != nil {
		return CleanupFileReport{}, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			report = CleanupFileReport{}
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return CleanupFileReport{}, err
	}
	if err = root.Verify(); err != nil {
		return CleanupFileReport{}, err
	}
	return CleanupFileReport{Path: path, Size: info.Size(), Mode: info.Mode()}, nil
}

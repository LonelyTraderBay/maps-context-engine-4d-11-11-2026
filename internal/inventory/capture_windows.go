package inventory

import (
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func verifyCaptureFile(file *os.File) error { return paths.VerifyRegularFile(file) }

func sameCapturedObservation(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
}

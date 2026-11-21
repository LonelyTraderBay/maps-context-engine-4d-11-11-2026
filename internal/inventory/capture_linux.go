package inventory

import (
	"errors"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"os"
	"syscall"
)

func verifyCaptureFile(file *os.File) error {
	err := paths.VerifyRegularFile(file)
	if !errors.Is(err, paths.ErrHardLinked) {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		return errors.Join(err, statErr)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink == 0 {
		return errors.Join(ErrCaptureChanged, err)
	}
	return err
}

func sameCapturedObservation(a, b os.FileInfo) bool {
	before, okBefore := a.Sys().(*syscall.Stat_t)
	after, okAfter := b.Sys().(*syscall.Stat_t)
	return okBefore && okAfter && os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime()) && before.Ctim == after.Ctim
}

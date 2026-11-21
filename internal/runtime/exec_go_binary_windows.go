package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

const goExecutableName = "go.exe"

func openGoFile(root *paths.Root) (_ *os.File, resultErr error) {
	bin, err := root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	name := filepath.Join(bin, goExecutableName)
	if err := CheckAdministratorPath(name); err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), goExecutableName)
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	observed, err := root.OpenRegular(goExecutableName)
	if err != nil {
		return nil, err
	}
	pinnedStat, pinnedErr := file.Stat()
	observedStat, observedErr := observed.Stat()
	err = errors.Join(pinnedErr, observedErr, observed.Close(), paths.VerifyRegularFile(file), root.Verify())
	if err != nil || !os.SameFile(pinnedStat, observedStat) {
		return nil, errors.Join(ErrGoToolchain, err)
	}
	keep = true
	return file, nil
}

func goPathSafe(bin string) bool {
	return os.Getenv("PATH") == bin && strings.EqualFold(os.Getenv("PATHEXT"), ".EXE")
}

func goFileState(file *os.File) (windowsToolchainEntry, error) {
	if file == nil {
		return windowsToolchainEntry{}, ErrGoToolchain
	}
	state, err := windowsToolchainStat(file, false)
	if err != nil || goFileSize(state) < 1 {
		return state, errors.Join(ErrGoToolchain, err)
	}
	return state, paths.VerifyRegularFile(file)
}

func goFileSize(s windowsToolchainEntry) int64 {
	return int64(s.file.FileSizeHigh)<<32 | int64(s.file.FileSizeLow)
}

func sameGoFileState(a, b windowsToolchainEntry) bool { return a == b }

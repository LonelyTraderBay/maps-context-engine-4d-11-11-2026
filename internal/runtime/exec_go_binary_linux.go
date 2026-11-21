//go:build linux

package runtime

import (
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

const goExecutableName = "go"

func openGoFile(root *paths.Root) (*os.File, error) {
	return root.OpenRegular(goExecutableName)
}

func goPathSafe(bin string) bool { return os.Getenv("PATH") == bin }

func goFileState(file *os.File) (unix.Stat_t, error) {
	var state unix.Stat_t
	if file == nil {
		return state, ErrGoToolchain
	}
	err := unix.Fstat(int(file.Fd()), &state)
	if err != nil || state.Mode&unix.S_IFMT != unix.S_IFREG || state.Nlink != 1 || state.Mode&0111 == 0 || state.Size < 1 {
		return state, ErrGoToolchain
	}
	return state, nil
}

func goFileSize(s unix.Stat_t) int64 { return s.Size }

func sameGoFileState(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid &&
		a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

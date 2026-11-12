//go:build linux

package runtime

import (
	"os"
	"syscall"
)

func sameFileChangeTime(a, b os.FileInfo) bool {
	before, okBefore := a.Sys().(*syscall.Stat_t)
	after, okAfter := b.Sys().(*syscall.Stat_t)
	return okBefore && okAfter && before.Ctim == after.Ctim
}

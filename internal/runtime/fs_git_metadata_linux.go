//go:build linux

package runtime

import (
	"os"
	"syscall"
)

func gitFileChangeTime(_ *os.File, info os.FileInfo) ([2]int64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return [2]int64{}, ErrGitMetadataUnverified
	}
	return [2]int64{int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec)}, nil
}

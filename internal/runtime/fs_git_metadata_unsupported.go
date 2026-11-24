//go:build !linux && !windows

package runtime

import "os"

func gitFileChangeTime(*os.File, os.FileInfo) ([2]int64, error) {
	return [2]int64{}, ErrGitMetadataUnverified
}

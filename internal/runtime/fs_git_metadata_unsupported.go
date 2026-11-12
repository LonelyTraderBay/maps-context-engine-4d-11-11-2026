//go:build !linux

package runtime

import "os"

func sameFileChangeTime(os.FileInfo, os.FileInfo) bool { return false }

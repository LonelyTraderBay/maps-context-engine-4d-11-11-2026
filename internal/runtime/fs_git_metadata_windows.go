package runtime

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func gitFileChangeTime(file *os.File, _ os.FileInfo) ([2]int64, error) {
	var info windowsFileBasicInfo
	err := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return [2]int64{info.changed, 0}, err
}

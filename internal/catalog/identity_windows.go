package catalog

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

func directoryIdentity(root *paths.Root) (path, identity string, resultErr error) {
	path, err := root.CanonicalPath()
	if err != nil {
		return "", "", err
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err := dir.Close(); err != nil {
			path, identity = "", ""
			resultErr = errors.Join(resultErr, err)
		}
	}()
	var info struct {
		volume uint64
		id     [16]byte
	}
	if err := windows.GetFileInformationByHandleEx(windows.Handle(dir.Fd()), windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return "", "", err
	}
	if info.id == ([16]byte{}) {
		return "", "", paths.ErrUnsupported
	}
	if err := root.Verify(); err != nil {
		return "", "", err
	}
	return path, fmt.Sprintf("windows:%016x:%x", info.volume, info.id), nil
}

func sameDirectory(a, b *paths.Root) (bool, error) {
	_, aID, err := directoryIdentity(a)
	if err != nil {
		return false, err
	}
	_, bID, err := directoryIdentity(b)
	if err != nil {
		return false, err
	}
	return aID == bID, errors.Join(a.Verify(), b.Verify())
}

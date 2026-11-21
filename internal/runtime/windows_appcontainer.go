//go:build windows

package runtime

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getWindowsContainerFolder = windows.NewLazySystemDLL("userenv.dll").NewProc("GetAppContainerFolderPath")

// WindowsAppContainerFolder resolves the Windows-managed package data
// directory for an already provisioned package SID.
func WindowsAppContainerFolder(sid *windows.SID) (string, error) {
	if sid == nil || !sid.IsValid() || sid.SubAuthorityCount() != 8 {
		return "", ErrGoEnvironment
	}
	name, err := windows.UTF16PtrFromString(sid.String())
	if err != nil {
		return "", err
	}
	var path *uint16
	hr, _, _ := getWindowsContainerFolder.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&path)))
	if uint32(hr) != 0 || path == nil {
		return "", errors.New("Windows AppContainer storage is unavailable")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(path))
	resolved := windows.UTF16PtrToString(path)
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	rel, relErr := filepath.Rel(local, resolved)
	if err != nil || relErr != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) != resolved || rel == "." || !filepath.IsLocal(rel) {
		return "", ErrGoEnvironment
	}
	return resolved, nil
}

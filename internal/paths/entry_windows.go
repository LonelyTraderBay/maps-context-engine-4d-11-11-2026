package paths

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// OpenWindowsEntry opens a regular leaf relative to a pinned directory.
// The caller owns the directory and must verify the file's required ACL and
// identity before use.
func OpenWindowsEntry(dir *os.File, name string, descriptor *windows.SECURITY_DESCRIPTOR, rename bool, disposition, share uint32) (*os.File, error) {
	segments, err := windowsSegments(name)
	if err != nil || len(segments) != 1 {
		return nil, ErrInvalidPath
	}
	relative, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE)
	if rename {
		access |= windows.DELETE
	}
	var file windows.Handle
	err = withWindowsHandle(dir, func(parent windows.Handle) error {
		attr := windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: relative,
			Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE, SecurityDescriptor: descriptor}
		attr.Length = uint32(unsafe.Sizeof(attr))
		var status windows.IO_STATUS_BLOCK
		return windows.NtCreateFile(&file, access, &attr, &status, nil, windows.FILE_ATTRIBUTE_NORMAL,
			share, disposition,
			windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(file), name), nil
}

// RenameWindowsEntry renames the opened entry within its original directory.
// The caller must hold DELETE access, keep the directory pinned and verify
// both the source and the installed identity.
func RenameWindowsEntry(file *os.File, name string, replace bool) error {
	segments, err := windowsSegments(name)
	if err != nil || len(segments) != 1 {
		return ErrInvalidPath
	}
	var rename struct {
		replace uint8
		parent  windows.Handle
		length  uint32
		name    [64]uint16
	}
	utf16, err := windows.UTF16FromString(name)
	if err != nil || len(utf16) > len(rename.name) {
		return ErrInvalidPath
	}
	if replace {
		rename.replace = 1
	}
	rename.length = uint32(copy(rename.name[:], utf16[:len(utf16)-1]) * 2)
	return withWindowsHandle(file, func(handle windows.Handle) error {
		var status windows.IO_STATUS_BLOCK
		return windows.NtSetInformationFile(handle, &status,
			(*byte)(unsafe.Pointer(&rename)), uint32(unsafe.Sizeof(rename)), windows.FileRenameInformation)
	})
}

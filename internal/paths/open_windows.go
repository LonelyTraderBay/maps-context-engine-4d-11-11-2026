package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"golang.org/x/sys/windows"
)

// Root keeps each ancestor directory open without delete sharing. Relative
// reads resolve one component at a time from a verified directory handle.
type Root struct {
	dir       *os.File
	ancestors []*os.File
	path      string
	volume    uint32
}

const windowsShare = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE

func OpenRoot(path string) (*Root, error) {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	drive := strings.ToUpper(volume) + "\\"
	if drive[0] < 'A' || drive[0] > 'Z' || drive[1] != ':' || len(path) < len(drive) {
		return nil, ErrInvalidPath
	}
	var segments []string
	if path[len(drive):] != "" {
		var err error
		segments, err = windowsSegments(filepath.ToSlash(path[len(drive):]))
		if err != nil {
			return nil, err
		}
	}
	driveName, err := windows.UTF16PtrFromString(drive)
	if err != nil {
		return nil, err
	}
	if windows.GetDriveType(driveName) != windows.DRIVE_FIXED {
		return nil, ErrUnsupported
	}
	handle, err := windows.CreateFile(driveName, windows.FILE_GENERIC_READ,
		windowsShare, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	anchor := os.NewFile(uintptr(handle), "confined-drive")
	var filesystem [32]uint16
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, nil, &filesystem[0], uint32(len(filesystem))); err != nil {
		return nil, errors.Join(ErrUnsupported, err, anchor.Close())
	}
	if windows.UTF16ToString(filesystem[:]) != "NTFS" {
		return nil, errors.Join(ErrUnsupported, anchor.Close())
	}
	if err := verifyWindowsName(anchor, drive, false); err != nil {
		return nil, errors.Join(err, anchor.Close())
	}
	root := &Root{dir: anchor}
	info, err := windowsEntryInfo(handle, true)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	root.volume = info.VolumeSerialNumber
	for _, segment := range segments {
		dir, err := openWindowsChild(root.dir, segment, root.volume, true)
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		root.ancestors = append(root.ancestors, root.dir)
		root.dir = dir
	}
	root.path, err = windowsName(root.dir)
	if err == nil && !strings.EqualFold(root.path, path) {
		err = ErrInvalidPath
	}
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return root, nil
}

func windowsSegments(path string) ([]string, error) {
	if model.ValidateRelativePath(path, false) != nil {
		return nil, ErrInvalidPath
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if !filepath.IsLocal(segment) || strings.ContainsAny(segment, "<>\"|?*") ||
			strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return nil, ErrInvalidPath
		}
	}
	return segments, nil
}

func openWindowsChild(dir *os.File, segment string, volume uint32, directory bool) (*os.File, error) {
	name, err := windows.NewNTUnicodeString(segment)
	if err != nil {
		return nil, err
	}
	var handle windows.Handle
	options := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_NON_DIRECTORY_FILE | windows.FILE_OPEN_REPARSE_POINT)
	if directory {
		options = windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_DIRECTORY_FILE | windows.FILE_OPEN_REPARSE_POINT
	}
	err = withWindowsHandle(dir, func(parent windows.Handle) error {
		attributes := windows.OBJECT_ATTRIBUTES{
			RootDirectory: parent,
			ObjectName:    name,
			Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
		}
		attributes.Length = uint32(unsafe.Sizeof(attributes))
		var status windows.IO_STATUS_BLOCK
		return windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ, &attributes, &status, nil, 0, windowsShare, windows.FILE_OPEN, options, 0, 0)
	})
	if err != nil {
		switch err {
		case windows.STATUS_REPARSE_POINT_ENCOUNTERED:
			return nil, ErrInvalidPath
		case windows.STATUS_FILE_IS_A_DIRECTORY, windows.STATUS_NOT_A_DIRECTORY:
			return nil, ErrNotRegular
		}
		if status, ok := err.(windows.NTStatus); ok {
			return nil, status.Errno()
		}
		return nil, err
	}
	file := os.NewFile(uintptr(handle), "confined-entry")
	info, err := windowsEntryInfo(handle, directory)
	if err == nil && info.VolumeSerialNumber != volume {
		err = ErrInvalidPath
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func windowsEntryInfo(handle windows.Handle, directory bool) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return info, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 {
		return info, ErrInvalidPath
	}
	if (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return info, ErrNotRegular
	}
	if directory {
		var flags uint32
		var status windows.IO_STATUS_BLOCK
		if err := windows.NtQueryInformationFile(handle, &status, (*byte)(unsafe.Pointer(&flags)), 4,
			windows.FileCaseSensitiveInformation); err != nil {
			return info, errors.Join(ErrUnsupported, err)
		}
		if flags != 0 {
			return info, ErrUnsupported
		}
	} else if info.NumberOfLinks != 1 {
		return info, ErrHardLinked
	}
	return info, nil
}

func (r *Root) OpenDirectory(path string) (*Root, error) {
	if _, err := windowsSegments(path); err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	requested := filepath.Join(r.path, filepath.FromSlash(path))
	child, err := OpenRoot(requested)
	if err != nil {
		return nil, err
	}
	if child.volume != r.volume || child.path != requested {
		return nil, errors.Join(ErrInvalidPath, child.Close())
	}
	if err := r.Verify(); err != nil {
		return nil, errors.Join(err, child.Close())
	}
	return child, nil
}

func (r *Root) OpenRegular(path string) (*os.File, error) {
	segments, err := windowsSegments(path)
	if err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	dir := r.dir
	for _, segment := range segments[:len(segments)-1] {
		dir, err = openWindowsChild(dir, segment, r.volume, true)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
	}
	file, err := openWindowsChild(dir, segments[len(segments)-1], r.volume, false)
	if err != nil {
		return nil, err
	}
	if err := verifyWindowsName(file, filepath.Join(r.path, filepath.FromSlash(path)), true); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := r.Verify(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func VerifyRegularFile(file *os.File) error {
	return withWindowsHandle(file, func(handle windows.Handle) error {
		kind, err := windows.GetFileType(handle)
		if err != nil {
			return err
		}
		if kind != windows.FILE_TYPE_DISK {
			return ErrNotRegular
		}
		_, err = windowsEntryInfo(handle, false)
		return err
	})
}

func (r *Root) Verify() error {
	if r == nil || r.dir == nil || r.path == "" {
		return ErrInvalidPath
	}
	if err := withWindowsHandle(r.dir, func(handle windows.Handle) error {
		info, err := windowsEntryInfo(handle, true)
		if err == nil && info.VolumeSerialNumber != r.volume {
			return ErrRootChanged
		}
		return err
	}); err != nil {
		return errors.Join(ErrRootChanged, err)
	}
	if err := verifyWindowsName(r.dir, r.path, true); err != nil {
		return errors.Join(ErrRootChanged, err)
	}
	return nil
}

func (r *Root) CanonicalPath() (string, error) {
	if err := r.Verify(); err != nil {
		return "", err
	}
	return r.path, nil
}

func (r *Root) Close() error {
	if r == nil || r.dir == nil {
		return ErrInvalidPath
	}
	err := r.dir.Close()
	for i := len(r.ancestors) - 1; i >= 0; i-- {
		err = errors.Join(err, r.ancestors[i].Close())
	}
	return err
}

func (r *Root) DirectoryHandle() (*os.File, error) {
	if err := r.Verify(); err != nil {
		return nil, err
	}
	var duplicate windows.Handle
	self := windows.CurrentProcess()
	err := withWindowsHandle(r.dir, func(handle windows.Handle) error {
		return windows.DuplicateHandle(self, handle, self, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(duplicate), "confined-directory"), nil
}

func withWindowsHandle(file *os.File, use func(windows.Handle) error) error {
	if file == nil {
		return ErrInvalidPath
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var operationErr error
	if err := raw.Control(func(handle uintptr) { operationErr = use(windows.Handle(handle)) }); err != nil {
		return err
	}
	return operationErr
}

func verifyWindowsName(file *os.File, expected string, exact bool) error {
	actual, err := windowsName(file)
	if err != nil {
		return err
	}
	if actual == expected || !exact && strings.EqualFold(actual, expected) {
		return nil
	}
	return ErrInvalidPath
}

func windowsName(file *os.File) (string, error) {
	var result string
	err := withWindowsHandle(file, func(handle windows.Handle) error {
		buffer := make([]uint16, 260)
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return err
		}
		if length >= uint32(len(buffer)) {
			if length > 32768 {
				return ErrInvalidPath
			}
			buffer = make([]uint16, length)
			length, err = windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
			if err != nil {
				return err
			}
			if length >= uint32(len(buffer)) {
				return ErrInvalidPath
			}
		}
		const localPrefix = "\\\\?\\"
		name := windows.UTF16ToString(buffer[:length])
		if !strings.HasPrefix(name, localPrefix) || len(filepath.VolumeName(name[len(localPrefix):])) != 2 {
			return ErrUnsupported
		}
		result = name[len(localPrefix):]
		return nil
	})
	return result, err
}

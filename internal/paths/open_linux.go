//go:build linux

package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"golang.org/x/sys/unix"
)

// Root holds the directory handle used as the anchor for every source open.
// Callers must establish root registration, permission, and stable identity.
type Root struct {
	dir  *os.File
	path string
	dev  uint64
	ino  uint64
}

func OpenRoot(path string) (*Root, error) {
	if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return nil, ErrInvalidPath
		}
	}
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	var identity unix.Stat_t
	if err = unix.Fstat(fd, &identity); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return &Root{dir: os.NewFile(uintptr(fd), "confined-root"), path: path, dev: uint64(identity.Dev), ino: identity.Ino}, nil
}

// OpenDirectory opens a child directory beneath this root without following
// links or crossing mounts. The caller is responsible for authorization.
func (r *Root) OpenDirectory(path string) (*Root, error) {
	if r == nil || r.dir == nil {
		return nil, ErrInvalidPath
	}
	if model.ValidateRelativePath(path, false) != nil {
		return nil, ErrInvalidPath
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	raw, err := r.dir.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var openErr error
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV}
	if err = raw.Control(func(rootFD uintptr) { fd, openErr = unix.Openat2(int(rootFD), path, how) }); err != nil {
		return nil, err
	}
	if openErr != nil {
		if errors.Is(openErr, unix.ENOSYS) || errors.Is(openErr, unix.EINVAL) {
			return nil, ErrUnsupported
		}
		return nil, openErr
	}
	var identity unix.Stat_t
	if err = unix.Fstat(fd, &identity); err != nil {
		unix.Close(fd)
		return nil, err
	}
	child := &Root{dir: os.NewFile(uintptr(fd), "confined-directory"), path: filepath.Join(r.path, filepath.FromSlash(path)), dev: uint64(identity.Dev), ino: identity.Ino}
	if err = r.Verify(); err == nil {
		err = child.Verify()
	}
	if err != nil {
		return nil, errors.Join(err, child.Close())
	}
	return child, nil
}

// CanonicalPath returns the lexical path for an identity-verified root. It is
// not evidence of registration or permission and must not be logged by default.
func (r *Root) CanonicalPath() (string, error) {
	if err := r.Verify(); err != nil {
		return "", err
	}
	return r.path, nil
}

// OpenRegular resolves under the anchored root without symlinks or mount
// crossings. An O_PATH handle is checked before any content-open operation;
// the verified procfs descriptor reopens that pinned regular inode.
func (r *Root) OpenRegular(path string) (*os.File, error) {
	if r == nil || r.dir == nil {
		return nil, ErrInvalidPath
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	if err := model.ValidateRelativePath(path, false); err != nil {
		return nil, ErrInvalidPath
	}
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV}
	raw, err := r.dir.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var openErr error
	if err = raw.Control(func(rootFD uintptr) { fd, openErr = unix.Openat2(int(rootFD), path, how) }); err != nil {
		return nil, err
	}
	if openErr != nil {
		if errors.Is(openErr, unix.ENOSYS) || errors.Is(openErr, unix.EINVAL) {
			return nil, ErrUnsupported
		}
		return nil, openErr
	}
	defer unix.Close(fd)
	var pinned unix.Stat_t
	if err = unix.Fstat(fd, &pinned); err != nil {
		return nil, err
	}
	if pinned.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, ErrNotRegular
	}
	if pinned.Nlink != 1 {
		return nil, ErrHardLinked
	}
	proc, err := unix.Open("/proc/self/fd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsupported
	}
	defer unix.Close(proc)
	var filesystem unix.Statfs_t
	if err = unix.Fstatfs(proc, &filesystem); err != nil || uint64(filesystem.Type) != uint64(unix.PROC_SUPER_MAGIC) {
		return nil, ErrUnsupported
	}
	opened, err := unix.Openat(proc, strconv.Itoa(fd), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	var result unix.Stat_t
	if err = unix.Fstat(opened, &result); err != nil {
		unix.Close(opened)
		return nil, err
	}
	if result.Mode&unix.S_IFMT != unix.S_IFREG || result.Dev != pinned.Dev || result.Ino != pinned.Ino {
		unix.Close(opened)
		return nil, ErrInvalidPath
	}
	if result.Nlink != 1 {
		unix.Close(opened)
		return nil, ErrHardLinked
	}
	if err = r.Verify(); err != nil {
		unix.Close(opened)
		return nil, err
	}
	return os.NewFile(uintptr(opened), "confined-file"), nil
}

// VerifyRegularFile checks the still-open file before its bytes are released.
// It cannot rule out transient link changes between observations.
func VerifyRegularFile(file *os.File) error {
	if file == nil {
		return ErrInvalidPath
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	var statErr error
	if err = raw.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &stat) }); err != nil {
		return err
	}
	if statErr != nil {
		return statErr
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return ErrNotRegular
	}
	if stat.Nlink != 1 {
		return ErrHardLinked
	}
	return nil
}

// Verify observes whether the registered path still denotes the anchored inode.
// A caller must also check its trusted registration and policy revisions.
func (r *Root) Verify() error {
	if r == nil || r.dir == nil || r.path == "" {
		return ErrInvalidPath
	}
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS}
	fd, err := unix.Openat2(unix.AT_FDCWD, r.path, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
			return ErrUnsupported
		}
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
			return ErrRootChanged
		}
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if uint64(st.Dev) != r.dev || st.Ino != r.ino {
		return ErrRootChanged
	}
	return nil
}
func (r *Root) Close() error {
	if r == nil || r.dir == nil {
		return ErrInvalidPath
	}
	return r.dir.Close()
}

// DirectoryHandle duplicates the anchored directory for an owning adapter.
// The caller must close the returned handle and apply its own access policy.
func (r *Root) DirectoryHandle() (*os.File, error) {
	if r == nil || r.dir == nil {
		return nil, ErrInvalidPath
	}
	raw, err := r.dir.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var dupErr error
	if err = raw.Control(func(rootFD uintptr) { fd, dupErr = unix.FcntlInt(rootFD, unix.F_DUPFD_CLOEXEC, 3) }); err != nil {
		return nil, err
	}
	if dupErr != nil {
		return nil, dupErr
	}
	return os.NewFile(uintptr(fd), "confined-directory"), nil
}

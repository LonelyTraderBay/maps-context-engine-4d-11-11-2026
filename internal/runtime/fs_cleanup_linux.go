//go:build linux

package runtime

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

type cleanupDirID struct {
	deviceMajor uint32
	deviceMinor uint32
	inode       uint64
}

type cleanupAncestors struct {
	mount uint64
	self  cleanupDirID
	dirs  map[cleanupDirID]struct{}
}

// CheckCleanupRoot checks that a directory root neither contains nor is
// contained by any supplied protected root. The caller must supply the full
// protected set, prove provenance/authority and stopped writers, and repeat
// containment at action time. This check never authorizes or performs removal.
// Unknown/different mounts fail closed because bind mounts can alias data.
func CheckCleanupRoot(output *paths.Root, protected []*paths.Root) error {
	if output == nil || len(protected) == 0 || len(protected) > 64 {
		return ErrCleanupUnverified
	}
	protected = append([]*paths.Root(nil), protected...)
	out, err := cleanupRootAncestors(output)
	if err != nil {
		return errors.Join(ErrCleanupUnverified, err)
	}
	nestedMount, err := cleanupRootHasNestedMount(output)
	if err != nil {
		return errors.Join(ErrCleanupUnverified, err)
	}
	if nestedMount {
		return ErrCleanupUnverified
	}
	for _, root := range protected {
		if root == nil {
			return ErrCleanupUnverified
		}
		other, err := cleanupRootAncestors(root)
		if err != nil {
			return errors.Join(ErrCleanupUnverified, err)
		}
		if out.mount != other.mount {
			return ErrCleanupUnverified
		}
		if _, ok := out.dirs[other.self]; ok {
			return ErrCleanupProtected
		}
		if _, ok := other.dirs[out.self]; ok {
			return ErrCleanupProtected
		}
	}
	if err := output.Verify(); err != nil {
		return errors.Join(ErrCleanupUnverified, err)
	}
	for _, root := range protected {
		if err := root.Verify(); err != nil {
			return errors.Join(ErrCleanupUnverified, err)
		}
	}
	return nil
}

// cleanupRootHasNestedMount rejects every mount below the output root. Even a
// bind mount whose source appears disjoint can expose protected content there.
// Mount topology is observed, not locked; callers must recheck at action time.
func cleanupRootHasNestedMount(root *paths.Root) (found bool, resultErr error) {
	rootPath, err := root.CanonicalPath()
	if err != nil {
		return false, err
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var stat unix.Statfs_t
	if err = unix.Fstatfs(int(file.Fd()), &stat); err != nil || uint64(stat.Type) != uint64(unix.PROC_SUPER_MAGIC) {
		return false, errors.Join(ErrCleanupUnverified, err)
	}
	const maxMountInfoBytes = 4 * 1024 * 1024
	content, err := io.ReadAll(io.LimitReader(file, maxMountInfoBytes+1))
	if err != nil {
		return false, err
	}
	if len(content) == 0 || len(content) > maxMountInfoBytes || content[len(content)-1] != '\n' {
		return false, ErrCleanupUnverified
	}
	for _, line := range bytes.Split(content[:len(content)-1], []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) < 10 || !bytes.Contains(line, []byte(" - ")) {
			return false, ErrCleanupUnverified
		}
		mountPath, ok := decodeCleanupMountPath(string(fields[4]))
		if !ok || !filepath.IsAbs(mountPath) || filepath.Clean(mountPath) != mountPath {
			return false, ErrCleanupUnverified
		}
		if isNestedCleanupPath(rootPath, mountPath) {
			return true, nil
		}
	}
	if err = root.Verify(); err != nil {
		return false, err
	}
	return false, nil
}

func isNestedCleanupPath(root, candidate string) bool {
	if root == string(filepath.Separator) {
		return candidate != root && strings.HasPrefix(candidate, root)
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

func decodeCleanupMountPath(value string) (string, bool) {
	decoded := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			decoded = append(decoded, value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", false
		}
		var octet uint16
		for j := 1; j <= 3; j++ {
			c := value[i+j]
			if c < '0' || c > '7' {
				return "", false
			}
			octet = octet*8 + uint16(c-'0')
		}
		if octet == 0 || octet > 255 {
			return "", false
		}
		decoded = append(decoded, byte(octet))
		i += 3
	}
	return string(decoded), true
}

// cleanupRootAncestors walks directory handles only within one mount. A
// bounded walk and unique mount IDs keep unknown mount topology fail-closed.
func cleanupRootAncestors(root *paths.Root) (result cleanupAncestors, resultErr error) {
	if err := root.Verify(); err != nil {
		return result, err
	}
	file, err := root.DirectoryHandle()
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	initial, mount, err := cleanupDirIdentity(file)
	if err != nil {
		return result, err
	}
	result = cleanupAncestors{mount: mount, self: initial, dirs: map[cleanupDirID]struct{}{}}
	for depth := 0; depth < 256; depth++ {
		current, currentMount, err := cleanupDirIdentity(file)
		if err != nil || currentMount != mount {
			return result, errors.Join(ErrCleanupUnverified, err)
		}
		if _, exists := result.dirs[current]; exists {
			return result, ErrCleanupUnverified
		}
		result.dirs[current] = struct{}{}
		fd, err := unix.Openat(int(file.Fd()), "..", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return result, err
		}
		parent := os.NewFile(uintptr(fd), "cleanup-parent")
		parentID, parentMount, statErr := cleanupDirIdentity(parent)
		if statErr != nil {
			return result, errors.Join(statErr, parent.Close())
		}
		if parentMount != mount || parentID == current {
			return result, parent.Close()
		}
		if err = file.Close(); err != nil {
			return result, errors.Join(err, parent.Close())
		}
		file = parent
	}
	return result, ErrCleanupUnverified
}

func cleanupDirIdentity(file *os.File) (cleanupDirID, uint64, error) {
	var info unix.Statx_t
	mask := unix.STATX_INO | unix.STATX_TYPE | unix.STATX_MNT_ID_UNIQUE
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_NO_AUTOMOUNT, mask, &info); err != nil {
		return cleanupDirID{}, 0, err
	}
	if info.Mask&uint32(mask) != uint32(mask) || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mnt_id == 0 {
		return cleanupDirID{}, 0, ErrCleanupUnverified
	}
	return cleanupDirID{deviceMajor: info.Dev_major, deviceMinor: info.Dev_minor, inode: info.Ino}, info.Mnt_id, nil
}

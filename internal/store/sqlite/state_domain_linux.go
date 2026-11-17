//go:build linux

package sqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

// stateDomain validates a preprovisioned Linux privilege boundary. It never
// changes ownership, permissions, sysctls, credentials or mounts. The caller
// authenticates clientUID independently of requests and keeps all roots open.
//
// The engine account and administrator remain trusted (S14). Untrusted source
// tools MUST NOT run under the engine UID. This is not protection against a
// malicious engine-account process or an administrator rolling back the disk.
// Existing main/sidecar inode safety is a separate prerequisite: call Observe
// before opening SQLite. A protected directory cannot undo an existing alias.
type stateDomain struct {
	root      *paths.Root
	protected []*paths.Root
	owner     uint32
	client    uint32
	anchor    *stateDomain
}

// Child borrows the top-level privilege boundary. Directories below the
// owner-only state root may be engine-owned; an external client still cannot
// traverse or move them. Cooperating engine processes must additionally hold
// the matching lifecycle lease. The caller closes child.root after all users.
func (d *stateDomain) Child(ctx context.Context, relative string) (*stateDomain, error) {
	if d == nil || d.Check(ctx) != nil {
		return nil, ErrStateUnsafe
	}
	root, err := d.root.OpenDirectory(relative)
	if err != nil {
		return nil, ErrStateUnsafe
	}
	anchor := d
	if d.anchor != nil {
		anchor = d.anchor
	}
	child := &stateDomain{root: root, protected: append([]*paths.Root(nil), d.protected...), owner: d.owner, client: d.client, anchor: anchor}
	if err := child.Check(ctx); err != nil {
		_ = root.Close()
		return nil, err
	}
	return child, nil
}

func openStateDomain(ctx context.Context, root *paths.Root, protected []*paths.Root, clientUID uint32) (*stateDomain, error) {
	d := &stateDomain{root: root, protected: append([]*paths.Root(nil), protected...), owner: uint32(unix.Geteuid()), client: clientUID}
	if err := d.Check(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *stateDomain) Observe(ctx context.Context, lease *runtime.Lifecycle, scope, name string, maxBytes int64) (*StateObservation, error) {
	if d == nil {
		return nil, ErrStateUnsafe
	}
	return ObserveStateFiles(ctx, d.root, d.protected, lease, scope, name, maxBytes, d.Check)
}

func unprivilegedStateProcess(ctx context.Context) error {
	if ctx == nil {
		return ErrStateUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if unix.Geteuid() == 0 || unix.Getuid() != unix.Geteuid() || unix.Getgid() != unix.Getegid() {
		return ErrStateUnsafe
	}
	var caps [2]unix.CapUserData
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if unix.Capget(&header, &caps[0]) != nil || caps != [2]unix.CapUserData{} {
		return ErrStateUnsafe
	}
	noNewPrivileges, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || noNewPrivileges != 1 {
		return ErrStateUnsafe
	}
	mapFile, err := os.Open("/proc/self/uid_map")
	if err != nil {
		return ErrStateUnsupported
	}
	var procFS unix.Statfs_t
	statErr := unix.Fstatfs(int(mapFile.Fd()), &procFS)
	uidMap, readErr := io.ReadAll(io.LimitReader(mapFile, 128))
	closeErr := mapFile.Close()
	fields := strings.Fields(string(uidMap))
	if errors.Join(statErr, readErr, closeErr) != nil || uint64(procFS.Type) != uint64(unix.PROC_SUPER_MAGIC) ||
		len(fields) != 3 || fields[0] != "0" || fields[1] != "0" || fields[2] != "4294967295" {
		return ErrStateUnsafe
	}
	return ctx.Err()
}

func (d *stateDomain) Check(ctx context.Context) error {
	return checkStateDomains(ctx, d)
}

// Check shared ancestor/process evidence once within this single observation.
// Nothing is retained between SQL statements, transactions or public calls.
func checkStateDomains(ctx context.Context, domains ...*stateDomain) error {
	if err := unprivilegedStateProcess(ctx); err != nil {
		return err
	}
	seen := make(map[*stateDomain]bool, len(domains)+1)
	for _, domain := range domains {
		if err := checkStateTopology(ctx, domain, seen); err != nil {
			return err
		}
	}
	return checkProtectedHardlinks(ctx)
}

func checkStateTopology(ctx context.Context, d *stateDomain, seen map[*stateDomain]bool) error {
	if d != nil && seen[d] {
		return ctx.Err()
	}
	if d == nil || d.owner != uint32(unix.Geteuid()) || d.client == 0 || d.client == d.owner ||
		stateRootOwner(d.root) != nil || runtime.CheckCleanupRoot(d.root, d.protected) != nil {
		return ErrStateUnsafe
	}
	if d.anchor == nil {
		if err := rootManagedAncestors(ctx, d.root, false); err != nil {
			return err
		}
	} else {
		if d.anchor == d || d.anchor.anchor != nil || d.anchor.owner != d.owner || d.anchor.client != d.client || checkStateTopology(ctx, d.anchor, seen) != nil {
			return ErrStateUnsafe
		}
		base, baseErr := d.anchor.root.CanonicalPath()
		child, childErr := d.root.CanonicalPath()
		if baseErr != nil || childErr != nil || !strings.HasPrefix(child, base+string(filepath.Separator)) {
			return ErrStateUnsafe
		}
	}
	seen[d] = true
	return ctx.Err()
}

func checkProtectedHardlinks(ctx context.Context) error {
	// A separate UID without protected_hardlinks is not a sufficient boundary.
	// Read the actual kernel sysctl, never a caller-provided boolean.
	f, err := os.Open("/proc/sys/fs/protected_hardlinks")
	if err != nil {
		return ErrStateUnsupported
	}
	var st unix.Statfs_t
	statErr := unix.Fstatfs(int(f.Fd()), &st)
	raw, readErr := io.ReadAll(io.LimitReader(f, 4))
	closeErr := f.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || uint64(st.Type) != uint64(unix.PROC_SUPER_MAGIC) || string(raw) != "1\n" {
		return ErrStateUnsafe
	}
	return ctx.Err()
}

// Every directory above a state root, or including a control root, must be
// owned by initial-namespace root and unwritable by group/other. This is
// stronger than pinning one dirfd: an unprivileged account cannot rename the
// root by moving an ancestor. Extended ACL write masks appear in group bits.
// Unknown/cross-mount topology and non-ext-family storage fail closed. Positive
// filesystem evidence currently covers ext4 only, not every ext-family setup.
func rootManagedAncestors(ctx context.Context, root *paths.Root, includeRoot bool) (resultErr error) {
	if root == nil || ctx == nil || root.Verify() != nil {
		return ErrStateUnsafe
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return ErrStateUnsafe
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	var fs unix.Statfs_t
	if unix.Fstatfs(int(dir.Fd()), &fs) != nil || uint64(fs.Type) != uint64(unix.EXT4_SUPER_MAGIC) {
		return ErrStateUnsupported
	}
	for depth := 0; depth < 256; depth++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var current unix.Stat_t
		if unix.Fstat(int(dir.Fd()), &current) != nil || current.Mode&unix.S_IFMT != unix.S_IFDIR {
			return ErrStateUnsafe
		}
		if (includeRoot || depth > 0) && (current.Uid != 0 || current.Mode&0022 != 0) {
			return ErrStateUnsafe
		}
		fd, err := unix.Openat2(int(dir.Fd()), "..", &unix.OpenHow{
			Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
		})
		if err != nil {
			return ErrStateUnsafe
		}
		next := os.NewFile(uintptr(fd), "state-domain-parent")
		var parent unix.Stat_t
		err = unix.Fstat(fd, &parent)
		if err != nil {
			return errors.Join(ErrStateUnsafe, next.Close())
		}
		if parent.Dev == current.Dev && parent.Ino == current.Ino {
			if err := next.Close(); err != nil {
				return err
			}
			if current.Uid != 0 || current.Mode&0022 != 0 || root.Verify() != nil {
				return ErrStateUnsafe
			}
			return ctx.Err()
		}
		if err := dir.Close(); err != nil {
			return errors.Join(err, next.Close())
		}
		dir = next
	}
	return ErrStateLimit
}

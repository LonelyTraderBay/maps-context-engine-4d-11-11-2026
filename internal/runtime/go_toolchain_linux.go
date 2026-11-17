//go:build linux

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// GoToolchainProof attests a complete administrator-controlled distribution.
// It does not approve an executable, profile or target. The native owner must
// match Digest against the approved profile and separately pin bin/go and LSP.
// The administrator and kernel are trusted; a privileged snapshot rollback is
// outside this proof. No authorization/epoch or workspace truth is cached.
type GoToolchainProof struct {
	mu      sync.Mutex
	root    *paths.Root
	anchor  *os.File
	entries []toolchainEntry
	digest  string
	closed  bool
}

type toolchainEntry struct {
	name string
	stat unix.Stat_t
	sum  [32]byte
}

const toolchainEntries = 50000
const toolchainBytes int64 = 2 << 30

// CheckPrivateToolUmask observes kernel state without changing the global
// mask. Embedded hosts must establish 077 before starting concurrent handlers;
// the standalone CLI does so at startup. Child tool caches otherwise violate
// the protected worktree's owner-only storage contract.
func CheckPrivateToolUmask(ctx context.Context) error {
	if ctx == nil {
		return ErrGoToolchain
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return ErrGoToolchain
	}
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	closeErr := file.Close()
	if err != nil || closeErr != nil || len(raw) > 16384 {
		return ErrGoToolchain
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "Umask:") {
			value, e := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "Umask:")), 8, 32)
			if e != nil || value != 0077 {
				return ErrGoToolchain
			}
			return ctx.Err()
		}
	}
	return ErrGoToolchain
}

// CheckAdministratorPath rejects symlinks and any non-root owner or writable
// group/other ancestor. It grants no authority to execute the named file.
func CheckAdministratorPath(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return ErrGoToolchain
	}
	for current := name; ; current = filepath.Dir(current) {
		var st unix.Stat_t
		if unix.Lstat(current, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&06000 != 0 || st.Mode&unix.S_IFMT == unix.S_IFLNK {
			return ErrGoToolchain
		}
		if current == "/" {
			return nil
		}
	}
}

// InspectGoToolchain reads/hashes the complete existing tree without executing
// anything. Every file must be root-managed, single-link and on the root's
// filesystem. Callers must bound the context and keep the returned proof alive.
func InspectGoToolchain(ctx context.Context, name string) (_ *GoToolchainProof, resultErr error) {
	if ctx == nil {
		return nil, ErrGoToolchain
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrGoToolchain
	}
	if err := CheckAdministratorPath(name); err != nil {
		return nil, err
	}
	root, err := paths.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	p := &GoToolchainProof{root: root, anchor: anchor}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, p.Close())
		}
	}()
	var total int64
	buffer := make([]byte, 64<<10)
	var walk func(string, int) error
	walk = func(relative string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 || len(p.entries) >= toolchainEntries || len(relative) > 16384 {
			return ErrGoToolchain
		}
		var before unix.Stat_t
		if unix.Fstatat(int(anchor.Fd()), relative, &before, unix.AT_SYMLINK_NOFOLLOW) != nil || before.Uid != 0 || before.Mode&0022 != 0 || before.Mode&06000 != 0 {
			return ErrGoToolchain
		}
		kind := before.Mode & unix.S_IFMT
		if kind != unix.S_IFDIR && kind != unix.S_IFREG || kind == unix.S_IFREG && before.Nlink != 1 {
			return ErrGoToolchain
		}
		entry := toolchainEntry{name: relative, stat: before}
		if kind == unix.S_IFDIR {
			fd, e := unix.Openat2(int(anchor.Fd()), relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
			if e != nil {
				return ErrGoToolchain
			}
			dir := os.NewFile(uintptr(fd), "approved-toolchain-directory")
			names, e := dir.Readdirnames(toolchainEntries + 1)
			closeErr := dir.Close()
			if e != nil && !errors.Is(e, io.EOF) || closeErr != nil || len(names) > toolchainEntries {
				return ErrGoToolchain
			}
			sort.Strings(names)
			p.entries = append(p.entries, entry)
			for _, leaf := range names {
				if leaf == "." || leaf == ".." || strings.ContainsAny(leaf, "/\\\x00\r\n") {
					return ErrGoToolchain
				}
				child := leaf
				if relative != "." {
					child = relative + "/" + leaf
				}
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		} else {
			if before.Size < 0 || before.Size > 128<<20 || before.Size > toolchainBytes-total {
				return ErrGoToolchain
			}
			fd, e := unix.Openat2(int(anchor.Fd()), relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
			if e != nil {
				return ErrGoToolchain
			}
			file := os.NewFile(uintptr(fd), "approved-toolchain-file")
			h := sha256.New()
			count, e := copyToolchainFile(ctx, h, file, before.Size, buffer)
			var after unix.Stat_t
			statErr := unix.Fstat(fd, &after)
			closeErr := file.Close()
			if e != nil || statErr != nil || closeErr != nil || count != before.Size || !sameToolchainStat(before, after) {
				return ErrGoToolchain
			}
			copy(entry.sum[:], h.Sum(nil))
			total += count
			p.entries = append(p.entries, entry)
		}
		var after unix.Stat_t
		if unix.Fstatat(int(anchor.Fd()), relative, &after, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameToolchainStat(before, after) {
			return ErrGoToolchain
		}
		return nil
	}
	if err := walk(".", 0); err != nil {
		return nil, err
	}
	sort.Slice(p.entries, func(i, j int) bool { return p.entries[i].name < p.entries[j].name })
	h := sha256.New()
	_, _ = h.Write([]byte("MCE4D-NATIVE-GO-TOOLCHAIN1\x00"))
	for _, entry := range p.entries {
		writeToolchainInt(h, uint64(len(entry.name)))
		_, _ = h.Write([]byte(entry.name))
		kind := byte('f')
		size := entry.stat.Size
		if entry.stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			kind = 'd'
			size = 0
		}
		_, _ = h.Write([]byte{kind})
		writeToolchainInt(h, uint64(entry.stat.Mode&0777))
		writeToolchainInt(h, uint64(size))
		_, _ = h.Write(entry.sum[:])
	}
	p.digest = hex.EncodeToString(h.Sum(nil))
	if err := p.Check(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func writeToolchainInt(h hash.Hash, v uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], v)
	_, _ = h.Write(raw[:])
}

func copyToolchainFile(ctx context.Context, h hash.Hash, file *os.File, size int64, buffer []byte) (int64, error) {
	var total int64
	for total < size {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		limit := int64(len(buffer))
		if limit > size-total {
			limit = size - total
		}
		n, err := file.Read(buffer[:int(limit)])
		if n > 0 {
			_, _ = h.Write(buffer[:n])
			total += int64(n)
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
	return total, ctx.Err()
}

func sameToolchainStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// Check validates every pinned inode/change stamp, including all directory
// membership stamps. This relies on root-controlled, non-client-writable paths;
// it is not a metadata-only proof for an attacker-writable distribution.
func (p *GoToolchainProof) Check(ctx context.Context) error {
	if p == nil || ctx == nil {
		return ErrGoToolchain
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.anchor == nil || p.root.Verify() != nil {
		return ErrGoToolchain
	}
	name, err := p.root.CanonicalPath()
	if err != nil || CheckAdministratorPath(name) != nil {
		return ErrGoToolchain
	}
	for _, entry := range p.entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		var observed unix.Stat_t
		if unix.Fstatat(int(p.anchor.Fd()), entry.name, &observed, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameToolchainStat(entry.stat, observed) {
			return ErrGoToolchain
		}
	}
	return p.root.Verify()
}

func (p *GoToolchainProof) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}

// CheckPath is for each stdlib parse inside a load bracketed by full Check
// calls. It verifies the exact known path through a confined descriptor; it
// does not replace the full before/after toolchain check.
func (p *GoToolchainProof) CheckPath(ctx context.Context, relative string) error {
	if p == nil || ctx == nil {
		return ErrGoToolchain
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.root.Verify() != nil {
		return ErrGoToolchain
	}
	i := sort.Search(len(p.entries), func(i int) bool { return p.entries[i].name >= relative })
	if i == len(p.entries) || p.entries[i].name != relative {
		return ErrGoToolchain
	}
	fd, err := unix.Openat2(int(p.anchor.Fd()), relative, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return ErrGoToolchain
	}
	defer unix.Close(fd)
	var observed unix.Stat_t
	if unix.Fstat(fd, &observed) != nil || !sameToolchainStat(p.entries[i].stat, observed) {
		return ErrGoToolchain
	}
	return ctx.Err()
}

// StandardPackages derives a bounded allowlist from the already verified Go
// source tree. NewTrustedStdlib still verifies every parsed body and complete
// dependency graph; this list alone never supplies typed closure authority.
func (p *GoToolchainProof) StandardPackages() []string {
	if p == nil {
		return nil
	}
	names := map[string]bool{}
	for _, entry := range p.entries {
		name := entry.name
		if !strings.HasPrefix(name, "src/") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.Contains(name, "/testdata/") {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(name, "src/")))
		if dir != "." && !strings.HasPrefix(dir, "cmd/") && dir != "cmd" {
			names[dir] = true
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (p *GoToolchainProof) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return errors.Join(p.anchor.Close(), p.root.Close())
}

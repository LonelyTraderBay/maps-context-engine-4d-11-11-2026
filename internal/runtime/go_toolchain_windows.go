package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

type GoToolchainProof struct {
	mu      sync.Mutex
	root    *paths.Root
	entries map[string]windowsToolchainEntry
	names   []string
	digest  string
	closed  bool
}

type windowsToolchainEntry struct {
	file      windows.ByHandleFileInformation
	basic     windowsFileBasicInfo
	digest    [32]byte
	directory bool
}

type windowsFileBasicInfo struct {
	created, accessed, written, changed int64
	attributes, reserved                uint32
}

// Windows uses protected ACLs for private tool output rather than an umask.
func CheckPrivateToolUmask(context.Context) error { return ErrGoToolchain }

func CheckAdministratorPath(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return ErrGoToolchain
	}
	parent := filepath.Dir(name)
	if parent == name {
		return checkWindowsToolchainDirectory(name)
	}
	dirErr := checkWindowsToolchainDirectory(name)
	if errors.Is(dirErr, paths.ErrNotRegular) {
		root, err := paths.OpenRoot(parent)
		if err != nil {
			return err
		}
		file, err := root.OpenRegular(filepath.Base(name))
		if err == nil {
			err = errors.Join(checkWindowsToolchainACL(file, false), file.Close())
		}
		dirErr = errors.Join(err, root.Close())
	}
	if dirErr != nil {
		return errors.Join(ErrGoToolchain, dirErr)
	}
	for {
		if err := checkWindowsToolchainDirectory(parent); err != nil {
			return errors.Join(ErrGoToolchain, err)
		}
		if next := filepath.Dir(parent); next != parent {
			parent = next
		} else {
			return nil
		}
	}
}

func checkWindowsToolchainDirectory(name string) (resultErr error) {
	root, err := paths.OpenRoot(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	handle, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	return errors.Join(checkWindowsToolchainACL(handle, filepath.Dir(name) == name), handle.Close())
}

func checkWindowsToolchainACL(file *os.File, volumeRoot bool) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || !windowsToolchainAdministrator(owner) {
		return errors.Join(ErrGoToolchain, err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return errors.Join(ErrGoToolchain, err)
	}
	const fileDeleteChild = 0x0040
	mutation := uint32(windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
		windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | fileDeleteChild | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER)
	if volumeRoot {
		// The pinned installation ancestors prevent name substitution.
		// The system volume may allow creation of unrelated top-level dirs.
		mutation &^= windows.FILE_APPEND_DATA
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace == nil {
			return ErrGoToolchain
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return ErrGoToolchain
		}
		if uint32(ace.Mask)&mutation != 0 && !windowsToolchainAdministrator((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return ErrGoToolchain
		}
	}
	return nil
}

func windowsToolchainAdministrator(sid *windows.SID) bool {
	if sid == nil {
		return false
	}
	return sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) ||
		sid.String() == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
}

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
	proof := &GoToolchainProof{root: root, entries: map[string]windowsToolchainEntry{}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, root.Close())
		}
	}()
	capture := windowsToolchainScan{proof: proof, remaining: 2 << 30, buffer: make([]byte, 64<<10)}
	if err := capture.walk(ctx, root, ".", 0); err != nil {
		return nil, err
	}
	for _, name := range []string{"VERSION", "bin/go.exe", "src", "pkg/tool/windows_amd64"} {
		if _, exists := proof.entries[name]; !exists {
			return nil, ErrGoToolchain
		}
	}
	sort.Strings(proof.names)
	digest := sha256.New()
	_, _ = digest.Write([]byte("MCE4D-WINDOWS-GO-TOOLCHAIN1\x00"))
	for _, name := range proof.names {
		entry := proof.entries[name]
		_, _ = fmt.Fprintf(digest, "%d:%s:%t:%x\n", len(name), name, entry.directory, entry.digest)
	}
	proof.digest = hex.EncodeToString(digest.Sum(nil))
	return proof, errors.Join(ctx.Err(), root.Verify())
}

type windowsToolchainScan struct {
	proof     *GoToolchainProof
	remaining int64
	buffer    []byte
}

func (s *windowsToolchainScan) walk(ctx context.Context, root *paths.Root, name string, depth int) (resultErr error) {
	if depth > 64 || len(s.proof.names) >= 50000 || len(name) > 4096 {
		return ErrGoToolchain
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.Close()) }()
	before, err := windowsToolchainStat(handle, true)
	if err != nil {
		return err
	}
	s.proof.entries[name] = before
	s.proof.names = append(s.proof.names, name)
	children, err := handle.ReadDir(50001)
	if err != nil && !errors.Is(err, io.EOF) || len(children) > 50000 {
		return errors.Join(ErrGoToolchain, err)
	}
	for _, item := range children {
		relative := item.Name()
		if name != "." {
			relative = name + "/" + relative
		}
		if item.IsDir() {
			child, err := root.OpenDirectory(item.Name())
			if err != nil {
				return err
			}
			if err := errors.Join(s.walk(ctx, child, relative, depth+1), child.Close()); err != nil {
				return err
			}
		} else if err := s.hashFile(ctx, root, item.Name(), relative); err != nil {
			return err
		}
	}
	now, err := windowsToolchainStat(handle, true)
	if err != nil || before != now {
		return errors.Join(ErrGoToolchain, err)
	}
	return root.Verify()
}

func (s *windowsToolchainScan) hashFile(ctx context.Context, root *paths.Root, name, relative string) (resultErr error) {
	if len(s.proof.names) >= 50000 || len(relative) > 4096 {
		return ErrGoToolchain
	}
	file, err := root.OpenRegular(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	before, err := windowsToolchainStat(file, false)
	if err != nil {
		return err
	}
	size := int64(before.file.FileSizeHigh)<<32 | int64(before.file.FileSizeLow)
	if size < 0 || size > 128<<20 || size > s.remaining {
		return ErrGoToolchain
	}
	digest := sha256.New()
	reader := io.NewSectionReader(file, 0, size)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(s.buffer)
		read += int64(n)
		_, _ = digest.Write(s.buffer[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	after, err := windowsToolchainStat(file, false)
	if err != nil || before != after || read != size {
		return errors.Join(ErrGoToolchain, err)
	}
	copy(before.digest[:], digest.Sum(nil))
	s.remaining -= size
	s.proof.entries[relative] = before
	s.proof.names = append(s.proof.names, relative)
	return nil
}

func windowsToolchainStat(file *os.File, directory bool) (entry windowsToolchainEntry, resultErr error) {
	handle := windows.Handle(file.Fd())
	entry.directory = directory
	if err := errors.Join(windows.GetFileInformationByHandle(handle, &entry.file),
		windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&entry.basic)),
			uint32(unsafe.Sizeof(entry.basic))), checkWindowsToolchainACL(file, false)); err != nil {
		return entry, err
	}
	entry.file.LastAccessTime = windows.Filetime{}
	entry.basic.accessed = 0
	return entry, nil
}

func (p *GoToolchainProof) Check(ctx context.Context) error {
	if p == nil || ctx == nil {
		return ErrGoToolchain
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrGoToolchain
	}
	name, err := p.root.CanonicalPath()
	if err != nil {
		return err
	}
	if err := CheckAdministratorPath(name); err != nil {
		return err
	}
	for _, name := range p.names {
		if err := p.checkPath(ctx, name); err != nil {
			return err
		}
	}
	return p.root.Verify()
}

func (p *GoToolchainProof) CheckPath(ctx context.Context, name string) error {
	if p == nil || ctx == nil {
		return ErrGoToolchain
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.checkPath(ctx, name)
}

func (p *GoToolchainProof) checkPath(ctx context.Context, name string) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, exists := p.entries[name]
	if p.closed || !exists {
		return ErrGoToolchain
	}
	var file *os.File
	var err error
	if entry.directory {
		root := p.root
		if name != "." {
			root, err = p.root.OpenDirectory(name)
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
		}
		file, err = root.DirectoryHandle()
	} else {
		file, err = p.root.OpenRegular(name)
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	got, err := windowsToolchainStat(file, entry.directory)
	entry.digest = [32]byte{}
	if err != nil || got != entry {
		return errors.Join(ErrGoToolchain, err)
	}
	return nil
}

func (p *GoToolchainProof) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}

func (p *GoToolchainProof) StandardPackages() []string {
	if p == nil {
		return nil
	}
	names := map[string]struct{}{}
	for _, entry := range p.names {
		if relative, ok := strings.CutPrefix(entry, "src/"); ok && strings.HasSuffix(relative, ".go") &&
			!strings.HasSuffix(relative, "_test.go") && !strings.Contains(relative, "/testdata/") {
			dir := filepath.ToSlash(filepath.Dir(relative))
			if dir != "." && dir != "cmd" && !strings.HasPrefix(dir, "cmd/") {
				names[dir] = struct{}{}
			}
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
	return p.root.Close()
}

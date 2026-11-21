//go:build linux || windows

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// GoExecutable pins the initially authorized inode and detects replacement or
// changed bytes on each observation. The toolchain directory must also be
// trusted against mutation during use; this does not control exec.Command in
// another library or turn a PATH lookup into descriptor-based execution.
type GoExecutable struct {
	mu        sync.Mutex
	root      *paths.Root
	file      *os.File
	binPath   string
	expected  string
	maxBytes  int64
	authorize func(context.Context) error
}

func OpenGoExecutable(ctx context.Context, input GoExecutableInput) (result *GoExecutable, resultErr error) {
	if ctx == nil || input.BinRoot == nil || input.Authorize == nil || input.MaxBytes < 1 || input.MaxBytes > 128*1024*1024 || len(input.ExpectedSHA256) != 64 {
		return nil, ErrGoToolchain
	}
	if _, err := hex.DecodeString(input.ExpectedSHA256); err != nil || input.ExpectedSHA256 != stringLowerHex(input.ExpectedSHA256) {
		return nil, ErrGoToolchain
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrGoToolchain
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := input.Authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrGoToolchain
	}
	binPath, err := input.BinRoot.CanonicalPath()
	if err != nil {
		return nil, ErrGoToolchain
	}
	file, err := openGoFile(input.BinRoot)
	if err != nil {
		return nil, ErrGoToolchain
	}
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	if err := verifyGoFile(ctx, file, input.ExpectedSHA256, input.MaxBytes); err != nil {
		return nil, err
	}
	if err := input.BinRoot.Verify(); err != nil {
		return nil, ErrGoToolchain
	}
	if err := input.Authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrGoToolchain
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return &GoExecutable{root: input.BinRoot, file: file, binPath: binPath, expected: input.ExpectedSHA256, maxBytes: input.MaxBytes, authorize: input.Authorize}, nil
}

func stringLowerHex(value string) string {
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return ""
		}
	}
	return value
}

func verifyGoFile(ctx context.Context, file *os.File, expected string, maxBytes int64) error {
	before, err := goFileState(file)
	if err != nil || goFileSize(before) > maxBytes {
		return ErrGoToolchain
	}
	hash := sha256.New()
	reader := io.NewSectionReader(file, 0, goFileSize(before))
	buffer := make([]byte, 32*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			read += int64(n)
			_, _ = hash.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return ErrGoToolchain
		}
	}
	after, err := goFileState(file)
	if err != nil || read != goFileSize(before) || !sameGoFileState(before, after) || hex.EncodeToString(hash.Sum(nil)) != expected {
		return ErrGoToolchain
	}
	return ctx.Err()
}

// CheckForLoad requires the parent PATH itself to be the verified bin directory
// because x/tools/go/packages resolves "go" before applying Config.Env.
// It is a pre/post observation, not atomic execution or a same-user sandbox.
func (g *GoExecutable) CheckForLoad(ctx context.Context, goRoot string) (resultErr error) {
	if g == nil || ctx == nil {
		return ErrGoToolchain
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrGoToolchain
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.file == nil || g.root == nil || g.authorize == nil || !filepath.IsAbs(goRoot) || filepath.Clean(goRoot) != goRoot || filepath.Join(goRoot, "bin") != g.binPath {
		return ErrGoToolchain
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := g.authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrGoToolchain
	}
	if !goPathSafe(g.binPath) {
		return ErrGoToolchain
	}
	resolved, err := exec.LookPath("go")
	if err != nil || resolved != filepath.Join(g.binPath, goExecutableName) {
		return ErrGoToolchain
	}
	current, err := g.root.OpenRegular(goExecutableName)
	if err != nil {
		return ErrGoToolchain
	}
	defer func() { resultErr = errors.Join(resultErr, current.Close()) }()
	initialInfo, err := g.file.Stat()
	if err != nil {
		return ErrGoToolchain
	}
	currentInfo, err := current.Stat()
	if err != nil || !os.SameFile(initialInfo, currentInfo) {
		return ErrGoToolchain
	}
	if err := verifyGoFile(ctx, current, g.expected, g.maxBytes); err != nil {
		return err
	}
	if err := g.root.Verify(); err != nil {
		return ErrGoToolchain
	}
	if err := g.authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrGoToolchain
	}
	if !goPathSafe(g.binPath) {
		return ErrGoToolchain
	}
	return ctx.Err()
}

func (g *GoExecutable) Close() error {
	if g == nil {
		return ErrGoToolchain
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.file == nil {
		return ErrGoToolchain
	}
	err := g.file.Close()
	g.file = nil
	return err
}

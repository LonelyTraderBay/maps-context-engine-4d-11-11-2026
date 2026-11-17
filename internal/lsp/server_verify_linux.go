//go:build linux

package lsp

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func verifyServerBinding(ctx context.Context, root *paths.Root, original *os.File, expected string) (resultErr error) {
	if root == nil || original == nil || root.Verify() != nil {
		return ErrLSPAuthority
	}
	current, err := root.OpenRegular("gopls")
	if err != nil {
		return ErrLSPAuthority
	}
	defer func() { resultErr = errors.Join(resultErr, current.Close()) }()
	a, err := original.Stat()
	if err != nil {
		return ErrLSPAuthority
	}
	b, err := current.Stat()
	if err != nil || !os.SameFile(a, b) || verifyGoplsFile(ctx, current, expected) != nil || root.Verify() != nil {
		return ErrLSPAuthority
	}
	return ctx.Err()
}

// verifyGoplsFile pins a reviewed build graph and the exact caller-allowlisted
// bytes. It is a pre/post observation, not a sandbox against same-user writes.
func verifyGoplsFile(ctx context.Context, file *os.File, expected string) error {
	if ctx == nil || file == nil || !resultHash(expected) {
		return ErrLSPAuthority
	}
	stat := func() (unix.Stat_t, error) {
		var s unix.Stat_t
		err := unix.Fstat(int(file.Fd()), &s)
		if err != nil || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Nlink != 1 || s.Mode&0111 == 0 || s.Mode&0022 != 0 || s.Size < 1 || s.Size > 128*1024*1024 || s.Uid != uint32(unix.Geteuid()) && s.Uid != 0 {
			return s, ErrLSPAuthority
		}
		return s, nil
	}
	before, err := stat()
	if err != nil {
		return err
	}
	hash := sha256.New()
	reader := io.NewSectionReader(file, 0, before.Size)
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return ErrLSPAuthority
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return ErrLSPAuthority
	}
	info, err := buildinfo.Read(file)
	if err != nil || info.GoVersion != "go1.27.4" || info.Path != "golang.org/x/tools/gopls" || info.Main.Path != "golang.org/x/tools/gopls" || info.Main.Version != "v0.24.0" || info.Main.Sum != "h1:nPO/LffYa4sP62Ueuema5PPnZPGaZjGl+vZGvbTek4g=" {
		return ErrLSPAuthority
	}
	asm := false
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/segmentio/asm" {
			asm = dependency.Version == "v1.2.1" && dependency.Replace != nil && dependency.Replace.Path == "github.com/segmentio/asm" && dependency.Replace.Version == "v1.1.3" && dependency.Replace.Sum == "h1:WM03sfUOENvvKexOLp+pCqgb/WDjsi7EK8gIsICtzhc="
		}
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if !asm || settings["GOOS"] != "linux" || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" {
		return ErrLSPAuthority
	}
	after, err := stat()
	if err != nil || before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return ErrLSPAuthority
	}
	return ctx.Err()
}

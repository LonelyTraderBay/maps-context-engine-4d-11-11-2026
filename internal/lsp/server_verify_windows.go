package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

// VerifyWindowsServer checks the pinned build and digest without a launch.
func VerifyWindowsServer(ctx context.Context, root *paths.Root, expected string) error {
	file, err := openSessionServer(root)
	if err != nil {
		return err
	}
	return errors.Join(verifyServerBinding(ctx, root, file, expected), file.Close())
}

func openSessionServer(root *paths.Root) (*os.File, error) {
	name, err := root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(filepath.Join(name, "gopls.exe"))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), "pinned-gopls-server")
	if err := errors.Join(paths.VerifyRegularFile(file), root.Verify()); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func verifyServerBinding(ctx context.Context, root *paths.Root, original *os.File, expected string) (resultErr error) {
	if ctx == nil || root == nil || original == nil || !resultHash(expected) || root.Verify() != nil {
		return ErrLSPAuthority
	}
	current, err := root.OpenRegular("gopls.exe")
	if err != nil {
		return errors.Join(ErrLSPAuthority, err)
	}
	defer func() { resultErr = errors.Join(resultErr, current.Close()) }()
	before, err := original.Stat()
	if err != nil || before.Size() < 1 || before.Size() > 128<<20 {
		return errors.Join(ErrLSPAuthority, err)
	}
	observed, err := current.Stat()
	if err != nil || !os.SameFile(before, observed) {
		return errors.Join(ErrLSPAuthority, err)
	}
	hash := sha256.New()
	reader := io.NewSectionReader(original, 0, before.Size())
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buffer)
		_, _ = hash.Write(buffer[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Join(ErrLSPAuthority, err)
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return ErrLSPAuthority
	}
	if err := errors.Join(verifyGoplsBuild(original, "windows"), paths.VerifyRegularFile(original), root.Verify()); err != nil {
		return errors.Join(ErrLSPAuthority, err)
	}
	return ctx.Err()
}

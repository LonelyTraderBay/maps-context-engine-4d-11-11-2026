package providers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

func verifyWindowsMaterial(ctx context.Context, root *paths.Root, name, digest string, limit int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file, err := root.OpenRegular(name)
	if err != nil {
		return 0, errors.Join(ErrAuthority, err)
	}
	hash := sha256.New()
	n, readErr := io.Copy(hash, io.LimitReader(file, limit+1))
	if err := errors.Join(readErr, file.Close()); err != nil || n > limit || hex.EncodeToString(hash.Sum(nil)) != digest {
		return 0, errors.Join(ErrAuthority, err)
	}
	return n, root.Verify()
}

func VerifyWindowsMaterialTree(ctx context.Context, root *paths.Root, files map[string]string, remaining int64) error {
	name, err := root.CanonicalPath()
	if err != nil {
		return err
	}
	expected := make(map[string]bool, len(files))
	for leaf, digest := range files {
		if path.Clean(leaf) != leaf || remaining < 0 {
			return ErrAuthority
		}
		n, err := verifyWindowsMaterial(ctx, root, leaf, digest, remaining)
		if err != nil {
			return err
		}
		expected[leaf] = true
		remaining -= n
	}
	return verifyMembers(ctx, root, name, expected)
}

// RunWindowsProvider runs only an installation-owned program over an already
// materialized capture. Callers verify toolchain pins and protect both roots.
func RunWindowsProvider(ctx context.Context, capture *paths.Root, input ports.ProviderJob, child runtime.WindowsJobChild,
	sink ports.ProviderSink) (done model.ProviderDone, resultErr error) {
	manifest := input.Start.ManifestPath
	if ctx == nil || capture == nil || sink == nil || input.Recheck == nil || input.Capture == nil ||
		!filepath.IsAbs(manifest) || filepath.Clean(manifest) != manifest || filepath.Dir(manifest) != input.Start.SnapshotRoot {
		return done, ErrAuthority
	}
	raw, err := json.Marshal(&input.Start)
	if err != nil || len(raw) > 65536 {
		return done, ErrProtocol
	}
	if _, err := model.DecodeProviderStartJSON(raw, int(input.Start.MaxFrameBytes)); err != nil {
		return done, ErrProtocol
	}
	wire, err := encodeFrame(&input.Start, int(input.Start.MaxFrameBytes))
	if err != nil {
		return done, ErrProtocol
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return done, ErrProtocol
	}
	if err := errors.Join(verifyCapture(ctx, capture, input), input.Recheck(ctx)); err != nil {
		return done, err
	}
	system, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return done, err
	}
	child.Environment = append(child.Environment, "SystemRoot="+system, "WINDIR="+system)
	inR, inW, err := os.Pipe()
	if err != nil {
		return done, err
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		return done, err
	}
	defer outR.Close()
	defer outW.Close()
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return done, err
	}
	defer stderr.Close()
	job, err := runtime.NewWindowsProviderJob()
	if err != nil {
		return done, err
	}
	defer func() { resultErr = errors.Join(resultErr, job.Close()) }()
	child.Stdin, child.Stdout, child.Stderr = inR, outW, stderr
	process, err := job.StartChild(ctx, child, func(process windows.Handle) error {
		name := make([]uint16, 32768)
		n := uint32(len(name))
		if err := windows.QueryFullProcessImageName(process, 0, &name[0], &n); err != nil ||
			!strings.EqualFold(windows.UTF16ToString(name[:n]), child.Executable) {
			return errors.Join(ErrAuthority, err)
		}
		return input.Recheck(ctx)
	})
	if err != nil {
		return done, errors.Join(ErrProcess, err)
	}
	defer windows.CloseHandle(process)
	_ = inR.Close()
	_ = outW.Close()
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = job.Terminate(74)
		_ = inW.Close()
		_ = outR.Close()
		close(stopped)
	})
	defer func() {
		if !stop() {
			<-stopped
		}
		resultErr = errors.Join(resultErr, ctx.Err())
	}()
	if _, err := inW.Write(wire); err != nil {
		return done, errors.Join(ErrProcess, err)
	}
	if err := inW.Close(); err != nil {
		return done, err
	}
	reader := bufio.NewReaderSize(outR, 8192)
	done, err = readProviderStream(ctx, reader, input, sink)
	if err != nil {
		return model.ProviderDone{}, err
	}
	if _, err := reader.Peek(1); !errors.Is(err, io.EOF) {
		return model.ProviderDone{}, ErrProtocol
	}
	if err := job.WaitEmpty(ctx); err != nil {
		return model.ProviderDone{}, err
	}
	var exit uint32
	if err := windows.GetExitCodeProcess(process, &exit); err != nil || exit != 0 {
		return model.ProviderDone{}, errors.Join(ErrProcess, err)
	}
	return done, errors.Join(verifyCapture(ctx, capture, input), input.Recheck(ctx))
}

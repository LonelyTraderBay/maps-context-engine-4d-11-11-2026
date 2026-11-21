//go:build linux

package providers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/unix"
)

func (p *PythonProvider) Analyze(parent context.Context, job ports.ProviderJob, sink ports.ProviderSink) (model.ProviderDone, error) {
	var done model.ProviderDone
	if p == nil || parent == nil || job.Recheck == nil || job.Capture == nil || sink == nil {
		return done, ErrAuthority
	}
	cfg := p.config
	body, err := json.Marshal(&job.Start)
	if err != nil || len(body) > 65536 {
		return done, ErrProtocol
	}
	job.Start, err = model.DecodeProviderStartJSON(body, int(job.Start.MaxFrameBytes))
	if err != nil {
		return done, ErrProtocol
	}
	profile, ok := job.Start.Profile.(*model.PythonProfile)
	if !ok || job.Start.ProviderID != cfg.ProviderID || profile.ProviderDigest != cfg.ProviderDigest ||
		profile.InterpreterDigest != cfg.InterpreterDigest || profile.StdlibDigest != cfg.StdlibDigest ||
		profile.PythonVersion != cfg.Version || profile.AnalysisMode != cfg.AnalysisMode {
		return done, ErrAuthority
	}
	deadline, err := time.Parse(time.RFC3339Nano, job.Start.DeadlineAt)
	if err != nil {
		return done, ErrProtocol
	}
	ctx, cancel := context.WithDeadline(parent, minTime(deadline, time.Now().Add(time.Minute)))
	defer cancel()
	root, err := verifyCapturedJob(ctx, cfg.ScratchParent, job)
	if err != nil {
		return done, err
	}
	defer root.Close()
	interpreter, err := openPinned(cfg.Interpreter, cfg.InterpreterDigest, maxNodeBytes, true)
	if err != nil {
		return done, err
	}
	defer interpreter.Close()
	program, err := sealProviderProgram(cfg.Program, cfg.ProviderDigest)
	if err != nil {
		return done, err
	}
	defer program.Close()
	verify := func(ctx context.Context) error {
		digest, err := PythonStdlibDigest(ctx, cfg.Stdlib)
		if err != nil {
			return err
		}
		if digest != cfg.StdlibDigest {
			return ErrAuthority
		}
		return job.Recheck(ctx)
	}
	if err := verify(ctx); err != nil {
		return done, err
	}
	args := []string{"-I", "-S", "-B", "/proc/self/fd/4"}
	env := []string{"LC_ALL=C", "TZ=UTC", "MCE4D_PYTHON_STDLIB=" + cfg.Stdlib}
	err = runFramedWorker(ctx, root, interpreter, program, job, args, env, verify,
		func(ctx context.Context, reader *bufio.Reader) error {
			done, err = readProviderStream(ctx, reader, job, sink)
			return err
		})
	if err != nil {
		return model.ProviderDone{}, err
	}
	return done, nil
}

// PythonStdlibDigest binds a root-owned, locally installed standard library.
// The worker checks the interpreter's import path and uses only stdlib modules.
// No environment, user site, or target project libraries are part of this pin.
func PythonStdlibDigest(ctx context.Context, name string) (string, error) {
	if ctx == nil || !filepath.IsAbs(name) || filepath.Clean(name) != name || !strings.HasPrefix(filepath.Base(name), "python3.") {
		return "", ErrAuthority
	}
	zip := filepath.Join(filepath.Dir(name), strings.ReplaceAll(filepath.Base(name), ".", "")+".zip")
	if _, err := os.Lstat(zip); !os.IsNotExist(err) {
		return "", ErrAuthority
	}
	for current := name; ; current = filepath.Dir(current) {
		var stat unix.Stat_t
		if err := unix.Lstat(current, &stat); err != nil || stat.Uid != 0 ||
			stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
			return "", ErrAuthority
		}
		if current == "/" {
			break
		}
	}
	root, err := paths.OpenRoot(name)
	if err != nil {
		return "", ErrAuthority
	}
	defer root.Close()
	h := sha256.New()
	_, _ = io.WriteString(h, "MCE4D-PYTHON-STDLIB1\x00")
	var files int
	var bytes int64
	err = filepath.WalkDir(name, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return ErrAuthority
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		files++
		if files > 8192 {
			return ErrBudget
		}
		local, err := filepath.Rel(name, path)
		if err != nil {
			return ErrAuthority
		}
		var stat unix.Stat_t
		if unix.Lstat(path, &stat) != nil || stat.Uid != 0 || stat.Mode&0022 != 0 && entry.Type()&os.ModeSymlink == 0 {
			return ErrAuthority
		}
		switch {
		case entry.IsDir():
			fmt.Fprintf(h, "%s\x00D\x00", local)
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil || len(link) > 8192 {
				return ErrAuthority
			}
			fmt.Fprintf(h, "%s\x00L\x00%s\x00", local, link)
		case entry.Type().IsRegular():
			if stat.Size < 0 || stat.Size > (128<<20)-bytes {
				return ErrBudget
			}
			file, err := root.OpenRegular(filepath.ToSlash(local))
			if err != nil {
				return ErrAuthority
			}
			sum := sha256.New()
			n, readErr := io.Copy(sum, io.LimitReader(file, stat.Size+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil || n != stat.Size {
				return ErrAuthority
			}
			fmt.Fprintf(h, "%s\x00F\x00%x\x00", local, sum.Sum(nil))
			bytes += n
		default:
			return ErrAuthority
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if root.Verify() != nil {
		return "", ErrAuthority
	}
	return hex.EncodeToString(h.Sum(nil)), ctx.Err()
}

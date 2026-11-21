//go:build linux

package providers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/unix"
)

const (
	maxManifest     = 16 << 20
	maxStderr       = 1 << 20
	maxNodeBytes    = 128 << 20
	maxProgramBytes = 4 << 20
)

func (p *NodeProvider) Analyze(parent context.Context, job ports.ProviderJob, sink ports.ProviderSink) (model.ProviderDone, error) {
	if sink == nil {
		return model.ProviderDone{}, ErrAuthority
	}
	var done model.ProviderDone
	err := p.withNode(parent, job, func(ctx context.Context, root, compiler *paths.Root, node, worker *os.File, verified ports.ProviderJob) error {
		return runFramedNode(ctx, root, compiler, node, worker, verified, "", func(ctx context.Context, reader *bufio.Reader) error {
			var err error
			done, err = readProviderStream(ctx, reader, verified, sink)
			return err
		})
	})
	if err != nil {
		return model.ProviderDone{}, err
	}
	return done, nil
}

// AnalyzeFrontend reads one bounded addon response from an embedded worker.
// The caller returns an error for an invalid response, including scope mismatch.
func (p *NodeProvider) AnalyzeFrontend(parent context.Context, job ports.ProviderJob, accept func(context.Context, ports.ProviderJob, []byte) error) error {
	if p == nil || len(p.config.EmbeddedWorker) == 0 || p.config.Compiler == (CompilerInstallation{}) || accept == nil {
		return ErrAuthority
	}
	return p.withNode(parent, job, func(ctx context.Context, root, compiler *paths.Root, node, worker *os.File, verified ports.ProviderJob) error {
		return runFramedNode(ctx, root, compiler, node, worker, verified, "frontend", func(ctx context.Context, reader *bufio.Reader) error {
			body, err := readFrame(reader, int(verified.Start.MaxFrameBytes))
			if err != nil {
				return err
			}
			return accept(ctx, verified, body)
		})
	})
}

func (p *NodeProvider) withNode(parent context.Context, job ports.ProviderJob,
	run func(context.Context, *paths.Root, *paths.Root, *os.File, *os.File, ports.ProviderJob) error) error {
	if p == nil || parent == nil || job.Recheck == nil || job.Capture == nil {
		return ErrAuthority
	}
	start := job.Start
	body, err := json.Marshal(&start)
	if err != nil || len(body) > 65536 {
		return ErrProtocol
	}
	start, err = model.DecodeProviderStartJSON(body, int(start.MaxFrameBytes))
	if err != nil {
		return ErrProtocol
	}
	job.Start = start
	ts, ok := start.Profile.(*model.TSProfile)
	if !ok || start.ProviderID != p.config.ProviderID || start.ProviderDigest != p.config.ProviderDigest ||
		ts.NodeDigest != p.config.NodeDigest || ts.CompilerDigest != p.config.CompilerDigest {
		return ErrAuthority
	}
	for _, config := range ts.TsconfigPaths {
		if _, err := job.Capture.CheckCapturedRange(config, model.ByteRange{}); err != nil {
			return ErrAuthority
		}
	}
	deadline, err := time.Parse(time.RFC3339Nano, start.DeadlineAt)
	if err != nil {
		return ErrProtocol
	}
	ctx, stop := context.WithDeadline(parent, minTime(deadline, time.Now().Add(time.Minute)))
	defer stop()
	root, err := verifyCapturedJob(ctx, p.config.ScratchParent, job)
	if err != nil {
		return err
	}
	defer root.Close()
	node, err := openPinned(p.config.NodePath, p.config.NodeDigest, maxNodeBytes, true)
	if err != nil {
		return err
	}
	defer node.Close()
	worker, err := p.sealWorker()
	if err != nil {
		return err
	}
	defer worker.Close()
	compiler, err := p.verifyCompiler(ctx)
	if err != nil {
		return err
	}
	if compiler != nil {
		defer compiler.Close()
	}
	if err := job.Recheck(ctx); err != nil {
		return ErrAuthority
	}
	return run(ctx, root, compiler, node, worker, job)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func verifyCapturedJob(ctx context.Context, scratchParent string, job ports.ProviderJob) (*paths.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := job.Recheck(ctx); err != nil {
		return nil, ErrAuthority
	}
	path := job.Start.SnapshotRoot
	if filepath.Dir(path) != scratchParent || strings.ContainsAny(path, "*,") ||
		filepath.Dir(job.Start.ManifestPath) != path || filepath.Clean(job.Start.ManifestPath) != job.Start.ManifestPath {
		return nil, ErrAuthority
	}
	parent, err := paths.OpenRoot(scratchParent)
	if err != nil {
		return nil, ErrAuthority
	}
	defer parent.Close()
	root, err := parent.OpenDirectory(filepath.Base(path))
	if err != nil {
		return nil, ErrAuthority
	}
	if err := verifyCapture(ctx, root, job); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func verifyCapture(ctx context.Context, root *paths.Root, job ports.ProviderJob) error {
	name, err := root.CanonicalPath()
	if err != nil || name != job.Start.SnapshotRoot {
		return ErrAuthority
	}
	manifestName := filepath.Base(job.Start.ManifestPath)
	manifest, err := root.OpenRegular(manifestName)
	if err != nil {
		return ErrAuthority
	}
	raw, readErr := io.ReadAll(io.LimitReader(manifest, maxManifest+1))
	closeErr := manifest.Close()
	if readErr != nil || closeErr != nil || len(raw) > maxManifest || hexDigest(raw) != job.Start.ManifestDigest {
		return ErrAuthority
	}
	captured := job.Capture.Manifest()
	parsed, err := model.DecodeCaptureManifestJSON(raw, maxManifest)
	if err != nil || model.VerifyCaptureDigest(parsed, maxManifest) != nil ||
		parsed.SnapshotDigest != captured.SnapshotDigest || parsed.WorktreeID != captured.WorktreeID ||
		parsed.ProfileID != job.Start.ProfileID ||
		parsed.ProviderDigest != job.Start.ProviderDigest {
		return ErrAuthority
	}
	expected := map[string]bool{manifestName: true}
	for _, item := range parsed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.Status != "captured" {
			continue
		}
		if expected[item.Path] {
			return ErrAuthority
		}
		expected[item.Path] = true
		source, err := root.OpenRegular(item.Path)
		if err != nil {
			return ErrAuthority
		}
		digest := sha256.New()
		size, readErr := io.Copy(digest, io.LimitReader(source, *item.ByteLength+1))
		closeErr := source.Close()
		if readErr != nil || closeErr != nil || size != *item.ByteLength ||
			hex.EncodeToString(digest.Sum(nil)) != *item.ContentHash {
			return ErrAuthority
		}
	}
	return verifyMembers(ctx, root, name, expected)
}

func verifyMembers(ctx context.Context, root *paths.Root, name string, expected map[string]bool) error {
	directories := map[string]bool{".": true}
	for file := range expected {
		for dir := filepath.Dir(file); !directories[dir]; dir = filepath.Dir(dir) {
			directories[dir] = true
		}
	}
	err := fs.WalkDir(os.DirFS(name), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return ErrAuthority
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if !directories[path] {
				return ErrAuthority
			}
			return nil
		}
		if !entry.Type().IsRegular() || !expected[path] {
			return ErrAuthority
		}
		delete(expected, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(expected) != 0 || root.Verify() != nil {
		return ErrAuthority
	}
	return nil
}

func hexDigest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func openPinned(path, expected string, limit int64, executable bool) (*os.File, error) {
	dir, err := paths.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, ErrAuthority
	}
	defer dir.Close()
	file, err := dir.OpenRegular(filepath.Base(path))
	if err != nil {
		return nil, ErrAuthority
	}
	var before, after unix.Stat_t
	if unix.Fstat(int(file.Fd()), &before) == nil && before.Mode&0022 == 0 &&
		(!executable || before.Mode&0111 != 0) && before.Size > 0 && before.Size <= limit {
		digest := sha256.New()
		size, err := io.Copy(digest, io.LimitReader(file, limit+1))
		if err == nil && size == before.Size && expected == hex.EncodeToString(digest.Sum(nil)) &&
			unix.Fstat(int(file.Fd()), &after) == nil && before.Ino == after.Ino && before.Dev == after.Dev &&
			before.Size == after.Size && before.Nlink == after.Nlink && before.Mode == after.Mode &&
			before.Ctim == after.Ctim && before.Mtim == after.Mtim &&
			dir.Verify() == nil {
			return file, nil
		}
	}
	_ = file.Close()
	return nil, ErrAuthority
}

type stderrLimit struct {
	bytes  int
	cancel context.CancelCauseFunc
}

func (p *NodeProvider) sealWorker() (*os.File, error) {
	body := p.config.EmbeddedWorker
	if len(body) == 0 {
		program, err := openPinned(p.config.Entrypoint, p.config.ProviderDigest, maxProgramBytes, false)
		if err != nil {
			return nil, err
		}
		defer program.Close()
		if _, err := program.Seek(0, io.SeekStart); err != nil {
			return nil, ErrAuthority
		}
		body, err = io.ReadAll(io.LimitReader(program, maxProgramBytes+1))
		if err != nil {
			return nil, ErrAuthority
		}
	} else if p.config.Entrypoint != "" {
		return nil, ErrAuthority
	}
	return sealProviderProgram(body, p.config.ProviderDigest)
}

func sealProviderProgram(body []byte, digest string) (*os.File, error) {
	if len(body) == 0 || len(body) > maxProgramBytes || hexDigest(body) != digest {
		return nil, ErrAuthority
	}
	fd, err := unix.MemfdCreate("mce4d-provider", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, ErrUnsupported
	}
	worker := os.NewFile(uintptr(fd), "provider-entrypoint")
	if _, err = worker.Write(body); err == nil {
		_, err = unix.FcntlInt(worker.Fd(), unix.F_ADD_SEALS,
			unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL)
	}
	if err != nil {
		_ = worker.Close()
		return nil, ErrProcess
	}
	return worker, nil
}

func (s *stderrLimit) Write(data []byte) (int, error) {
	s.bytes += len(data)
	if s.bytes > maxStderr {
		s.cancel(ErrBudget)
		return 0, ErrBudget
	}
	return len(data), nil
}

func runFramedNode(ctx context.Context, root, compiler *paths.Root, node, script *os.File, job ports.ProviderJob,
	mode string, consume func(context.Context, *bufio.Reader) error) (resultErr error) {
	const programFD = "/proc/self/fd/4"
	args := []string{"--permission", "--preserve-symlinks-main",
		"--max-old-space-size=256", "--allow-fs-read=" + programFD,
		"--allow-fs-read=" + job.Start.SnapshotRoot}
	env := []string{"LC_ALL=C", "TZ=UTC"}
	if mode != "" {
		env = append(env, "MCE4D_PROVIDER_MODE="+mode)
	}
	var verify func(context.Context) error
	if compiler != nil {
		path, err := compiler.CanonicalPath()
		if err != nil {
			return ErrAuthority
		}
		args = append(args, "--allow-fs-read="+path)
		env = append(env, "MCE4D_COMPILER_ROOT="+path)
		verify = func(context.Context) error { return compiler.Verify() }
	}
	return runFramedWorker(ctx, root, node, script, job, append(args, programFD), env, verify, consume)
}

func runFramedWorker(ctx context.Context, root *paths.Root, executable, script *os.File, job ports.ProviderJob,
	args, env []string, verify func(context.Context) error, consume func(context.Context, *bufio.Reader) error,
) (resultErr error) {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	dir, err := root.DirectoryHandle()
	if err != nil {
		return ErrAuthority
	}
	defer dir.Close()
	inR, inW, err := os.Pipe()
	if err != nil {
		return ErrProcess
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		return ErrProcess
	}
	defer outR.Close()
	defer outW.Close()

	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", args...)
	cmd.Dir = "/proc/self/fd/" + strconv.FormatUint(uint64(dir.Fd()), 10)
	cmd.Env = env
	cmd.ExtraFiles = []*os.File{executable, script}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, &stderrLimit{cancel: stop}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = 2 * time.Second
	var write sync.Mutex
	limit := int(job.Start.MaxFrameBytes)
	cancelFrame, err := encodeFrame(&model.ProviderCancel{ProviderProtocol: 1, Type: "cancel", JobID: job.Start.JobID}, limit)
	if err != nil {
		return err
	}
	cmd.Cancel = func() error {
		if write.TryLock() {
			// One writer owns the pipe. If the start is still being written,
			// skip the optional cancel frame and stop the worker directly.
			_ = inW.SetWriteDeadline(time.Now().Add(25 * time.Millisecond))
			_, _ = inW.Write(cancelFrame)
			write.Unlock()
		}
		return cmd.Process.Kill()
	}
	if err := cmd.Start(); err != nil {
		return ErrProcess
	}
	_ = inR.Close()
	_ = outW.Close()
	defer func() {
		_ = inW.Close()
		if resultErr != nil {
			_ = cmd.Process.Kill()
		}
		_ = outR.Close()
		waitErr := cmd.Wait()
		if cause := context.Cause(ctx); cause != nil {
			resultErr = cause
		} else if waitErr != nil && resultErr == nil {
			resultErr = ErrProcess
		}
		if resultErr == nil {
			if err := verifyCapture(ctx, root, job); err != nil {
				resultErr = err
			} else if job.Recheck(ctx) != nil || verify != nil && verify(ctx) != nil {
				resultErr = ErrAuthority
			}
		}
	}()
	wire, err := encodeFrame(&job.Start, limit)
	if err != nil {
		return err
	}
	write.Lock()
	_, err = inW.Write(wire)
	write.Unlock()
	if err != nil {
		return ErrProcess
	}
	reader := bufio.NewReaderSize(outR, 8192)
	if err := consume(ctx, reader); err != nil {
		return err
	}
	_ = inW.Close()
	// Reject a second terminal frame or stray stdout before reaping.
	if _, err := reader.Peek(1); !errors.Is(err, io.EOF) {
		return ErrProtocol
	}
	return nil
}

func readProviderStream(ctx context.Context, reader *bufio.Reader, job ports.ProviderJob, sink ports.ProviderSink) (done model.ProviderDone, resultErr error) {
	facts := factScope{capture: job.Capture, packages: map[string]bool{}, symbols: map[string]bool{},
		relations: map[string]bool{}, diagnostics: map[string]bool{}}
	manifest := job.Capture.Manifest()
	stream, err := model.BeginProviderStream(job.Start.JobID, manifest.SnapshotDigest)
	if err != nil {
		return done, ErrProtocol
	}
	limit := int(job.Start.MaxFrameBytes)
	remaining := maxStream
	for {
		data, err := readFrame(reader, limit)
		if err != nil {
			return done, err
		}
		remaining -= len(data)
		if remaining < 0 {
			return done, ErrBudget
		}
		message, err := model.DecodeProviderMessageJSON(data, limit)
		if err != nil {
			return done, ErrProtocol
		}
		stream, err = model.AcceptProviderOutput(stream, message, limit)
		if err != nil {
			return done, ErrProtocol
		}
		if err := job.Recheck(ctx); err != nil {
			return done, ErrAuthority
		}
		switch value := message.(type) {
		case *model.ProviderBatch:
			if err := facts.batch(ctx, value); err != nil {
				return done, err
			}
			if err := sink(ctx, *value); err != nil {
				return done, err
			}
		case *model.ProviderDone:
			if err := checkDone(job, manifest.WorktreeID, value); err != nil {
				return done, err
			}
			return *value, nil
		}
	}
}

package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

const (
	windowsIPCRoleWorker        = "__mce4d-role-ipc-v1"
	windowsIPCImageLimit        = 128 << 20
	windowsIPCFrameSize         = 80
	windowsIPCRegisterCatalog   = 1
	windowsIPCProvisionWorktree = 2
	windowsIPCOpenPools         = 3
	windowsIPCMaintenance       = 4
	windowsIPCCatalogAdmin      = 5
)

type windowsRolePeer struct {
	process windows.Handle
	job     *runtime.WindowsProcessJob
	input   *os.File
	output  *os.File
	role    byte
	sid     string
}

type windowsRoleSession struct {
	engine, client *windowsRolePeer
	controller     string
	operation      byte
	binding        [32]byte
}

func withWindowsRoleIPC(ctx context.Context, m windowsProfilePreparation, files windowsMaintenanceFiles, operation byte, scope string, use func(func() error) error) (resultErr error) {
	session, err := openWindowsRoleSession(ctx, m, files, operation, scope)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, session.Close()) }()
	return use(func() error { return session.check(ctx) })
}

func openWindowsRoleSession(ctx context.Context, m windowsProfilePreparation, files windowsMaintenanceFiles, operation byte, scope string) (*windowsRoleSession, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, ErrProfileInput
	}
	image, err := windowsRoleWorkerImage(ctx)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(image)
	launch := func(role byte, sid, label string) (*windowsRolePeer, error) {
		worker, release, err := files.pinWindowsRoleWorker(ctx, sid, label, image, digest)
		if err != nil {
			return nil, err
		}
		peer, err := startWindowsRolePeer(ctx, worker, role, sid, m.ControllerSID)
		if failure := errors.Join(err, release()); failure != nil {
			if peer != nil {
				failure = errors.Join(failure, peer.close())
			}
			return nil, failure
		}
		return peer, nil
	}
	engine, err := launch(1, m.EngineSID, "engine")
	if err != nil {
		return nil, err
	}
	client, err := launch(2, m.ClientSID, "client")
	if err != nil {
		return nil, errors.Join(err, engine.close())
	}
	session := &windowsRoleSession{engine: engine, client: client, controller: m.ControllerSID,
		operation: operation, binding: sha256.Sum256([]byte("MCE4D-WINDOWS-ROLE-IPC1\x00" + m.ProfileID + "\x00" + scope))}
	if err := session.check(ctx); err != nil {
		return nil, errors.Join(err, session.Close())
	}
	return session, nil
}

// The session owner serializes requests and closes only after they drain.
func (s *windowsRoleSession) check(ctx context.Context) error {
	if err := s.engine.exchange(ctx, s.controller, s.operation, s.binding); err != nil {
		return err
	}
	return s.client.exchange(ctx, s.controller, s.operation, s.binding)
}

func (s *windowsRoleSession) Close() error {
	return errors.Join(s.client.close(), s.engine.close())
}

func windowsRoleWorkerImage(ctx context.Context) (image []byte, resultErr error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return readWindowsToolImage(ctx, executable)
}

func readWindowsToolImage(ctx context.Context, executable string) (_ []byte, resultErr error) {
	dir, err := paths.OpenRoot(filepath.Dir(executable))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	file, err := dir.OpenRegular(filepath.Base(executable))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > windowsIPCImageLimit {
		return nil, errors.Join(ErrStateUnsafe, err)
	}
	image := make([]byte, int(info.Size()))
	for off := 0; off < len(image); {
		end := min(off+(64<<10), len(image))
		if err := readLogicalAt(ctx, file, image[off:end], int64(off)); err != nil {
			return nil, err
		}
		off = end
	}
	return image, errors.Join(dir.Verify(), paths.VerifyRegularFile(file), ctx.Err())
}

func (j windowsMaintenanceFiles) pinWindowsRoleWorker(ctx context.Context, sid, label string, image []byte, digest [32]byte) (path string, release func() error, resultErr error) {
	return j.pinWindowsRoleImage(ctx, sid, "ipc-"+label, "mce4d.exe", image, digest)
}

func (j windowsMaintenanceFiles) pinWindowsRoleImage(ctx context.Context, sid, label, leaf string, image []byte, digest [32]byte) (path string, release func() error, resultErr error) {
	account, err := windows.StringToSid(sid)
	if err != nil || !windowsPreparationSID(sid) || account.SubAuthorityCount() != 8 {
		return "", nil, errors.Join(ErrProfileDenied, err)
	}
	slot := label + "-" + hex.EncodeToString(digest[:])
	acl := windowsControlACL{owner: j.acl.owner, reader: account}
	root, err := j.control.OpenDirectory(slot)
	if errors.Is(err, os.ErrNotExist) {
		// Scope the read/execute grant to a freshly created image directory.
		// No permissions are changed on the profile or its data directories.
		if err := createWindowsProfileDirectoryWithACL(ctx, j.control, j.acl, acl, slot); err != nil && !errors.Is(err, ErrProfileExists) {
			return "", nil, err
		}
		root, err = j.control.OpenDirectory(slot)
	}
	if err != nil {
		return "", nil, err
	}
	var worker *os.File
	defer func() {
		if resultErr != nil {
			if worker != nil {
				resultErr = errors.Join(resultErr, worker.Close())
			}
			resultErr = errors.Join(resultErr, root.Close())
			path, release = "", nil
		}
	}()
	stage := windowsMaintenanceFiles{control: root, acl: acl, profile: j.profile, scope: j.scope}
	dir, err := stage.directory(ctx)
	if err != nil {
		return "", nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	worker, err = root.OpenRegular(leaf)
	if errors.Is(err, os.ErrNotExist) {
		descriptor, descriptorErr := acl.descriptor()
		if descriptorErr != nil {
			return "", nil, descriptorErr
		}
		if err := stage.publish(ctx, dir, descriptor, leaf, image, false, nil); err != nil {
			return "", nil, err
		}
		worker, err = root.OpenRegular(leaf)
	}
	if err != nil {
		return "", nil, err
	}
	if err := acl.check(worker); err != nil {
		return "", nil, err
	}
	info, err := worker.Stat()
	if err != nil || info.Size() != int64(len(image)) {
		return "", nil, errors.Join(ErrProfileDenied, err)
	}
	actual, err := hashLogicalReader(ctx, worker, info.Size())
	if err != nil || actual != digest {
		return "", nil, errors.Join(ErrProfileDenied, err)
	}
	base, err := root.CanonicalPath()
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(base, leaf), func() error { return errors.Join(worker.Close(), root.Close()) }, nil
}

func startWindowsRolePeer(ctx context.Context, image string, role byte, expected, controller string) (_ *windowsRolePeer, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sid, err := windows.StringToSid(expected)
	if err != nil {
		return nil, err
	}
	job, err := runtime.NewWindowsProcessJob(1)
	if err != nil {
		return nil, err
	}
	p := &windowsRolePeer{job: job, role: role, sid: expected}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, p.close())
		}
	}()
	stdin, tx, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p.input = tx
	defer stdin.Close()
	rx, stdout, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p.output = rx
	defer stdout.Close()
	environment := []string{"GOMAXPROCS=2"}
	for _, name := range []string{"LOCALAPPDATA", "SystemRoot", "TEMP", "TMP", "USERPROFILE"} {
		environment = append(environment, name+"="+os.Getenv(name))
	}
	p.process, err = job.StartChild(ctx, runtime.WindowsJobChild{
		Executable: image, Directory: filepath.Dir(image), Arguments: []string{windowsIPCRoleWorker},
		Environment: environment, AppContainer: sid, Stdin: stdin, Stdout: stdout, Stderr: stdout,
	}, func(process windows.Handle) error {
		return matchWindowsRole(process, expected, controller)
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *windowsRolePeer) exchange(ctx context.Context, controller string, operation byte, binding [32]byte) error {
	if err := errors.Join(ctx.Err(), liveWindowsRoleProcess(p.process), matchWindowsRole(p.process, p.sid, controller)); err != nil {
		return err
	}
	var frame [windowsIPCFrameSize]byte
	copy(frame[:8], "MCEIPC01")
	frame[8], frame[9] = p.role, operation
	if _, err := rand.Read(frame[16:48]); err != nil {
		return err
	}
	copy(frame[48:], binding[:])
	result := make(chan error, 1)
	go func() {
		if _, err := p.input.Write(frame[:]); err != nil {
			result <- err
			return
		}
		var reply [windowsIPCFrameSize]byte
		if _, err := io.ReadFull(p.output, reply[:]); err != nil {
			result <- err
			return
		}
		copy(frame[:8], "MCEACK01")
		if !bytes.Equal(frame[:], reply[:]) {
			result <- ErrProfileDenied
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return errors.Join(err, ctx.Err(), liveWindowsRoleProcess(p.process), matchWindowsRole(p.process, p.sid, controller))
	case <-ctx.Done():
		// The job owns every inherited writer. Its termination releases any
		// blocked pipe operation before its buffers or handles can be reused.
		stop := p.job.Terminate(3)
		if stop != nil {
			stop = errors.Join(stop, windows.TerminateProcess(p.process, 3))
		}
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case err := <-result:
			return errors.Join(ctx.Err(), stop, err)
		case <-timer.C:
			return errors.Join(ctx.Err(), stop, ErrProfileIncomplete)
		}
	}
}

func (p *windowsRolePeer) close() error {
	var err error
	if p.input != nil {
		err = p.input.Close()
	}
	err = errors.Join(err, p.job.Close())
	if p.process != 0 {
		status, waitErr := windows.WaitForSingleObject(p.process, uint32((3 * time.Second).Milliseconds()))
		if waitErr == nil && status != windows.WAIT_OBJECT_0 {
			waitErr = ErrProfileIncomplete
		}
		err = errors.Join(err, waitErr, windows.CloseHandle(p.process))
	}
	if p.output != nil {
		err = errors.Join(err, p.output.Close())
	}
	return err
}

func runPlatformProtectedWorker(ctx context.Context, args []string, output io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != windowsIPCRoleWorker {
		return false, 0
	}
	token := windows.GetCurrentProcessToken()
	isolated, err := windowsTokenDWORD(token, windowsTokenIsAppContainer)
	if len(args) != 1 || err != nil || isolated != 1 {
		return true, 3
	}
	for {
		if ctx.Err() != nil {
			return true, 3
		}
		var frame [windowsIPCFrameSize]byte
		_, err := io.ReadFull(os.Stdin, frame[:])
		if errors.Is(err, io.EOF) {
			return true, 0
		}
		if err != nil || string(frame[:8]) != "MCEIPC01" || frame[8] < 1 || frame[8] > 2 ||
			(frame[9] < windowsIPCRegisterCatalog || frame[9] > windowsIPCCatalogAdmin) ||
			!bytes.Equal(frame[10:16], make([]byte, 6)) {
			return true, 3
		}
		copy(frame[:8], "MCEACK01")
		if n, err := output.Write(frame[:]); err != nil || n != len(frame) {
			return true, 3
		}
	}
}

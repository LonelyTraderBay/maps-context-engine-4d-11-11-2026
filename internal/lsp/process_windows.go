package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

type managedProcess struct {
	*processPipes
	job     *runtime.WindowsProcessJob
	once    sync.Once
	jobMu   sync.Mutex
	waitErr error
	stopErr error
}

func startSessionProcess(ctx context.Context, config SessionConfig, executable *os.File, env []string) (_ *managedProcess, resultErr error) {
	sid, err := windows.StringToSid(config.WindowsContainerSID)
	if err != nil || !strings.HasPrefix(config.WindowsContainerSID, "S-1-15-2-") || sid.SubAuthorityCount() != 8 {
		return nil, ErrLSPAuthority
	}
	root, err := config.ServerBinRoot.CanonicalPath()
	if err != nil {
		return nil, err
	}
	workspace, err := paths.OpenRoot(config.Environment.WorkspaceRoot)
	if err != nil {
		return nil, errors.Join(ErrLSPAuthority, err)
	}
	defer workspace.Close()
	system, err := windows.GetSystemWindowsDirectory()
	if err != nil || !filepath.IsAbs(system) {
		return nil, errors.Join(ErrLSPAuthority, err)
	}
	scratch := config.Environment.ScratchRoot
	env = append(env, "SystemRoot="+system, "PATHEXT=.EXE", "TEMP="+filepath.Join(scratch, "tmp"),
		"TMP="+filepath.Join(scratch, "tmp"), "APPDATA="+filepath.Join(scratch, "home"),
		"LOCALAPPDATA="+filepath.Join(scratch, "home"), "USERPROFILE="+filepath.Join(scratch, "home"),
		// The pinned gopls telemetry package uses this value for a process
		// that must neither collect nor spawn a telemetry sidecar.
		"GO_TELEMETRY_CHILD=2")
	job, err := runtime.NewWindowsProcessJob(16)
	if err != nil {
		return nil, errors.Join(ErrLSPProcess, err)
	}
	var pairs [3][2]*os.File
	defer func() {
		if resultErr != nil {
			for _, pair := range pairs {
				for _, file := range pair {
					if file != nil {
						_ = file.Close()
					}
				}
			}
			resultErr = errors.Join(resultErr, job.Close())
		}
	}()
	for i := range pairs {
		pairs[i][0], pairs[i][1], err = os.Pipe()
		if err != nil {
			return nil, errors.Join(ErrLSPProcess, err)
		}
	}
	child, err := job.StartChild(ctx, runtime.WindowsJobChild{
		Executable: filepath.Join(root, "gopls.exe"), Directory: config.Environment.WorkspaceRoot,
		Environment: env, AppContainer: sid, Stdin: pairs[0][0], Stdout: pairs[1][1], Stderr: pairs[2][1],
	}, func(process windows.Handle) error {
		return errors.Join(workspace.Verify(), config.Check(ctx), verifyServerBinding(ctx, config.ServerBinRoot, executable, config.ExpectedSHA256),
			verifyLSPContainer(process, sid))
	})
	if err != nil {
		return nil, errors.Join(ErrLSPProcess, err)
	}
	_ = pairs[0][0].Close()
	_ = pairs[1][1].Close()
	_ = pairs[2][1].Close()
	p := &managedProcess{job: job}
	p.processPipes = newProcessPipes(pairs[0][1], pairs[1][0], pairs[2][0], p.terminate)
	go func() {
		event, err := windows.WaitForSingleObject(child, windows.INFINITE)
		var code uint32
		if err == nil && event == windows.WAIT_OBJECT_0 {
			err = windows.GetExitCodeProcess(child, &code)
		}
		if event != windows.WAIT_OBJECT_0 || code != 0 {
			err = errors.Join(ErrLSPProcess, err)
		}
		p.waitErr = errors.Join(err, windows.CloseHandle(child))
		close(p.done)
	}()
	return p, nil
}

func verifyLSPContainer(process windows.Handle, expected *windows.SID) (resultErr error) {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, token.Close()) }()
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	child, err := token.GetTokenUser()
	if err != nil || !child.User.Sid.Equals(owner.User.Sid) {
		return errors.Join(ErrLSPAuthority, err)
	}
	const tokenAppContainerSid = 31
	var size uint32
	_ = windows.GetTokenInformation(token, tokenAppContainerSid, nil, 0, &size)
	if size == 0 || size > 4096 {
		return ErrLSPAuthority
	}
	raw := make([]byte, size)
	if err := windows.GetTokenInformation(token, tokenAppContainerSid, &raw[0], size, &size); err != nil {
		return err
	}
	info := (*struct{ SID *windows.SID })(unsafe.Pointer(&raw[0]))
	if info.SID == nil || !info.SID.Equals(expected) {
		return ErrLSPAuthority
	}
	return nil
}

func (p *managedProcess) close() error {
	if p == nil {
		return ErrLSPProcess
	}
	p.once.Do(func() {
		close(p.stop)
		p.terminate()
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		_ = p.stderr.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		waitErr := p.job.WaitEmpty(ctx)
		p.jobMu.Lock()
		closeErr := p.job.Close()
		p.jobMu.Unlock()
		if err := errors.Join(waitErr, closeErr); err != nil {
			p.stopErr = errors.Join(ErrLSPProcess, err)
		}
		select {
		case <-p.done:
		case <-ctx.Done():
			p.stopErr = errors.Join(ErrLSPProcess, p.stopErr, ctx.Err())
		}
		finished := make(chan struct{})
		go func() { p.workers.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-ctx.Done():
			p.stopErr = errors.Join(ErrLSPProcess, p.stopErr, ctx.Err())
		}
	})
	return p.stopErr
}

func (p *managedProcess) terminate() {
	p.jobMu.Lock()
	defer p.jobMu.Unlock()
	_ = p.job.Terminate(71)
}

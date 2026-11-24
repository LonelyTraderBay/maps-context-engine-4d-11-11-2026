package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsProcessJob owns a bounded Win32 process tree. It must be closed by its
// owner after use. A launcher must attach the initial child before resuming it.
type WindowsProcessJob struct {
	handle windows.Handle
}

// WindowsJobChild describes an already approved executable and workspace.
// The launcher owns neither the caller's files nor its authorization policy.
type WindowsJobChild struct {
	Executable, Directory  string
	Arguments, Environment []string
	AppContainer           *windows.SID
	Stdin, Stdout, Stderr  *os.File
}

var errWindowsJobInput = errors.New("invalid Windows process job input")

func NewWindowsProcessJob(activeLimit uint32) (*WindowsProcessJob, error) {
	return newWindowsProcessJob(activeLimit, 0)
}

// NewWindowsProviderJob bounds a native parser to one process and a finite
// amount of committed private memory.
func NewWindowsProviderJob() (*WindowsProcessJob, error) {
	return newWindowsProcessJob(1, 768<<20)
}

func newWindowsProcessJob(activeLimit uint32, memoryLimit uintptr) (*WindowsProcessJob, error) {
	if activeLimit < 1 || activeLimit > 32 {
		return nil, errWindowsJobInput
	}
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
	limits.BasicLimitInformation.ActiveProcessLimit = activeLimit
	if memoryLimit != 0 {
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
		limits.ProcessMemoryLimit = memoryLimit
	}
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	return &WindowsProcessJob{handle: handle}, nil
}

// StartChild inherits only the declared stdio handles. The child joins the
// job suspended, so the owner can verify its process token before resume.
// A non-nil AppContainer is launched without additional capabilities.
func (j *WindowsProcessJob) StartChild(ctx context.Context, child WindowsJobChild,
	check func(windows.Handle) error) (_ windows.Handle, resultErr error) {
	if ctx == nil || j == nil || j.handle == 0 || !filepath.IsAbs(child.Executable) || !filepath.IsAbs(child.Directory) ||
		child.Stdin == nil || child.Stdout == nil || child.Stderr == nil || child.AppContainer != nil && !child.AppContainer.IsValid() {
		return 0, errWindowsJobInput
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	executable, err := windows.UTF16FromString(child.Executable)
	if err != nil {
		return 0, err
	}
	directory, err := windows.UTF16FromString(child.Directory)
	if err != nil {
		return 0, err
	}
	argv, err := windows.UTF16FromString(windows.ComposeCommandLine(append([]string{child.Executable}, child.Arguments...)))
	if err != nil {
		return 0, err
	}
	env := make([]uint16, 0)
	for _, entry := range child.Environment {
		part, err := windows.UTF16FromString(entry)
		if err != nil {
			return 0, err
		}
		env = append(env, part...)
	}
	if len(env) == 0 {
		env = append(env, 0)
	}
	env = append(env, 0)
	var handles [3]windows.Handle
	self := windows.CurrentProcess()
	for i, file := range []*os.File{child.Stdin, child.Stdout, child.Stderr} {
		if err := windows.DuplicateHandle(self, windows.Handle(file.Fd()), self, &handles[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return 0, err
		}
		defer windows.CloseHandle(handles[i])
	}
	count := uint32(1)
	if child.AppContainer != nil {
		count++
	}
	attrs, err := windows.NewProcThreadAttributeList(count)
	if err != nil {
		return 0, err
	}
	defer attrs.Delete()
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), unsafe.Sizeof(handles)); err != nil {
		return 0, err
	}
	capabilities := struct {
		sid      *windows.SID
		list     *windows.SIDAndAttributes
		count    uint32
		reserved uint32
	}{sid: child.AppContainer}
	if child.AppContainer != nil {
		const securityCapabilities = 0x00020009
		if err := attrs.Update(securityCapabilities, unsafe.Pointer(&capabilities), unsafe.Sizeof(capabilities)); err != nil {
			return 0, err
		}
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES,
			StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2]},
		ProcThreadAttributeList: attrs.List(),
	}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcess(&executable[0], &argv[0], nil, nil, true, flags, &env[0], &directory[0], &startup.StartupInfo, &process); err != nil {
		return 0, err
	}
	defer windows.CloseHandle(process.Thread)
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, windows.TerminateProcess(process.Process, 72), windows.CloseHandle(process.Process))
		}
	}()
	if err := j.AssignSuspended(process.Process); err != nil {
		return 0, err
	}
	if check != nil {
		if err := check(process.Process); err != nil {
			return 0, err
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		return 0, err
	}
	return process.Process, nil
}

func (j *WindowsProcessJob) AssignSuspended(process windows.Handle) error {
	if j == nil || j.handle == 0 || process == 0 || process == windows.InvalidHandle {
		return errWindowsJobInput
	}
	return windows.AssignProcessToJobObject(j.handle, process)
}

func (j *WindowsProcessJob) Terminate(exitCode uint32) error {
	if j == nil || j.handle == 0 {
		return os.ErrClosed
	}
	return windows.TerminateJobObject(j.handle, exitCode)
}

// WaitEmpty observes all processes in the job, including descendants of a
// child that has already exited. It never extends the caller's deadline.
func (j *WindowsProcessJob) WaitEmpty(ctx context.Context) error {
	if j == nil || j.handle == 0 {
		return os.ErrClosed
	}
	if ctx == nil {
		return errWindowsJobInput
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errWindowsJobInput
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION layout.
		var counts struct {
			userTime, kernelTime, periodUserTime, periodKernelTime int64
			pageFaults, total, active, terminated                  uint32
		}
		if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&counts)), uint32(unsafe.Sizeof(counts)), nil); err != nil {
			return err
		}
		if counts.active == 0 {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (j *WindowsProcessJob) Close() error {
	if j == nil || j.handle == 0 {
		return os.ErrClosed
	}
	err := windows.CloseHandle(j.handle)
	if err == nil {
		j.handle = 0
	}
	return err
}

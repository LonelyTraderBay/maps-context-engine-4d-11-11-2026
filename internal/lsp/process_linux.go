//go:build linux

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

var ErrLSPProcess = errors.New("LSP process failed or could not be contained")

type outboundFrame struct {
	data []byte
	done chan error
}

// managedProcess owns exactly one child process group and its protocol pipes.
// It accepts an already opened executable; it does not authorize the binary,
// environment or working directory. Those are the session's responsibility.
type managedProcess struct {
	cmd        *exec.Cmd
	stdin      *os.File
	stdout     *os.File
	stderr     *os.File
	incoming   chan inboundFrame
	writes     chan outboundFrame
	stop       chan struct{}
	done       chan struct{} // observed with WNOWAIT; PID remains reserved
	reaped     chan struct{}
	observeErr error
	waitErr    error
	once       sync.Once
	reapOnce   sync.Once
	workers    sync.WaitGroup
}

func startManaged(executable *os.File, args []string, cwd string, env []string) (*managedProcess, error) {
	if executable == nil || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd || env == nil {
		return nil, ErrLSPProcess
	}
	cwdRoot, err := paths.OpenRoot(cwd)
	if err != nil {
		return nil, ErrLSPProcess
	}
	defer cwdRoot.Close()
	directory, err := cwdRoot.DirectoryHandle()
	if err != nil {
		return nil, ErrLSPProcess
	}
	defer directory.Close()
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, ErrLSPProcess
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		_ = inRead.Close()
		_ = inWrite.Close()
		return nil, ErrLSPProcess
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		_ = inRead.Close()
		_ = inWrite.Close()
		_ = outRead.Close()
		_ = outWrite.Close()
		return nil, ErrLSPProcess
	}
	// fd 3 is the authorized, pinned executable inode, not a PATH lookup.
	cmd := exec.Command("/proc/self/fd/3", args...)
	// Go's Linux child chdir occurs before descriptor remapping. The inherited
	// O_PATH handle pins the working-directory inode across that operation.
	cmd.Dir = "/proc/self/fd/" + strconv.FormatUint(uint64(directory.Fd()), 10)
	cmd.Env, cmd.ExtraFiles = append([]string{}, env...), []*os.File{executable}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inRead, outWrite, errWrite
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = inRead.Close()
		_ = inWrite.Close()
		_ = outRead.Close()
		_ = outWrite.Close()
		_ = errRead.Close()
		_ = errWrite.Close()
		return nil, ErrLSPProcess
	}
	_ = inRead.Close()
	_ = outWrite.Close()
	_ = errWrite.Close()
	if cwdRoot.Verify() != nil {
		_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		_ = cmd.Wait()
		_ = inWrite.Close()
		_ = outRead.Close()
		_ = errRead.Close()
		return nil, ErrLSPProcess
	}
	p := &managedProcess{cmd: cmd, stdin: inWrite, stdout: outRead, stderr: errRead, incoming: make(chan inboundFrame, 1), writes: make(chan outboundFrame), stop: make(chan struct{}), done: make(chan struct{}), reaped: make(chan struct{})}
	p.workers.Add(3)
	go p.readLoop()
	go p.writeLoop()
	go p.stderrLoop()
	go func() {
		var info unix.Siginfo
		for {
			p.observeErr = unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if !errors.Is(p.observeErr, unix.EINTR) {
				break
			}
		}
		close(p.done)
	}()
	return p, nil
}

func (p *managedProcess) readLoop() {
	defer p.workers.Done()
	defer close(p.incoming)
	reader := bufio.NewReaderSize(p.stdout, 8192)
	var total int64
	for {
		data, err := readLSPFrame(reader, int(resultHardBytes))
		total += int64(len(data))
		if total > 128*1024*1024 {
			err = ErrLSPFrame
		}
		select {
		case p.incoming <- inboundFrame{data: data, err: err}:
		case <-p.stop:
			return
		}
		if err != nil {
			return
		}
	}
}

func (p *managedProcess) writeLoop() {
	defer p.workers.Done()
	for {
		select {
		case <-p.stop:
			return
		case frame := <-p.writes:
			data := frame.data
			var err error
			for len(data) > 0 && err == nil {
				var n int
				n, err = p.stdin.Write(data)
				if n == 0 && err == nil {
					err = io.ErrShortWrite
				}
				data = data[n:]
			}
			frame.done <- err
		}
	}
}

func (p *managedProcess) stderrLoop() {
	defer p.workers.Done()
	buffer := make([]byte, 8192)
	var total int64
	for {
		n, err := p.stderr.Read(buffer)
		total += int64(n)
		if total > 1024*1024 {
			// Do not retain or print provider output, which can contain source.
			_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL)
			return
		}
		if err != nil {
			return
		}
	}
}

func (p *managedProcess) send(ctx context.Context, value any) error {
	if p == nil || ctx == nil {
		return ErrLSPProcess
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ErrLSPProtocol
	}
	data, err = encodeLSPFrame(data, int(resultHardBytes))
	if err != nil {
		return err
	}
	frame := outboundFrame{data: data, done: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stop:
		return ErrLSPProcess
	case <-p.done:
		return ErrLSPProcess
	case p.writes <- frame:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stop:
		return ErrLSPProcess
	case <-p.done:
		return ErrLSPProcess
	case err := <-frame.done:
		if err != nil {
			return ErrLSPProcess
		}
		return ctx.Err()
	}
}

// close stops only this process group and waits for its pipe workers to exit.
// It does not remove scratch files or operate on the target repository.
func (p *managedProcess) close() error {
	if p == nil {
		return ErrLSPProcess
	}
	p.once.Do(func() {
		close(p.stop)
		// A waitable leader retains its PID until cmd.Wait below, preventing
		// process-group reuse between observation and group teardown.
		identityAvailable := true
		select {
		case <-p.done:
			identityAvailable = p.observeErr == nil
		default:
		}
		if identityAvailable {
			_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL)
			_ = p.cmd.Process.Kill()
		}
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		_ = p.stderr.Close()
	})
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		return ErrLSPProcess
	}
	if p.observeErr != nil {
		return ErrLSPProcess
	}
	// SIGKILL is asynchronous: the leader may exit before its children finish
	// kernel work. Keep the leader waitable while the owned group settles.
	if err := waitForProcessGroup(p.cmd.Process.Pid, 5*time.Second); err != nil {
		return err
	}
	finished := make(chan struct{})
	go func() { p.workers.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		return ErrLSPProcess
	}
	p.reapOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.reaped)
	})
	<-p.reaped
	return nil
}

func waitForProcessGroup(group int, timeout time.Duration) error {
	if group < 1 || timeout <= 0 {
		return ErrLSPProcess
	}
	var filesystem unix.Statfs_t
	if unix.Statfs("/proc", &filesystem) != nil || uint64(filesystem.Type) != uint64(unix.PROC_SUPER_MAGIC) {
		return ErrLSPProcess
	}
	deadline := time.Now().Add(timeout)
	emptyScans := 0
	for {
		entries, err := os.ReadDir("/proc")
		if err != nil || len(entries) > 100000 {
			return ErrLSPProcess
		}
		active := false
		for _, entry := range entries {
			if _, err := strconv.Atoi(entry.Name()); err != nil {
				continue
			}
			data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
				continue
			}
			if err != nil {
				return ErrLSPProcess
			}
			end := strings.LastIndexByte(string(data), ')')
			if end < 0 {
				return ErrLSPProcess
			}
			fields := strings.Fields(string(data[end+1:]))
			if len(fields) < 3 {
				return ErrLSPProcess
			}
			pgid, err := strconv.Atoi(fields[2])
			if err != nil {
				return ErrLSPProcess
			}
			if pgid == group && fields[0] != "Z" && fields[0] != "X" && fields[0] != "x" {
				active = true
				break
			}
		}
		if !active {
			emptyScans++
			if emptyScans == 2 {
				return nil
			}
		} else {
			emptyScans = 0
		}
		if time.Now().After(deadline) {
			return ErrLSPProcess
		}
		time.Sleep(10 * time.Millisecond)
	}
}
